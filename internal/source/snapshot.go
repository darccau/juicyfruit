package source

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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type Snapshot struct {
	CacheKey  string
	Root      string
	Transport Transport
}

type SnapshotCache struct{ Directory string }

var (
	commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func NewSnapshotCache(directory string) *SnapshotCache { return &SnapshotCache{Directory: directory} }

func (c *SnapshotCache) FetchGit(rawURL, revision string) (Snapshot, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Snapshot{}, Errorf("Unsupported Git URL scheme: (none)")
	}
	switch strings.ToLower(u.Scheme) {
	case "file", "http", "https", "ssh", "git":
	default:
		scheme := u.Scheme
		if scheme == "" {
			scheme = "(none)"
		}
		return Snapshot{}, Errorf("Unsupported Git URL scheme: %s", scheme)
	}
	if revision == "" || len(revision) > 256 || strings.HasPrefix(revision, "-") || strings.ContainsAny(revision, " \t\r\n") {
		return Snapshot{}, Errorf("Invalid Git revision: %s", revision)
	}
	requested := strings.ToLower(revision)
	if commitPattern.MatchString(requested) {
		transport := Transport{Kind: "git", URL: rawURL, Revision: revision, ResolvedRevision: requested}
		cached, err := c.openCached(KindGit, fsx.Digest([]byte(rawURL+"\n"+requested)), transport, true)
		if err != nil {
			return Snapshot{}, err
		}
		if cached != nil {
			return *cached, nil
		}
	}
	resolved, err := resolveGitRevision(rawURL, revision)
	if err != nil {
		return Snapshot{}, err
	}
	transport := Transport{Kind: "git", URL: rawURL, Revision: revision, ResolvedRevision: resolved}
	cacheKey := fsx.Digest([]byte(rawURL + "\n" + resolved))
	return c.publish(KindGit, cacheKey, transport, func(stage string) (string, error) {
		content := filepath.Join(stage, "content")
		if err := runGit("clone", "--no-checkout", "--", rawURL, content); err != nil {
			return "", err
		}
		if err := runGit("-C", content, "checkout", "--detach", resolved); err != nil {
			return "", err
		}
		_ = os.RemoveAll(filepath.Join(content, ".git"))
		return "content", nil
	})
}

func (c *SnapshotCache) FetchArchive(rawURL, sha string) (Snapshot, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "file" && u.Scheme != "http" && u.Scheme != "https") {
		scheme := ""
		if u != nil {
			scheme = u.Scheme
		}
		return Snapshot{}, Errorf("Unsupported archive URL scheme: %s", scheme)
	}
	digest := strings.ToLower(sha)
	if !sha256Pattern.MatchString(digest) {
		return Snapshot{}, Errorf("Archive SHA-256 must be 64 hexadecimal characters.")
	}
	transport := Transport{Kind: "archive", URL: rawURL, SHA256: digest}
	return c.publish(KindArchive, digest, transport, func(stage string) (string, error) {
		data, err := readArchive(*u)
		if err != nil {
			return "", err
		}
		actual := fsx.Digest(data)
		if actual != digest {
			return "", Errorf("Archive checksum mismatch: expected %s, got %s.", digest, actual)
		}
		extraction := filepath.Join(stage, "extract")
		if err := os.MkdirAll(extraction, 0o755); err != nil {
			return "", err
		}
		if err := extractArchive(data, extraction); err != nil {
			return "", err
		}
		root, err := findSourceRoot(extraction)
		if err != nil {
			return "", err
		}
		relative, err := fsx.PosixRel(stage, root)
		if err != nil {
			return "", err
		}
		return relative, nil
	})
}

