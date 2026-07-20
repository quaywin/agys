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

// GetAgysDir returns the root configuration directory (~/.agys or $AGYS_DIR).
func GetAgysDir() (string, error) {
	if custom := os.Getenv("AGYS_DIR"); custom != "" {
		return custom, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to determine user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".agys"), nil
}

// GetBaseDir returns the global base directory for storing profiles (~/.agys/profiles).
func GetBaseDir() (string, error) {
	agysDir, err := GetAgysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(agysDir, "profiles"), nil
}

// GetProfileDir returns the directory path for a specific profile name.
func GetProfileDir(name string) (string, error) {
	if IsAuto(name) {
		return "", fmt.Errorf("profile name %q is a reserved system keyword", name)
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}
	baseDir, err := GetBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, name), nil
}

// ValidateName ensures profile names contain only allowed characters and are not reserved keywords.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if IsAuto(name) {
		return fmt.Errorf("invalid profile name %q: reserved keyword", name)
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

// MigrateLegacyProfiles migrates profiles from legacy ~/.antigravity-profiles to ~/.agys/profiles if present.
func MigrateLegacyProfiles() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	legacyDir := filepath.Join(homeDir, ".antigravity-profiles")
	entries, err := os.ReadDir(legacyDir)
	if err != nil || len(entries) == 0 {
		return
	}

	baseDir, err := GetBaseDir()
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		oldPath := filepath.Join(legacyDir, entry.Name())
		newPath := filepath.Join(baseDir, entry.Name())
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			_ = os.MkdirAll(baseDir, 0700)
			_ = os.Rename(oldPath, newPath)
		}
	}
}

// List returns a sorted slice of available profile names.
func List() ([]string, error) {
	MigrateLegacyProfiles()
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

	current, _ := GetCurrent()
	if current == name {
		_ = UnsetCurrent()
	}

	return nil
}

// Rename renames an existing profile directory to a new profile name.
func Rename(oldName, newName string) error {
	if err := ValidateName(oldName); err != nil {
		return fmt.Errorf("invalid old profile name: %w", err)
	}
	if err := ValidateName(newName); err != nil {
		return fmt.Errorf("invalid new profile name: %w", err)
	}

	exists, oldDir, err := Exists(oldName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("profile %q does not exist", oldName)
	}

	newExists, newDir, err := Exists(newName)
	if err != nil {
		return err
	}
	if newExists {
		return fmt.Errorf("profile %q already exists", newName)
	}

	current, _ := GetCurrent()

	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("failed to rename profile directory from %s to %s: %w", oldDir, newDir, err)
	}

	if current == oldName {
		_ = SetCurrent(newName)
	}

	return nil
}

const currentProfileFilename = "current"

// GetCurrent returns the name of the currently configured default profile, or empty string if none.
func GetCurrent() (string, error) {
	agysDir, err := GetAgysDir()
	if err != nil {
		return "", err
	}
	currentFile := filepath.Join(agysDir, currentProfileFilename)
	data, err := os.ReadFile(currentFile)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read current profile file: %w", err)
	}

	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", nil
	}

	if IsAuto(name) {
		return AutoProfileKeyword, nil
	}

	// Verify profile still exists
	exists, _, err := Exists(name)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}

	return name, nil
}

// SetCurrent sets the default active profile.
func SetCurrent(name string) error {
	if IsAuto(name) {
		name = AutoProfileKeyword
	} else {
		exists, _, err := Exists(name)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("profile %q does not exist", name)
		}
	}

	agysDir, err := GetAgysDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agysDir, 0700); err != nil {
		return fmt.Errorf("failed to create agys directory %s: %w", agysDir, err)
	}

	currentFile := filepath.Join(agysDir, currentProfileFilename)
	if err := os.WriteFile(currentFile, []byte(name+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write current profile file: %w", err)
	}

	return nil
}

// UnsetCurrent removes the default active profile setting.
func UnsetCurrent() error {
	agysDir, err := GetAgysDir()
	if err != nil {
		return err
	}
	currentFile := filepath.Join(agysDir, currentProfileFilename)
	if err := os.Remove(currentFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove current profile file: %w", err)
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
