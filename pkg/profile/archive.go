package profile

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExportProfile packages a profile directory into a gzipped tar archive.
func ExportProfile(profileName string, writer io.Writer) error {
	exists, profileDir, err := Exists(profileName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("profile %q does not exist", profileName)
	}

	gw := gzip.NewWriter(writer)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	baseDir := filepath.Dir(profileDir)
	return filepath.Walk(profileDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}

		relPath = filepath.ToSlash(relPath)

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		header.Name = relPath

		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			header.Linkname = linkTarget
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}

		return nil
	})
}

// ImportProfile extracts a gzipped tar archive into the profiles base directory under the target profile name.
func ImportProfile(reader io.Reader, targetProfileName string, overwrite bool) error {
	if err := ValidateName(targetProfileName); err != nil {
		return fmt.Errorf("invalid target profile name: %w", err)
	}

	exists, profileDir, err := Exists(targetProfileName)
	if err != nil {
		return err
	}
	if exists {
		if !overwrite {
			return fmt.Errorf("profile %q already exists", targetProfileName)
		}
		if err := os.RemoveAll(profileDir); err != nil {
			return fmt.Errorf("failed to remove existing profile directory: %w", err)
		}
	}

	targetDir, err := GetProfileDir(targetProfileName)
	if err != nil {
		return err
	}

	gr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return fmt.Errorf("failed to create target profile directory: %w", err)
	}

	var rootPrefix string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading tar archive: %w", err)
		}

		parts := strings.Split(filepath.ToSlash(filepath.Clean(header.Name)), "/")
		if len(parts) == 0 {
			continue
		}

		if rootPrefix == "" {
			if validProfileNameRegex.MatchString(parts[0]) {
				rootPrefix = parts[0]
			}
		}

		var relPath string
		if rootPrefix != "" && parts[0] == rootPrefix {
			if len(parts) == 1 {
				// Skip the root folder entry itself
				continue
			}
			relPath = filepath.Join(parts[1:]...)
		} else {
			relPath = filepath.Join(parts...)
		}

		targetPath := filepath.Join(targetDir, relPath)

		// Secure extraction check: prevent directory traversal
		cleanTargetDir := filepath.Clean(targetDir)
		cleanTargetPath := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTargetPath, cleanTargetDir+string(filepath.Separator)) && cleanTargetPath != cleanTargetDir {
			return fmt.Errorf("tar entry attempts directory traversal: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0700); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
			}

			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, header.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}
			
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return fmt.Errorf("failed to write file %s: %w", targetPath, err)
			}
			file.Close()
		case tar.TypeSymlink:
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0700); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
			}
			_ = os.Remove(targetPath)
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", targetPath, err)
			}
		}
	}

	return nil
}

// ExportAll packages all existing profiles into a single gzipped tar archive.
func ExportAll(writer io.Writer) error {
	profiles, err := List()
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no profiles found to export")
	}

	gw := gzip.NewWriter(writer)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	baseDir, err := GetBaseDir()
	if err != nil {
		return err
	}

	for _, pName := range profiles {
		profileDir := filepath.Join(baseDir, pName)
		err = filepath.Walk(profileDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}

			relPath = filepath.ToSlash(relPath)

			header, err := tar.FileInfoHeader(info, info.Name())
			if err != nil {
				return err
			}

			header.Name = relPath

			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(path)
				if err != nil {
					return err
				}
				header.Linkname = linkTarget
			}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if info.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()

				if _, err := io.Copy(tw, file); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ImportAll restores all profiles from a gzipped tar archive.
func ImportAll(reader io.Reader, overwrite bool) error {
	baseDir, err := GetBaseDir()
	if err != nil {
		return err
	}

	gr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	clearedProfiles := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading tar archive: %w", err)
		}

		parts := strings.Split(filepath.ToSlash(filepath.Clean(header.Name)), "/")
		if len(parts) == 0 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
			continue
		}

		pName := parts[0]
		if !validProfileNameRegex.MatchString(pName) {
			return fmt.Errorf("archive contains invalid profile name: %s", pName)
		}

		if !clearedProfiles[pName] {
			profileDir := filepath.Join(baseDir, pName)
			exists, _, err := Exists(pName)
			if err != nil {
				return err
			}
			if exists {
				if !overwrite {
					return fmt.Errorf("profile %q already exists; use --force to overwrite", pName)
				}
				if err := os.RemoveAll(profileDir); err != nil {
					return fmt.Errorf("failed to remove existing profile directory %s: %w", profileDir, err)
				}
			}
			clearedProfiles[pName] = true
		}

		targetPath := filepath.Join(baseDir, filepath.Join(parts...))

		// Secure extraction check: prevent directory traversal
		cleanBaseDir := filepath.Clean(baseDir)
		cleanTargetPath := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTargetPath, cleanBaseDir+string(filepath.Separator)) {
			return fmt.Errorf("tar entry attempts directory traversal: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0700); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
			}

			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, header.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}
			
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return fmt.Errorf("failed to write file %s: %w", targetPath, err)
			}
			file.Close()
		case tar.TypeSymlink:
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0700); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
			}
			_ = os.Remove(targetPath)
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", targetPath, err)
			}
		}
	}

	return nil
}
