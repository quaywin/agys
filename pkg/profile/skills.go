package profile

import (
	"os"
	"path/filepath"
)

// EnsureGlobalSkills ensures the isolated profile directory has access to the user's
// cross-tool global skills directory (~/.agents/skills, supported by agy v1.5.16+).
// If the profile does not define its own .agents/skills directory and the real user home
// contains ~/.agents/skills, a symlink is created so global skills sync live across profiles.
func EnsureGlobalSkills(profileDir string) error {
	if profileDir == "" {
		return nil
	}

	realHome, err := GetRealUserHome()
	if err != nil || realHome == "" || filepath.Clean(realHome) == filepath.Clean(profileDir) {
		return nil
	}

	profileSkillsDir := filepath.Join(profileDir, ".agents", "skills")

	// Preserve existing valid profile-specific skills directory or working symlink
	if _, err := os.Stat(profileSkillsDir); err == nil {
		return nil
	}

	// Remove dangling symlink if target disappeared
	if info, lerr := os.Lstat(profileSkillsDir); lerr == nil && (info.Mode()&os.ModeSymlink != 0) {
		_ = os.Remove(profileSkillsDir)
	}

	realSkillsDir := filepath.Join(realHome, ".agents", "skills")
	info, err := os.Stat(realSkillsDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(profileSkillsDir), 0700); err != nil {
		return err
	}

	return os.Symlink(realSkillsDir, profileSkillsDir)
}

// SyncAllProfilesGlobalSkills ensures that all active profiles link host ~/.agents/skills when present.
func SyncAllProfilesGlobalSkills() error {
	profiles, err := List()
	if err != nil {
		return err
	}
	for _, p := range profiles {
		pDir, err := GetProfileDir(p)
		if err != nil {
			continue
		}
		_ = EnsureGlobalSkills(pDir)
	}
	return nil
}
