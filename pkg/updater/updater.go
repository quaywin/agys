package updater

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRepoOwner = "quaywin"
	DefaultRepoName  = "agys"
)

// Asset represents a release asset on GitHub.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Release represents GitHub release metadata.
type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
	Body       string  `json:"body"`
}

// FetchLatestRelease retrieves the latest release metadata from GitHub API.
func FetchLatestRelease(owner, repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create release request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agys-cli-updater")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error while fetching release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP status %d (%s)", resp.StatusCode, resp.Status)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to parse release metadata: %w", err)
	}

	return &rel, nil
}

// CleanVersion strips leading 'v' or whitespace from version string.
func CleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// CompareVersions compares two semantic version strings.
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2.
func CompareVersions(v1, v2 string) int {
	clean1 := CleanVersion(v1)
	clean2 := CleanVersion(v2)

	if clean1 == clean2 {
		return 0
	}

	// Handle dev versions
	if strings.Contains(clean1, "dev") && !strings.Contains(clean2, "dev") {
		return -1
	}
	if !strings.Contains(clean1, "dev") && strings.Contains(clean2, "dev") {
		return 1
	}

	// Split by hyphens first to handle pre-releases
	parts1 := strings.SplitN(clean1, "-", 2)
	parts2 := strings.SplitN(clean2, "-", 2)

	numParts1 := strings.Split(parts1[0], ".")
	numParts2 := strings.Split(parts2[0], ".")

	maxLen := len(numParts1)
	if len(numParts2) > maxLen {
		maxLen = len(numParts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(numParts1) {
			n1, _ = strconv.Atoi(numParts1[i])
		}
		if i < len(numParts2) {
			n2, _ = strconv.Atoi(numParts2[i])
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	return 0
}

// IsNewer checks whether the target version is newer than the current version.
func IsNewer(current, latest string) bool {
	return CompareVersions(current, latest) < 0
}

// GetArchiveName generates the expected archive filename for GoReleaser artifacts.
func GetArchiveName(version, osName, archName string) string {
	cleanVer := CleanVersion(version)
	return fmt.Sprintf("agys_%s_%s_%s.tar.gz", cleanVer, osName, archName)
}

// DownloadAndExtractBinary downloads the tar.gz archive from downloadURL and extracts the binary to a temp file.
func DownloadAndExtractBinary(downloadURL string) (string, error) {
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "agys-cli-updater")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download release archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with HTTP status %d (%s)", resp.StatusCode, resp.Status)
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to initialize gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	binaryName := "agys"
	if runtime.GOOS == "windows" {
		binaryName = "agys.exe"
	}

	tmpDir, err := os.MkdirTemp("", "agys-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("error reading tar archive: %w", err)
		}

		if filepath.Base(header.Name) == binaryName && header.Typeflag == tar.TypeReg {
			extractedFile := filepath.Join(tmpDir, binaryName)
			outFile, err := os.OpenFile(extractedFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				os.RemoveAll(tmpDir)
				return "", fmt.Errorf("failed to create extracted file: %w", err)
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				os.RemoveAll(tmpDir)
				return "", fmt.Errorf("failed to write binary content: %w", err)
			}
			outFile.Close()

			return extractedFile, nil
		}
	}

	os.RemoveAll(tmpDir)
	return "", fmt.Errorf("binary %q not found in release archive", binaryName)
}

// InstallBinary replaces the currently running executable with newBinaryPath.
func InstallBinary(newBinaryPath string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate current executable path: %w", err)
	}

	// Resolve symlinks if current binary is symlinked
	resolvedPath, err := filepath.EvalSymlinks(execPath)
	if err == nil && resolvedPath != "" {
		execPath = resolvedPath
	}

	targetDir := filepath.Dir(execPath)
	targetFile := filepath.Base(execPath)

	// Create temp file in the same directory as target for atomic rename
	tmpFile, err := os.CreateTemp(targetDir, "."+targetFile+".new-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied writing to %s. Please re-run with: sudo agys upgrade", targetDir)
		}
		return fmt.Errorf("failed to create temporary binary file in %s: %w", targetDir, err)
	}
	tmpPath := tmpFile.Name()

	srcFile, err := os.Open(newBinaryPath)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to open source binary: %w", err)
	}
	defer srcFile.Close()

	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to copy new binary data: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync binary contents to disk: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set executable permissions: %w", err)
	}

	// Move new binary to target via background command after process exits to prevent macOS kernel SIGKILL
	if runtime.GOOS == "windows" {
		oldPath := execPath + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(execPath, oldPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to rename current binary: %w", err)
		}
		if err := os.Rename(tmpPath, execPath); err != nil {
			_ = os.Rename(oldPath, execPath)
			os.Remove(tmpPath)
			return fmt.Errorf("failed to replace binary: %w", err)
		}
		_ = os.Remove(oldPath)
	} else {
		if err := os.Rename(tmpPath, execPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to replace binary: %w", err)
		}
		if runtime.GOOS == "darwin" {
			_ = exec.Command("codesign", "-f", "-s", "-", execPath).Run()
		}
	}

	return nil
}