func (c *SnapshotCache) publish(kind Kind, cacheKey string, transport Transport, build func(string) (string, error)) (Snapshot, error) {
	cached, err := c.openCached(kind, cacheKey, transport, true)
	if err != nil {
		return Snapshot{}, err
	}
	if cached != nil {
		return *cached, nil
	}
	kindDir := filepath.Join(c.Directory, string(kind))
	staging := filepath.Join(kindDir, ".staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return Snapshot{}, Errorf("Cannot create source snapshot: %v", err)
	}
	stage := filepath.Join(staging, fsx.Nonce())
	target := filepath.Join(kindDir, cacheKey)
	defer func() {
		if fsx.Exists(stage) {
			_ = fsx.MakeWritable(stage)
			_ = os.RemoveAll(stage)
		}
	}()
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return Snapshot{}, Errorf("Cannot create source snapshot: %v", err)
	}
	root, err := build(stage)
	if err != nil {
		return Snapshot{}, err
	}
	if err := validateRelativeRoot(root); err != nil {
		return Snapshot{}, err
	}
	digest, err := treeDigest(stage)
	if err != nil {
		return Snapshot{}, err
	}
	marker := map[string]any{"version": 1, "root": root, "digest": digest}
	data, _ := json.Marshal(marker)
	if err := os.WriteFile(filepath.Join(stage, ".complete"), append(data, '\n'), 0o644); err != nil {
		return Snapshot{}, Errorf("Cannot create source snapshot: %v", err)
	}
	if err := os.Rename(stage, target); err != nil {
		winner, openErr := c.openCached(kind, cacheKey, transport, false)
		if openErr == nil && winner != nil {
			return *winner, nil
		}
		return Snapshot{}, Errorf("Cannot create source snapshot: %v", err)
	}
	if err := fsx.MakeReadOnly(target); err != nil {
		_ = discardInvalid(target)
		return Snapshot{}, Errorf("Cannot protect source snapshot: %v", err)
	}
	return Snapshot{CacheKey: cacheKey, Root: filepath.Join(target, fsx.FromUnix(root)), Transport: transport}, nil
}

func (c *SnapshotCache) openCached(kind Kind, cacheKey string, transport Transport, repair bool) (*Snapshot, error) {
	target := filepath.Join(c.Directory, string(kind), cacheKey)
	if !fsx.Exists(target) {
		return nil, nil
	}
	markerPath := filepath.Join(target, ".complete")
	if !fsx.IsFile(markerPath) {
		if repair {
			if err := discardInvalid(target); err != nil {
				return nil, err
			}
			return nil, nil
		}
		return nil, Errorf("Source snapshot is incomplete: %s", target)
	}
	var marker struct {
		Version int    `json:"version"`
		Root    string `json:"root"`
		Digest  string `json:"digest"`
	}
	data, err := os.ReadFile(markerPath)
	if err != nil || json.Unmarshal(data, &marker) != nil || marker.Version != 1 || !sha256Pattern.MatchString(marker.Digest) {
		if repair {
			_ = discardInvalid(target)
			return nil, nil
		}
		return nil, Errorf("Source snapshot marker is invalid: %s", target)
	}
	if err := validateRelativeRoot(marker.Root); err != nil {
		if repair {
			_ = discardInvalid(target)
			return nil, nil
		}
		return nil, err
	}
	actual, err := treeDigest(target)
	if err != nil || actual != marker.Digest {
		if repair {
			_ = discardInvalid(target)
			return nil, nil
		}
		return nil, Errorf("Source snapshot content changed: %s", target)
	}
	root := filepath.Join(target, fsx.FromUnix(marker.Root))
	if !fsx.IsDir(root) || !fsx.IsFile(filepath.Join(root, "juicyfruit-source.yaml")) {
		if repair {
			_ = discardInvalid(target)
			return nil, nil
		}
		return nil, Errorf("Source snapshot is invalid: %s", target)
	}
	_ = fsx.MakeReadOnly(target)
	return &Snapshot{CacheKey: cacheKey, Root: root, Transport: transport}, nil
}

func discardInvalid(target string) error {
	quarantine := fmt.Sprintf("%s.invalid-%s", target, fsx.Nonce())
	_ = fsx.MakeWritable(target)
	if err := os.Rename(target, quarantine); err != nil {
		return Errorf("Cannot replace invalid source snapshot: %v", err)
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return Errorf("Cannot replace invalid source snapshot: %v", err)
	}
	return nil
}

func treeDigest(root string) (string, error) {
	files, err := fsx.WalkFiles(root)
	if err != nil {
		return "", Errorf("Source snapshot contains a symbolic link: %s", root)
	}
	var builder strings.Builder
	for _, file := range files {
		if filepath.Clean(file) == filepath.Join(root, ".complete") {
			continue
		}
		relative, _ := fsx.PosixRel(root, file)
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		builder.WriteString(relative)
		builder.WriteByte(0)
		builder.Write(data)
		builder.WriteByte(0)
	}
	return fsx.Digest([]byte(builder.String())), nil
}

