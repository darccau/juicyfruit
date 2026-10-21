package fsx

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func Home() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("USERPROFILE")
	}
	return os.Getenv("HOME")
}

func UserRoot() string {
	if v := strings.TrimSpace(os.Getenv("JUICYFRUIT_USER_ROOT")); v != "" {
		return v
	}
	return Home()
}

func ProjectRoot() string {
	if v := strings.TrimSpace(os.Getenv("JUICYFRUIT_PROJECT_ROOT")); v != "" {
		return v
	}
	cwd, _ := os.Getwd()
	return cwd
}

func ConfigHome() string {
	if v := strings.TrimSpace(os.Getenv("JUICYFRUIT_CONFIG_HOME")); v != "" {
		return v
	}
	home := Home()
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "Juicyfruit")
		}
		return filepath.Join(home, "AppData", "Roaming", "Juicyfruit")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Juicyfruit")
	default:
		if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
			return filepath.Join(v, "juicyfruit")
		}
		return filepath.Join(home, ".config", "juicyfruit")
	}
}

func CacheHome() string {
	if v := strings.TrimSpace(os.Getenv("JUICYFRUIT_CACHE_HOME")); v != "" {
		return filepath.Join(v, "snapshots")
	}
	home := Home()
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("LOCALAPPDATA")
		if root == "" {
			root = os.Getenv("APPDATA")
		}
		return filepath.Join(root, "Juicyfruit", "snapshots")
	case "darwin":
		return filepath.Join(home, "Library", "Caches", "Juicyfruit", "snapshots")
	default:
		if v := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); v != "" {
			return filepath.Join(v, "juicyfruit", "snapshots")
		}
		return filepath.Join(home, ".cache", "juicyfruit", "snapshots")
	}
}

func ReadJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path, append(data, '\n'))
}

func EncodeJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func WriteStringAtomic(path, value string) error {
	return WriteAtomic(path, []byte(value))
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func DigestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

func Nonce() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d-%d-%s", os.Getpid(), time.Now().UnixMicro(), hex.EncodeToString(b[:]))
}

var safeRelative = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func SafeRelativePath(value string) bool {
	return value != "" && !strings.HasPrefix(value, "/") && !strings.Contains(value, "\\") &&
		!strings.Contains(value, "//") && !containsSegment(value, "..") && safeRelative.MatchString(value)
}

func SafeInstallPath(value string) bool {
	return value != "" && !strings.HasPrefix(value, "/") && !strings.Contains(value, "\\") && !containsSegment(value, "..")
}

func containsSegment(value, segment string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func Within(root, candidate string) bool {
	rootAbs, err1 := filepath.Abs(root)
	candidateAbs, err2 := filepath.Abs(candidate)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func ResolveDir(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("directory does not exist: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func ResolveFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("file does not exist: %s", path)
	}
	return filepath.EvalSymlinks(path)
}

func PosixRel(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func WalkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported: %s", path)
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func WithinOrEqual(root, path string) bool {
	r, err1 := filepath.Abs(root)
	p, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	return r == p || Within(r, p)
}

func UnixPath(path string) string {
	return filepath.ToSlash(path)
}

func FromUnix(path string) string {
	return filepath.FromSlash(path)
}

func MakeReadOnly(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() &^ 0o222
		if d.IsDir() {
			mode |= 0o500
		} else {
			mode |= 0o400
		}
		return os.Chmod(p, mode)
	})
}

func MakeWritable(path string) error {
	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() | 0o200
		if d.IsDir() {
			mode |= 0o700
		}
		return os.Chmod(p, mode)
	})
}
