package profile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Clone duplicates an existing profile under a new profile name.
func Clone(srcName, dstName string) error {
	if err := ValidateName(srcName); err != nil {
		return fmt.Errorf("invalid source profile name: %w", err)
	}
	if err := ValidateName(dstName); err != nil {
		return fmt.Errorf("invalid destination profile name: %w", err)
	}

	srcExists, srcDir, err := Exists(srcName)
	if err != nil {
		return err
	}
	if !srcExists {
		return fmt.Errorf("source profile %q does not exist", srcName)
	}

	dstExists, dstDir, err := Exists(dstName)
	if err != nil {
		return err
	}
	if dstExists {
		return fmt.Errorf("destination profile %q already exists", dstName)
	}

	return copyDir(srcDir, dstDir)
}

func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				return err
			}

			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(srcPath)
				if err != nil {
					return err
				}
				if err := os.Symlink(linkTarget, dstPath); err != nil {
					return err
				}
			} else {
				if err := copyFile(srcPath, dstPath); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return nil
}
