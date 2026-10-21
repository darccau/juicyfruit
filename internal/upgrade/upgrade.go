package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Error struct{ Message string }

func (e *Error) Error() string                { return e.Message }
func Errorf(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

type Client struct {
	Repository string
	Environ    map[string]string
	HTTP       *http.Client
}

func NewClient() *Client {
	env := map[string]string{}
	for _, key := range []string{"JUICYFRUIT_GITHUB_REPOSITORY", "JUICYFRUIT_DOWNLOAD_BASE_URL", "JUICYFRUIT_LATEST_VERSION"} {
		env[key] = os.Getenv(key)
	}
	return &Client{Repository: repository(env), Environ: env, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func repository(env map[string]string) string {
	if value := strings.TrimSpace(env["JUICYFRUIT_GITHUB_REPOSITORY"]); value != "" {
		return value
	}
	return "darccau/juicyfruit"
}

func (c *Client) AssetBaseURL() string {
	if value := strings.TrimSpace(c.Environ["JUICYFRUIT_DOWNLOAD_BASE_URL"]); value != "" {
		if !strings.HasSuffix(value, "/") {
			value += "/"
		}
		return value
	}
	return "https://github.com/" + c.Repository + "/releases/latest/download/"
}

func (c *Client) LatestVersion() (string, error) {
	if value := strings.TrimSpace(c.Environ["JUICYFRUIT_LATEST_VERSION"]); value != "" {
		return strings.TrimPrefix(value, "v"), nil
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	request, err := http.NewRequest("GET", "https://github.com/"+c.Repository+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", Errorf("Cannot reach GitHub: %v", err)
	}
	response.Body.Close()
	location := response.Header.Get("Location")
	if response.StatusCode < 300 || response.StatusCode >= 400 || location == "" {
		return "", Errorf("Cannot resolve the latest release: HTTP %d.", response.StatusCode)
	}
	parts := strings.Split(strings.TrimRight(location, "/"), "/")
	tag := parts[len(parts)-1]
	if tag == "" {
		return "", Errorf("Cannot resolve the latest release: empty tag.")
	}
	return strings.TrimPrefix(tag, "v"), nil
}

func (c *Client) Download(rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, Errorf("Unsupported release URL scheme: ")
	}
	if parsed.Scheme == "file" {
		path := parsed.Path
		if parsed.Host != "" && parsed.Host != "localhost" {
			path = filepath.Join(parsed.Host, parsed.Path)
		}
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			return nil, Errorf("Release asset does not exist: %s", filepath.FromSlash(path))
		}
		return data, nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, Errorf("Unsupported release URL scheme: %s", parsed.Scheme)
	}
	response, err := c.HTTP.Get(rawURL)
	if err != nil {
		return nil, Errorf("Cannot download release asset: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil, Errorf("Cannot download release asset: HTTP %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}

type Outcome struct {
	PreviousVersion string
	LatestVersion   string
	UpdateAvailable bool
	Applied         bool
	ExecutablePath  string
	Note            string
}

func (o Outcome) Map() map[string]any {
	out := map[string]any{
		"previous_version": o.PreviousVersion, "latest_version": o.LatestVersion,
		"update_available": o.UpdateAvailable, "applied": o.Applied,
		"executable_path": nil, "note": nil,
	}
	if o.ExecutablePath != "" {
		out["executable_path"] = o.ExecutablePath
	}
	if o.Note != "" {
		out["note"] = o.Note
	}
	return out
}

func AssetName() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "juicyfruit-linux-x64.tar.gz", nil
	case "linux/arm64":
		return "juicyfruit-linux-arm64.tar.gz", nil
	case "darwin/amd64":
		return "juicyfruit-macos-x64.tar.gz", nil
	case "darwin/arm64":
		return "juicyfruit-macos-arm64.tar.gz", nil
	case "windows/amd64":
		return "juicyfruit-windows-x64.zip", nil
	default:
		return "", Errorf("No Juicyfruit release is published for this platform (%s/%s).", runtime.GOOS, runtime.GOARCH)
	}
}

func Run(client *Client, currentVersion string, checkOnly, force bool) (Outcome, error) {
	latest, err := client.LatestVersion()
	if err != nil {
		return Outcome{}, err
	}
	available := isNewer(latest, currentVersion)
	outcome := Outcome{PreviousVersion: currentVersion, LatestVersion: latest, UpdateAvailable: available}
	if checkOnly || (!available && !force) {
		return outcome, nil
	}
	asset, err := AssetName()
	if err != nil {
		return Outcome{}, err
	}
	base := client.AssetBaseURL()
	archiveBytes, err := client.Download(base + asset)
	if err != nil {
		return Outcome{}, err
	}
	sumsBytes, err := client.Download(base + "SHA256SUMS")
	if err != nil {
		return Outcome{}, err
	}
	expected := checksumFor(asset, string(sumsBytes))
	if expected == "" {
		return Outcome{}, Errorf("No checksum published for %s.", asset)
	}
	if expected != fmt.Sprintf("%x", sha256Sum(archiveBytes)) {
		return Outcome{}, Errorf("Checksum verification failed for %s.", asset)
	}
	binary, err := extractExecutable(archiveBytes)
	if err != nil {
		return Outcome{}, err
	}
	note, err := swapExecutable(binary)
	if err != nil {
		return Outcome{}, err
	}
	executable, _ := os.Executable()
	outcome.Applied = true
	outcome.ExecutablePath = executable
	outcome.Note = note
	return outcome, nil
}

func checksumFor(asset, sums string) string {
	for _, line := range strings.Split(strings.ReplaceAll(sums, "\r\n", "\n"), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 2 && (parts[1] == asset || parts[1] == "*"+asset) {
			return strings.ToLower(parts[0])
		}
	}
	return ""
}

func sha256Sum(data []byte) []byte {
	// kept as a helper indirection to make checksum code compact
	return sha256Digest(data)
}

func extractExecutable(data []byte) ([]byte, error) {
	name := "juicyfruit"
	if runtime.GOOS == "windows" {
		name = "juicyfruit.exe"
	}
	if len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] >= 3 && data[2] <= 7 {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, Errorf("Cannot decode release archive: %v", err)
		}
		for _, entry := range reader.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			if entry.Name == name || filepath.Base(entry.Name) == name {
				in, err := entry.Open()
				if err != nil {
					return nil, err
				}
				defer in.Close()
				return io.ReadAll(in)
			}
		}
		return nil, Errorf("Release archive did not contain %s.", name)
	}
	var reader io.Reader = bytes.NewReader(data)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, Errorf("Cannot decode release archive: %v", err)
		}
		defer gz.Close()
		reader = gz
	}
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Errorf("Cannot decode release archive: %v", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Name == name || filepath.Base(header.Name) == name {
			return io.ReadAll(tarReader)
		}
	}
	return nil, Errorf("Release archive did not contain %s.", name)
}

