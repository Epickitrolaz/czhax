package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repo = "Epickitrolaz/czhax"

type releaseInfo struct {
	TagName string `json:"tag_name"`
}

type Result struct {
	CurrentVersion string
	LatestVersion  string
	Updated        bool
	ExePath        string
}

func AssetName(goos, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("self-update supports Linux binaries only (got %s)", goos)
	}
	switch goarch {
	case "arm64":
		return "czhax-linux-arm64", nil
	case "arm":
		return "czhax-linux-armv7", nil
	case "amd64":
		return "czhax-linux-amd64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}

func ParseChecksum(checksumsBody, asset string) (string, bool) {
	for _, line := range strings.Split(checksumsBody, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func ExecutablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	return exePath, nil
}

func NeedsSudo() bool {
	if os.Geteuid() == 0 {
		return false
	}
	exePath, err := ExecutablePath()
	if err != nil {
		return false
	}
	dir := filepath.Dir(exePath)
	f, err := os.CreateTemp(dir, ".czhax-perm-*")
	if err != nil {
		return os.IsPermission(err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return false
}

func CheckAndUpdate(currentVersion string, force bool) (Result, error) {
	res := Result{CurrentVersion: currentVersion}
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return res, err
	}

	exePath, err := ExecutablePath()
	if err != nil {
		return res, err
	}
	res.ExePath = exePath

	client := &http.Client{Timeout: 45 * time.Second}
	latestTag, err := fetchLatestTag(client)
	if err != nil {
		return res, err
	}
	res.LatestVersion = latestTag

	if !force && currentVersion == latestTag {
		return res, nil
	}

	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, latestTag)
	binBytes, err := fetchBytes(client, baseURL+"/"+asset)
	if err != nil {
		return res, fmt.Errorf("download %s: %w", asset, err)
	}

	csBytes, err := fetchBytes(client, baseURL+"/checksums.txt")
	if err != nil {
		return res, fmt.Errorf("download checksums.txt: %w", err)
	}
	expected, ok := ParseChecksum(string(csBytes), asset)
	if !ok {
		return res, fmt.Errorf("checksum entry for %s not found", asset)
	}

	sum := sha256.Sum256(binBytes)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return res, fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", asset, expected, actual)
	}

	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".czhax-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return res, fmt.Errorf("permission denied writing to %s (run: sudo czhax update)", dir)
		}
		return res, fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(binBytes); err != nil {
		_ = tmp.Close()
		return res, err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return res, err
	}
	if err := tmp.Close(); err != nil {
		return res, err
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		if os.IsPermission(err) {
			return res, fmt.Errorf("permission denied replacing %s (run: sudo czhax update)", exePath)
		}
		return res, err
	}
	cleanup = false
	res.Updated = true
	return res, nil
}

func Run(currentVersion string, force bool) error {
	res, err := CheckAndUpdate(currentVersion, force)
	if err != nil {
		return err
	}
	if !res.Updated {
		fmt.Printf("czhax is already up to date (%s).\n", res.CurrentVersion)
		return nil
	}
	fmt.Printf("Updated %s (%s -> %s, SHA256 verified).\n", res.ExePath, res.CurrentVersion, res.LatestVersion)
	return nil
}

func fetchLatestTag(client *http.Client) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "czhax-updater")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("query latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var rel releaseInfo
		if err := json.NewDecoder(resp.Body).Decode(&rel); err == nil && rel.TagName != "" {
			return rel.TagName, nil
		}
	}

	webURL := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	r2, err := client.Get(webURL)
	if err != nil {
		return "", fmt.Errorf("resolve latest tag: %w", err)
	}
	defer r2.Body.Close()
	if r2.Request != nil && r2.Request.URL != nil {
		parts := strings.Split(strings.TrimSuffix(r2.Request.URL.Path, "/"), "/")
		tag := parts[len(parts)-1]
		if tag != "" && tag != "latest" {
			return tag, nil
		}
	}
	return "", fmt.Errorf("could not resolve latest release tag (HTTP %d)", resp.StatusCode)
}

func fetchBytes(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "czhax-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
