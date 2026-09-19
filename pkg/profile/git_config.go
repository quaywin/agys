package profile

import (
	"os"
	"path/filepath"
)

// EnsureGitConfig ensures the profile directory has access to Git configuration (~/.gitconfig).
// If the profile directory does not have its own .gitconfig, it symlinks the real user's ~/.gitconfig
// (or copies it on filesystems where symlinking is unavailable) so that Git commands executed under
// the isolated profile preserve the user's Git author name, email, aliases, and core settings.
func EnsureGitConfig(profileDir string) error {
	if profileDir == "" {
		return nil
	}
	profileGitConfig := filepath.Join(profileDir, ".gitconfig")

	// If profile already has a valid .gitconfig (regular file or working symlink), keep it
	if _, err := os.Stat(profileGitConfig); err == nil {
		return nil
	}

	// Remove broken symlink if any exists
	if info, lerr := os.Lstat(profileGitConfig); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(profileGitConfig)
		}
	}

	realHome, err := GetRealUserHome()
	if err != nil || realHome == "" {
		return nil
	}
	realGitConfig := filepath.Join(realHome, ".gitconfig")
	if _, err := os.Stat(realGitConfig); err != nil {
		// Real user has no .gitconfig, nothing to link
		return nil
	}

	// Attempt symlink first for live synchronization with host ~/.gitconfig
	err = os.Symlink(realGitConfig, profileGitConfig)
	if err != nil {
		// Fallback to copy (e.g. Windows without elevated privileges or unsupported filesystems)
		content, rerr := os.ReadFile(realGitConfig)
		if rerr != nil {
			return rerr
		}
		return WriteFileAtomic(profileGitConfig, content, 0644)
	}
	return nil
}

// SyncAllProfilesGitConfig ensures that all active profiles have their .gitconfig linked or synced.
func SyncAllProfilesGitConfig() error {
	profiles, err := List()
	if err != nil {
		return err
	}
	for _, p := range profiles {
		pDir, err := GetProfileDir(p)
		if err != nil {
			continue
		}
		_ = EnsureGitConfig(pDir)
	}
	return nil
}

// GetGitEnv constructs an environment variable slice for Git commands executed by agys,
// ensuring GIT_CONFIG_GLOBAL points to the real user's global gitconfig if missing from $HOME.
func GetGitEnv() []string {
	baseEnv := os.Environ()
	if os.Getenv("GIT_CONFIG_GLOBAL") != "" {
		return baseEnv
	}

	home := os.Getenv("HOME")
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, ".gitconfig")); err == nil {
			return baseEnv
		}
	}

	realHome, err := GetRealUserHome()
	if err == nil && realHome != "" {
		realGitConfig := filepath.Join(realHome, ".gitconfig")
		if _, err := os.Stat(realGitConfig); err == nil {
			return append(baseEnv, "GIT_CONFIG_GLOBAL="+realGitConfig)
		}
	}

	return baseEnv
}