func swapExecutable(binary []byte) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(executable)
	name := filepath.Base(executable)
	nonce := fmt.Sprintf("%d-%d-%d", os.Getpid(), time.Now().UnixMicro(), time.Now().UnixNano()%1000000)
	staged := filepath.Join(directory, "."+name+".new-"+nonce)
	if err := os.WriteFile(staged, binary, 0o644); err != nil {
		return "", Errorf("Cannot replace the executable: %v", err)
	}
	defer os.Remove(staged)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(staged, 0o755); err != nil {
			return "", Errorf("Cannot mark the new binary executable: %v", err)
		}
		if err := os.Rename(staged, executable); err != nil {
			return "", Errorf("Cannot replace the executable: %v", err)
		}
		return "", nil
	}
	backup := executable + ".old-" + nonce
	if err := os.Rename(executable, backup); err != nil {
		return "", Errorf("Cannot replace the executable: %v", err)
	}
	if err := os.Rename(staged, executable); err != nil {
		_ = os.Rename(backup, executable)
		return "", Errorf("Cannot replace the executable: %v", err)
	}
	if err := os.Remove(backup); err != nil {
		return "Previous binary kept at " + backup + "; delete it after restarting your shell.", nil
	}
	return "", nil
}

func isNewer(candidate, current string) bool {
	left, ok1 := parseVersion(candidate)
	right, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return candidate != current
	}
	for index := 0; index < 3; index++ {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return false
}

func parseVersion(value string) ([3]int, bool) {
	core := strings.Split(strings.Split(value, "-")[0], "+")[0]
	parts := strings.Split(core, ".")
	var out [3]int
	if len(parts) != 3 {
		return out, false
	}
	for index, part := range parts {
		number := 0
		if part == "" {
			return out, false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return out, false
			}
			number = number*10 + int(char-'0')
		}
		out[index] = number
	}
	return out, true
}

func Pretty(value any) string {
	data, _ := json.MarshalIndent(value, "", "  ")
	return string(data)
}
