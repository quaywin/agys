package profile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExportImport(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	srcName := "exp-profile"
	dstName := "imp-profile"

	// Create source profile
	srcDir, err := Create(srcName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Add files and directories inside source profile
	subDir := filepath.Join(srcDir, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	testFilePath := filepath.Join(subDir, "oauth-token")
	testContent := `{"token": {"access_token": "foo"}}`
	if err := os.WriteFile(testFilePath, []byte(testContent), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Export to buffer
	var buf bytes.Buffer
	if err := ExportProfile(srcName, &buf); err != nil {
		t.Fatalf("Export error: %v", err)
	}

	// Import under new name from buffer
	if err := ImportProfile(&buf, dstName, false); err != nil {
		t.Fatalf("Import error: %v", err)
	}

	// Verify imported profile exists and contains matching contents
	dstExists, dstDir, err := Exists(dstName)
	if err != nil {
		t.Fatalf("Exists dest error: %v", err)
	}
	if !dstExists {
		t.Fatalf("Expected imported profile to exist")
	}

	importedFilePath := filepath.Join(dstDir, ".gemini", "antigravity-cli", "oauth-token")
	data, err := os.ReadFile(importedFilePath)
	if err != nil {
		t.Fatalf("Failed to read imported file: %v", err)
	}
	if string(data) != testContent {
		t.Errorf("Expected content %q, got %q", testContent, string(data))
	}

	// Test overwrite fails if false
	var buf2 bytes.Buffer
	_ = ExportProfile(srcName, &buf2)
	err = ImportProfile(&buf2, dstName, false)
	if err == nil {
		t.Errorf("Expected import to fail on existing profile when overwrite=false")
	}

	// Test overwrite succeeds if true
	var buf3 bytes.Buffer
	_ = ExportProfile(srcName, &buf3)
	err = ImportProfile(&buf3, dstName, true)
	if err != nil {
		t.Errorf("Expected import to succeed on existing profile when overwrite=true: %v", err)
	}
}

func TestExportImportAll(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create two profiles
	p1Name := "profile-one"
	p2Name := "profile-two"
	dir1, _ := Create(p1Name)
	dir2, _ := Create(p2Name)

	_ = os.WriteFile(filepath.Join(dir1, "data1.txt"), []byte("content1"), 0600)
	_ = os.WriteFile(filepath.Join(dir2, "data2.txt"), []byte("content2"), 0600)

	// Export all
	var buf bytes.Buffer
	if err := ExportAll(&buf); err != nil {
		t.Fatalf("ExportAll error: %v", err)
	}

	// Clear profile-one and profile-two to test restoration
	_ = Delete(p1Name)
	_ = Delete(p2Name)

	// Import all
	if err := ImportAll(&buf, false); err != nil {
		t.Fatalf("ImportAll error: %v", err)
	}

	// Verify both profiles restored
	e1, d1, _ := Exists(p1Name)
	e2, d2, _ := Exists(p2Name)
	if !e1 || !e2 {
		t.Fatalf("Expected both profiles to be imported")
	}

	content1, _ := os.ReadFile(filepath.Join(d1, "data1.txt"))
	content2, _ := os.ReadFile(filepath.Join(d2, "data2.txt"))
	if string(content1) != "content1" || string(content2) != "content2" {
		t.Errorf("Restored contents do not match")
	}
}

func TestImportDirectoryTraversalProtection(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	dstName := "safe-profile"

	// Build a malicious in-memory tar archive trying to escape to "../../malicious.txt"
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	header := &tar.Header{
		Name:     "safe-profile/../../../malicious.txt",
		Mode:     0600,
		Size:     12,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("Failed to write header: %v", err)
	}
	if _, err := tw.Write([]byte("evil content")); err != nil {
		t.Fatalf("Failed to write body: %v", err)
	}

	tw.Close()
	gw.Close()

	// Import should fail with a directory traversal error
	err := ImportProfile(&buf, dstName, false)
	if err == nil {
		t.Fatalf("Expected directory traversal check to block import, but it succeeded")
	}
	t.Logf("Blocked malicious import as expected: %v", err)
}
