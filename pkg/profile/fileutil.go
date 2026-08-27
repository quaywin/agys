package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandTilde expands a leading ~/ or ~ in a path with the real user home directory.
func ExpandTilde(path string) string {
	if path == "" {
		return ""
	}
	if path == "~" {
		if realHome, err := GetRealUserHome(); err == nil {
			return realHome
		}
		return path
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if realHome, err := GetRealUserHome(); err == nil {
			return filepath.Join(realHome, path[2:])
		}
	}
	return path
}

// WriteFileAtomic writes data to a temporary file in the same target directory
// and then renames it atomically to the target filename.
// This prevents corrupted or truncated 0-byte files in case of sudden process interruption.
func WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".agys-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file in %s: %w", dir, err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write data to temporary file %s: %w", tmpName, err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temporary file %s: %w", tmpName, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file %s: %w", tmpName, err)
	}

	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("failed to set permissions on temporary file %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("failed to atomically rename %s to %s: %w", tmpName, filename, err)
	}

	return nil
}
