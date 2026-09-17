package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/quaywin/agys/pkg/profile"
	"github.com/quaywin/agys/pkg/version"
)

const (
	// DefaultCheckInterval is the standard throttle duration between update checks.
	DefaultCheckInterval = 6 * time.Hour

	timestampFilename = "last_check.timestamp"
	lockFilename      = "update.lock"
	statusFilename    = "update_status.json"
	logFilename       = "updater.log"
)

// UpdateStatus represents the recorded state of the background updater.
type UpdateStatus struct {
	LastCheckTime time.Time `json:"last_check_time"`
	Status        string    `json:"status"` // "up_to_date", "updated", "error"
	CurrentVer    string    `json:"current_version,omitempty"`
	LatestVer     string    `json:"latest_version,omitempty"`
	OldVer        string    `json:"old_version,omitempty"`
	NewVer        string    `json:"new_version,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	Error         string    `json:"error,omitempty"`
	Notified      bool      `json:"notified"`
}

// GetUpdaterDir returns ~/.agys/updater (or $AGYS_DIR/updater) and ensures it exists.
func GetUpdaterDir() (string, error) {
	agysDir, err := profile.GetAgysDir()
	if err != nil {
		home, hErr := os.UserHomeDir()
		if hErr != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		agysDir = filepath.Join(home, ".agys")
	}
	updaterDir := filepath.Join(agysDir, "updater")
	if err := os.MkdirAll(updaterDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create updater dir: %w", err)
	}
	return updaterDir, nil
}

// GetCheckInterval returns the configured interval or DefaultCheckInterval.
func GetCheckInterval() time.Duration {
	if custom := os.Getenv("AGYS_UPDATE_INTERVAL"); custom != "" {
		if d, err := time.ParseDuration(custom); err == nil && d > 0 {
			return d
		}
	}
	return DefaultCheckInterval
}

// ShouldTriggerAutoUpdate evaluates guards to decide if a background check should run.
func ShouldTriggerAutoUpdate(cmdName string) bool {
	// 1. Guard against hook processes, updater worker itself, completion, and manual commands
	if profile.IsHookProcess() {
		return false
	}

	switch cmdName {
	case "__bg-updater", "herdr-hook", "completion", "__complete", "upgrade", "update", "help", "version", "alias":
		return false
	}

	// 2. Global environment disable flags
	if os.Getenv("AGYS_NO_AUTO_UPDATE") == "1" || os.Getenv("AGYS_AUTO_UPDATE") == "off" || os.Getenv("CI") != "" {
		return false
	}

	// 3. Safety guard: never overwrite local development builds (-dev)
	cleanVer := CleanVersion(version.Version)
	if strings.Contains(strings.ToLower(cleanVer), "dev") && os.Getenv("AGYS_AUTO_UPDATE_DEV") != "1" {
		return false
	}

	// 4. Fast-path throttling check using file mtime
	updaterDir, err := GetUpdaterDir()
	if err != nil {
		return false
	}

	timestampPath := filepath.Join(updaterDir, timestampFilename)
	fi, err := os.Stat(timestampPath)
	if err == nil {
		interval := GetCheckInterval()
		if time.Since(fi.ModTime()) < interval {
			return false // Fast path: skipping update
		}
	}

	return true
}

// TouchTimestamp touches ~/.agys/updater/last_check.timestamp to record check time.
func TouchTimestamp(dir string) error {
	timestampPath := filepath.Join(dir, timestampFilename)
	now := time.Now()
	if err := os.Chtimes(timestampPath, now, now); err != nil {
		f, cErr := os.OpenFile(timestampPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if cErr != nil {
			return cErr
		}
		_ = f.Close()
	}
	return nil
}

// MaybeTriggerBackgroundUpdate initiates a detached background updater process if due.
func MaybeTriggerBackgroundUpdate(cmdName string) {
	if !ShouldTriggerAutoUpdate(cmdName) {
		return
	}

	updaterDir, err := GetUpdaterDir()
	if err != nil {
		return
	}

	// Touch timestamp immediately to prevent concurrent shell commands from spawning duplicate updaters
	_ = TouchTimestamp(updaterDir)

	execPath, err := os.Executable()
	if err != nil {
		return
	}
	resolved, err := filepath.EvalSymlinks(execPath)
	if err == nil && resolved != "" {
		execPath = resolved
	}

	logFile := ""
	if os.Getenv("AGYS_DEBUG") == "1" || os.Getenv("AGYS_UPDATER_LOG") == "1" {
		logFile = filepath.Join(updaterDir, logFilename)
	}

	_ = SpawnDetachedProcess(execPath, []string{"__bg-updater"}, logFile)
}

// ReadUpdateStatus loads the status json from ~/.agys/updater/update_status.json.
func ReadUpdateStatus(dir string) (*UpdateStatus, error) {
	statusFile := filepath.Join(dir, statusFilename)
	data, err := os.ReadFile(statusFile)
	if err != nil {
		return nil, err
	}
	var st UpdateStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// WriteUpdateStatus saves the status json atomically.
func WriteUpdateStatus(dir string, status *UpdateStatus) error {
	statusFile := filepath.Join(dir, statusFilename)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return profile.WriteFileAtomic(statusFile, data, 0600)
}

// RunBackgroundWorker is executed by `agys __bg-updater` in the detached background process.
func RunBackgroundWorker() error {
	updaterDir, err := GetUpdaterDir()
	if err != nil {
		return err
	}

	// 1. Acquire non-blocking file lock to guarantee single worker instance
	lockPath := filepath.Join(updaterDir, lockFilename)
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLock()
	if err != nil || !locked {
		// Another background updater is currently active; exit silently
		return nil
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	_ = TouchTimestamp(updaterDir)

	// 2. Fetch latest release from GitHub
	rel, err := FetchLatestRelease(DefaultRepoOwner, DefaultRepoName)
	if err != nil {
		st := &UpdateStatus{
			LastCheckTime: time.Now(),
			Status:        "error",
			Error:         err.Error(),
		}
		_ = WriteUpdateStatus(updaterDir, st)
		return err
	}

	currentVer := version.Version
	latestVer := CleanVersion(rel.TagName)

	if !IsNewer(currentVer, latestVer) {
		st := &UpdateStatus{
			LastCheckTime: time.Now(),
			Status:        "up_to_date",
			CurrentVer:    CleanVersion(currentVer),
			LatestVer:     latestVer,
		}
		_ = WriteUpdateStatus(updaterDir, st)
		return nil
	}

	// 3. Find matching asset for runtime OS/Arch
	expectedArchive := GetArchiveName(latestVer, runtime.GOOS, runtime.GOARCH)
	var downloadURL string

	for _, asset := range rel.Assets {
		if asset.Name == expectedArchive {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		for _, asset := range rel.Assets {
			nameLower := strings.ToLower(asset.Name)
			if strings.Contains(nameLower, runtime.GOOS) && strings.Contains(nameLower, runtime.GOARCH) && strings.HasSuffix(nameLower, ".tar.gz") {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}
	}

	if downloadURL == "" {
		errNotFound := fmt.Errorf("no release asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
		st := &UpdateStatus{
			LastCheckTime: time.Now(),
			Status:        "error",
			Error:         errNotFound.Error(),
			CurrentVer:    CleanVersion(currentVer),
			LatestVer:     latestVer,
		}
		_ = WriteUpdateStatus(updaterDir, st)
		return errNotFound
	}

	// 4. Download and extract binary
	extractedBinary, err := DownloadAndExtractBinary(downloadURL)
	if err != nil {
		st := &UpdateStatus{
			LastCheckTime: time.Now(),
			Status:        "error",
			Error:         err.Error(),
			CurrentVer:    CleanVersion(currentVer),
			LatestVer:     latestVer,
		}
		_ = WriteUpdateStatus(updaterDir, st)
		return err
	}
	defer os.RemoveAll(filepath.Dir(extractedBinary))

	// 5. Atomically replace existing binary
	if err := InstallBinary(extractedBinary); err != nil {
		st := &UpdateStatus{
			LastCheckTime: time.Now(),
			Status:        "error",
			Error:         err.Error(),
			CurrentVer:    CleanVersion(currentVer),
			LatestVer:     latestVer,
		}
		_ = WriteUpdateStatus(updaterDir, st)
		return err
	}

	// 6. Record successful upgrade
	st := &UpdateStatus{
		LastCheckTime: time.Now(),
		Status:        "updated",
		OldVer:        CleanVersion(currentVer),
		NewVer:        latestVer,
		UpdatedAt:     time.Now(),
		Notified:      false,
	}
	_ = WriteUpdateStatus(updaterDir, st)
	return nil
}

// NotifyIfRecentlyUpdated checks ~/.agys/updater/update_status.json and prints a discreet notification once.
func NotifyIfRecentlyUpdated(cmdName string) {
	if profile.IsHookProcess() {
		return
	}

	// Suppress notifications during completion, hook, help, version, alias, and updater commands
	switch cmdName {
	case "__bg-updater", "herdr-hook", "completion", "__complete", "help", "version", "upgrade", "update", "alias":
		return
	}

	updaterDir, err := GetUpdaterDir()
	if err != nil {
		return
	}

	st, err := ReadUpdateStatus(updaterDir)
	if err != nil || st == nil {
		return
	}

	if st.Status == "updated" && !st.Notified && st.NewVer != "" {
		fmt.Fprintf(os.Stderr, "[agys] Automatically updated to v%s in background! Run 'agys changelog' to view release notes.\n", st.NewVer)
		st.Notified = true
		_ = WriteUpdateStatus(updaterDir, st)
	}
}
