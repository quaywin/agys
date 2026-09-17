package updater

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/quaywin/agys/pkg/version"
)

func TestGetUpdaterDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)

	dir, err := GetUpdaterDir()
	if err != nil {
		t.Fatalf("unexpected error from GetUpdaterDir: %v", err)
	}

	expected := filepath.Join(tmpDir, "updater")
	if dir != expected {
		t.Errorf("GetUpdaterDir() = %q, want %q", dir, expected)
	}

	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected updater dir to exist on disk: %v", err)
	}
	if !fi.IsDir() {
		t.Errorf("expected %q to be a directory", dir)
	}
}

func TestGetCheckInterval(t *testing.T) {
	// Default
	t.Setenv("AGYS_UPDATE_INTERVAL", "")
	if got := GetCheckInterval(); got != DefaultCheckInterval {
		t.Errorf("GetCheckInterval() = %v, want %v", got, DefaultCheckInterval)
	}

	// Custom valid
	t.Setenv("AGYS_UPDATE_INTERVAL", "2h")
	if got := GetCheckInterval(); got != 2*time.Hour {
		t.Errorf("GetCheckInterval() with 2h = %v, want 2h", got)
	}

	// Custom invalid fallback
	t.Setenv("AGYS_UPDATE_INTERVAL", "invalid")
	if got := GetCheckInterval(); got != DefaultCheckInterval {
		t.Errorf("GetCheckInterval() with invalid = %v, want %v", got, DefaultCheckInterval)
	}
}

func TestShouldTriggerAutoUpdate_CommandExclusions(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = origVer }()

	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)
	t.Setenv("AGYS_NO_AUTO_UPDATE", "")
	t.Setenv("AGYS_AUTO_UPDATE", "")
	t.Setenv("CI", "")

	excluded := []string{"__bg-updater", "herdr-hook", "completion", "__complete", "upgrade", "update", "help", "version", "alias"}
	for _, cmd := range excluded {
		if ShouldTriggerAutoUpdate(cmd) {
			t.Errorf("ShouldTriggerAutoUpdate(%q) = true, want false (excluded command)", cmd)
		}
	}
}

func TestShouldTriggerAutoUpdate_EnvDisables(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = origVer }()

	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)

	// AGYS_NO_AUTO_UPDATE=1
	t.Setenv("AGYS_NO_AUTO_UPDATE", "1")
	t.Setenv("AGYS_AUTO_UPDATE", "")
	t.Setenv("CI", "")
	if ShouldTriggerAutoUpdate("list") {
		t.Errorf("ShouldTriggerAutoUpdate() should be false when AGYS_NO_AUTO_UPDATE=1")
	}

	// AGYS_AUTO_UPDATE=off
	t.Setenv("AGYS_NO_AUTO_UPDATE", "")
	t.Setenv("AGYS_AUTO_UPDATE", "off")
	if ShouldTriggerAutoUpdate("list") {
		t.Errorf("ShouldTriggerAutoUpdate() should be false when AGYS_AUTO_UPDATE=off")
	}

	// CI=true
	t.Setenv("AGYS_AUTO_UPDATE", "")
	t.Setenv("CI", "true")
	if ShouldTriggerAutoUpdate("list") {
		t.Errorf("ShouldTriggerAutoUpdate() should be false in CI")
	}
}

func TestShouldTriggerAutoUpdate_DevVersionGuard(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)
	t.Setenv("AGYS_NO_AUTO_UPDATE", "")
	t.Setenv("AGYS_AUTO_UPDATE", "")
	t.Setenv("CI", "")
	t.Setenv("AGYS_AUTO_UPDATE_DEV", "")

	origVer := version.Version
	defer func() { version.Version = origVer }()

	version.Version = "0.2.0-dev"
	if ShouldTriggerAutoUpdate("status") {
		t.Errorf("ShouldTriggerAutoUpdate() should be false for dev version %q", version.Version)
	}

	// With AGYS_AUTO_UPDATE_DEV=1 override
	t.Setenv("AGYS_AUTO_UPDATE_DEV", "1")
	if !ShouldTriggerAutoUpdate("status") {
		t.Errorf("ShouldTriggerAutoUpdate() should be true when AGYS_AUTO_UPDATE_DEV=1 is explicitly set")
	}
}

