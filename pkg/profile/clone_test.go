package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClone(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	srcName := "source-profile"
	dstName := "dest-profile"

	// Setup source profile
	srcDir, err := Create(srcName)
	if err != nil {
		t.Fatalf("Create source profile error: %v", err)
	}

	// Create a dummy file in source profile
	subDir := filepath.Join(srcDir, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatalf("Failed to create subdirectories: %v", err)
	}
	testFilePath := filepath.Join(subDir, "test.txt")
	testContent := "hello clone world"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Clone profile
	if err := Clone(srcName, dstName); err != nil {
		t.Fatalf("Clone error: %v", err)
	}

	// Verify destination exists and contains the cloned file
	dstExists, dstDir, err := Exists(dstName)
	if err != nil {
		t.Fatalf("Exists dest error: %v", err)
	}
	if !dstExists {
		t.Fatalf("Expected destination profile to exist")
	}

	clonedFilePath := filepath.Join(dstDir, ".gemini", "antigravity-cli", "test.txt")
	data, err := os.ReadFile(clonedFilePath)
	if err != nil {
		t.Fatalf("Failed to read cloned file: %v", err)
	}
	if string(data) != testContent {
		t.Errorf("Expected content %q, got %q", testContent, string(data))
	}

	// Test: cloning to an already existing destination should fail
	err = Clone(srcName, dstName)
	if err == nil {
		t.Errorf("Expected error when cloning to an existing destination profile")
	}

	// Test: cloning a non-existent source profile should fail
	err = Clone("non-existent", "another-dst")
	if err == nil {
		t.Errorf("Expected error when cloning non-existent source profile")
	}
}