func resolveGitRevision(rawURL, revision string) (string, error) {
	result, err := runGitOutput("ls-remote", "--quiet", "--", rawURL)
	if err != nil {
		return "", err
	}
	type candidate struct{ commit, ref string }
	var candidates []candidate
	for _, line := range strings.Split(result, "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		ref := parts[1]
		if ref == revision || ref == "refs/heads/"+revision || ref == "refs/tags/"+revision+"^{}" || ref == "refs/tags/"+revision || (commitPattern.MatchString(strings.ToLower(revision)) && parts[0] == strings.ToLower(revision)) {
			candidates = append(candidates, candidate{parts[0], ref})
		}
	}
	if revision == "HEAD" {
		for _, line := range strings.Split(result, "\n") {
			parts := strings.Fields(line)
			if len(parts) == 2 && parts[1] == "HEAD" {
				candidates = append(candidates, candidate{parts[0], parts[1]})
			}
		}
	}
	if len(candidates) == 0 {
		return "", Errorf("Git revision was not found: %s", revision)
	}
	chosen := candidates[0]
	for _, item := range candidates {
		if strings.HasSuffix(item.ref, "^{}") {
			chosen = item
			break
		}
	}
	if !commitPattern.MatchString(chosen.commit) {
		return "", Errorf("Git revision did not resolve to a commit: %s", revision)
	}
	return chosen.commit, nil
}

func runGit(args ...string) error {
	_, err := runGitOutput(args...)
	return err
}

func runGitOutput(args ...string) (string, error) {
	full := append([]string{"-c", "core.longpaths=true"}, args...)
	cmd := exec.Command("git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" && len(args) > 0 {
			message = args[0]
		}
		return "", Errorf("Git command failed: %s", message)
	}
	return stdout.String(), nil
}

func readArchive(u url.URL) ([]byte, error) {
	if u.Scheme == "file" {
		path := u.Path
		if u.Host != "" && u.Host != "localhost" {
			path = filepath.Join(u.Host, u.Path)
		}
		decoded, err := url.PathUnescape(path)
		if err != nil {
			decoded = path
		}
		data, err := os.ReadFile(filepath.FromSlash(decoded))
		if err != nil {
			return nil, Errorf("Archive file does not exist: %s", filepath.FromSlash(decoded))
		}
		return data, nil
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}, Timeout: 60 * time.Second}
	response, err := client.Get(u.String())
	if err != nil {
		return nil, Errorf("Cannot download archive: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil, Errorf("Cannot download archive: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, Errorf("Cannot download archive: %v", err)
	}
	return data, nil
}

func extractArchive(data []byte, destination string) error {
	if len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] >= 3 && data[2] <= 7 {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return Errorf("Cannot decode archive: %v", err)
		}
		if len(reader.File) == 0 {
			return Errorf("Archive is empty.")
		}
		for _, entry := range reader.File {
			entryPath, err := ValidateArchiveEntryPath(entry.Name)
			if err != nil {
				return err
			}
			if entryPath == "" {
				continue
			}
			if entry.Mode()&os.ModeSymlink != 0 {
				return Errorf("Archive symbolic links are not supported: %s", entryPath)
			}
			output := filepath.Join(destination, fsx.FromUnix(entryPath))
			if entry.FileInfo().IsDir() {
				if err := os.MkdirAll(output, 0o755); err != nil {
					return err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
				return err
			}
			in, err := entry.Open()
			if err != nil {
				return err
			}
			out, err := os.Create(output)
			if err != nil {
				in.Close()
				return err
			}
			_, copyErr := io.Copy(out, in)
			in.Close()
			out.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	}
	var reader io.Reader = bytes.NewReader(data)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return Errorf("Cannot decode archive: %v", err)
		}
		defer gz.Close()
		reader = gz
	}
	tarReader := tar.NewReader(reader)
	found := false
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Errorf("Cannot decode archive: %v", err)
		}
		found = true
		entryPath, err := ValidateArchiveEntryPath(header.Name)
		if err != nil {
			return err
		}
		if entryPath == "" {
			continue
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return Errorf("Archive symbolic links are not supported: %s", entryPath)
		}
		output := filepath.Join(destination, fsx.FromUnix(entryPath))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(output, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		out, err := os.Create(output)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tarReader)
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	if !found {
		return Errorf("Archive is empty.")
	}
	return nil
}

func findSourceRoot(extraction string) (string, error) {
	if fsx.IsFile(filepath.Join(extraction, "juicyfruit-source.yaml")) {
		return extraction, nil
	}
	entries, err := os.ReadDir(extraction)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() && fsx.IsFile(filepath.Join(extraction, entry.Name(), "juicyfruit-source.yaml")) {
			candidates = append(candidates, filepath.Join(extraction, entry.Name()))
		}
	}
	if len(candidates) != 1 {
		return "", Errorf("Archive must contain one top-level juicyfruit-source.yaml.")
	}
	return candidates[0], nil
}

func validateRelativeRoot(value string) error {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || hasSegment(value, "..") {
		return Errorf("Source snapshot has an invalid root.")
	}
	return nil
}

func SortPackages(packages []Package) {
	sort.Slice(packages, func(i, j int) bool { return packages[i].Path < packages[j].Path })
}