func TestShouldTriggerAutoUpdate_FastPathThrottling(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	defer func() { version.Version = origVer }()

	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)
	t.Setenv("AGYS_NO_AUTO_UPDATE", "")
	t.Setenv("AGYS_AUTO_UPDATE", "")
	t.Setenv("CI", "")

	updaterDir := filepath.Join(tmpDir, "updater")
	_ = os.MkdirAll(updaterDir, 0700)

	// Case 1: No timestamp file -> should trigger
	if !ShouldTriggerAutoUpdate("status") {
		t.Errorf("expected ShouldTriggerAutoUpdate() to be true when timestamp file does not exist")
	}

	// Case 2: Timestamp touched recently -> fast path skips
	if err := TouchTimestamp(updaterDir); err != nil {
		t.Fatalf("TouchTimestamp failed: %v", err)
	}
	if ShouldTriggerAutoUpdate("status") {
		t.Errorf("expected ShouldTriggerAutoUpdate() to be false (fast path skip) after TouchTimestamp")
	}

	// Case 3: Timestamp is older than check interval -> should trigger
	timestampFile := filepath.Join(updaterDir, timestampFilename)
	oldTime := time.Now().Add(-7 * time.Hour)
	_ = os.Chtimes(timestampFile, oldTime, oldTime)

	if !ShouldTriggerAutoUpdate("status") {
		t.Errorf("expected ShouldTriggerAutoUpdate() to be true when timestamp is older than 6h interval")
	}
}

func TestReadWriteUpdateStatus(t *testing.T) {
	tmpDir := t.TempDir()
	updaterDir := filepath.Join(tmpDir, "updater")
	_ = os.MkdirAll(updaterDir, 0700)

	now := time.Now().Truncate(time.Second)
	status := &UpdateStatus{
		LastCheckTime: now,
		Status:        "updated",
		OldVer:        "0.1.0",
		NewVer:        "0.2.0",
		UpdatedAt:     now,
		Notified:      false,
	}

	if err := WriteUpdateStatus(updaterDir, status); err != nil {
		t.Fatalf("WriteUpdateStatus failed: %v", err)
	}

	read, err := ReadUpdateStatus(updaterDir)
	if err != nil {
		t.Fatalf("ReadUpdateStatus failed: %v", err)
	}

	if read.Status != "updated" {
		t.Errorf("read.Status = %q, want 'updated'", read.Status)
	}
	if read.NewVer != "0.2.0" {
		t.Errorf("read.NewVer = %q, want '0.2.0'", read.NewVer)
	}
	if read.OldVer != "0.1.0" {
		t.Errorf("read.OldVer = %q, want '0.1.0'", read.OldVer)
	}
	if read.Notified != false {
		t.Errorf("read.Notified = %v, want false", read.Notified)
	}
}

func TestNotifyIfRecentlyUpdated(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)
	updaterDir := filepath.Join(tmpDir, "updater")
	_ = os.MkdirAll(updaterDir, 0700)

	st := &UpdateStatus{
		LastCheckTime: time.Now(),
		Status:        "updated",
		NewVer:        "0.3.0",
		OldVer:        "0.2.0",
		Notified:      false,
	}
	if err := WriteUpdateStatus(updaterDir, st); err != nil {
		t.Fatalf("failed to write test status: %v", err)
	}

	// Capture stderr
	oldStderr := os.Stderr

	// Test suppression on excluded commands (herdr-hook, completion)
	rSuppressed, wSuppressed, _ := os.Pipe()
	os.Stderr = wSuppressed
	NotifyIfRecentlyUpdated("herdr-hook")
	wSuppressed.Close()
	os.Stderr = oldStderr

	var bufSuppressed bytes.Buffer
	_, _ = io.Copy(&bufSuppressed, rSuppressed)
	if bufSuppressed.Len() > 0 {
		t.Errorf("expected no output for excluded command 'herdr-hook', got: %q", bufSuppressed.String())
	}

	// Active command (e.g. status) should trigger notification
	r, w, _ := os.Pipe()
	os.Stderr = w

	NotifyIfRecentlyUpdated("status")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "Automatically updated to v0.3.0 in background") {
		t.Errorf("expected notification output, got: %q", output)
	}

	// Verify status was marked notified
	stAfter, err := ReadUpdateStatus(updaterDir)
	if err != nil {
		t.Fatalf("ReadUpdateStatus failed: %v", err)
	}
	if !stAfter.Notified {
		t.Errorf("expected stAfter.Notified to be true after notification was displayed")
	}

	// Second invocation should produce no output
	r2, w2, _ := os.Pipe()
	os.Stderr = w2
	NotifyIfRecentlyUpdated("status")
	w2.Close()
	os.Stderr = oldStderr

	var buf2 bytes.Buffer
	_, _ = io.Copy(&buf2, r2)
	if buf2.Len() > 0 {
		t.Errorf("expected no output on second call, got: %q", buf2.String())
	}
}

func TestBackgroundWorker_LockExclusion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)
	updaterDir := filepath.Join(tmpDir, "updater")
	_ = os.MkdirAll(updaterDir, 0700)

	lockPath := filepath.Join(updaterDir, lockFilename)
	externalLock := flock.New(lockPath)
	locked, err := externalLock.TryLock()
	if err != nil || !locked {
		t.Fatalf("failed to hold external test lock: %v", err)
	}
	defer func() { _ = externalLock.Unlock() }()

	// Calling RunBackgroundWorker should immediately return nil without blocking
	done := make(chan error, 1)
	go func() {
		done <- RunBackgroundWorker()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error when worker detects lock collision, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunBackgroundWorker hung when lock was held by another process")
	}
}
