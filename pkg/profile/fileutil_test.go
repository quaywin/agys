package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic_Success(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "sub", "config.json")
	content := []byte(`{"key": "value"}`)

	err := WriteFileAtomic(targetFile, content, 0600)
	if err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}

	readData, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read back target file: %v", err)
	}

	if string(readData) != string(content) {
		t.Errorf("expected content %q, got %q", string(content), string(readData))
	}

	info, err := os.Stat(targetFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("expected perm 0600, got %o", info.Mode().Perm())
	}
}

func TestWriteFileAtomic_Overwrite(t *testing.T) {
	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "config.txt")

	initialData := []byte("v1")
	if err := WriteFileAtomic(targetFile, initialData, 0600); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	newData := []byte("v2-updated")
	if err := WriteFileAtomic(targetFile, newData, 0600); err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}

	readData, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(readData) != "v2-updated" {
		t.Errorf("expected overwritten content %q, got %q", "v2-updated", string(readData))
	}
}

func TestGetRealUserHome(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	home, err := GetRealUserHome()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if home != tempHome {
		t.Errorf("expected %q, got %q", tempHome, home)
	}

	// Test when HOME points inside a profile directory
	profileHome := filepath.Join(tempHome, ".agys", "profiles", "testprof")
	t.Setenv("HOME", profileHome)

	homeFromProfile, err := GetRealUserHome()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if homeFromProfile != tempHome {
		t.Errorf("expected stripped home %q, got %q", tempHome, homeFromProfile)
	}

	// Test AGYS_REAL_HOME env override
	overrideHome := filepath.Join(tempHome, "custom_real_home")
	t.Setenv("AGYS_REAL_HOME", overrideHome)

	homeOverride, err := GetRealUserHome()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if homeOverride != overrideHome {
		t.Errorf("expected %q, got %q", overrideHome, homeOverride)
	}
}

func TestExpandTilde(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_REAL_HOME", tempHome)

	if res := ExpandTilde("~/Projects_1/agys"); res != filepath.Join(tempHome, "Projects_1", "agys") {
		t.Errorf("unexpected expansion: %s", res)
	}

	if res := ExpandTilde("~"); res != tempHome {
		t.Errorf("unexpected expansion for ~: %s", res)
	}

	if res := ExpandTilde("/absolute/path"); res != "/absolute/path" {
		t.Errorf("absolute path should remain unchanged: %s", res)
	}
}
