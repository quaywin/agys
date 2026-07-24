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
