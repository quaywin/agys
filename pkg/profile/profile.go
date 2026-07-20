package profile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var validProfileNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// GetBaseDir returns the global base directory for storing antigravity profiles (~/.antigravity-profiles).
func GetBaseDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to determine user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".antigravity-profiles"), nil
}

// GetProfileDir returns the directory path for a specific profile name.
func GetProfileDir(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	baseDir, err := GetBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, name), nil
}

// ValidateName ensures profile names contain only allowed characters.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if !validProfileNameRegex.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: must contain only letters, numbers, hyphens, and underscores", name)
	}
	return nil
}

// EnsureBaseDirExists creates the base profiles directory if it does not exist.
func EnsureBaseDirExists() (string, error) {
	baseDir, err := GetBaseDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create base profile directory %s: %w", baseDir, err)
	}
	return baseDir, nil
}

// Exists checks if a profile directory exists.
func Exists(name string) (bool, string, error) {
	profileDir, err := GetProfileDir(name)
	if err != nil {
		return false, "", err
	}
	info, err := os.Stat(profileDir)
	if os.IsNotExist(err) {
		return false, profileDir, nil
	}
	if err != nil {
		return false, profileDir, err
	}
	return info.IsDir(), profileDir, nil
}

// Create initializes a new profile folder.
func Create(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	profileDir, err := GetProfileDir(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create profile directory %s: %w", profileDir, err)
	}
	return profileDir, nil
}

// List returns a sorted slice of available profile names.
func List() ([]string, error) {
	baseDir, err := GetBaseDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(baseDir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read profile directory: %w", err)
	}

	var profiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			profiles = append(profiles, entry.Name())
		}
	}
	sort.Strings(profiles)
	return profiles, nil
}

// Delete removes the specified profile directory.
func Delete(name string) error {
	exists, profileDir, err := Exists(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("profile %q does not exist", name)
	}
	if err := os.RemoveAll(profileDir); err != nil {
		return fmt.Errorf("failed to delete profile directory %s: %w", profileDir, err)
	}
	return nil
}

// BuildCmd constructs an exec.Cmd for running `agy` with the modified HOME environment variable.
func BuildCmd(profileDir string, args ...string) *exec.Cmd {
	cmd := exec.Command("agy", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set HOME environment variable while preserving other environment variables
	env := os.Environ()
	homeEnv := "HOME=" + profileDir
	updated := false
	for i, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			env[i] = homeEnv
			updated = true
			break
		}
	}
	if !updated {
		env = append(env, homeEnv)
	}
	cmd.Env = env

	return cmd
}
