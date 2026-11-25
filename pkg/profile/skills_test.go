package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGlobalSkills_SymlinkAndPreservation(t *testing.T) {
	realHome := t.TempDir()
	agysDir := filepath.Join(t.TempDir(), ".agys")
	t.Setenv("AGYS_REAL_HOME", realHome)
	t.Setenv("AGYS_DIR", agysDir)

	profileDir := filepath.Join(agysDir, "profiles", "work")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		t.Fatalf("failed to create profile dir: %v", err)
	}

	// 1. No ~/.agents/skills in real home -> no-op
	if err := EnsureGlobalSkills(profileDir); err != nil {
		t.Fatalf("EnsureGlobalSkills failed when host has no skills: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(profileDir, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("expected no profile skills symlink when host has none, err=%v", err)
	}

	// 2. Host ~/.agents/skills exists -> creates symlink
	hostSkills := filepath.Join(realHome, ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(hostSkills, "my-skill"), 0755); err != nil {
		t.Fatalf("failed to create host skills dir: %v", err)
	}
	if err := EnsureGlobalSkills(profileDir); err != nil {
		t.Fatalf("EnsureGlobalSkills failed: %v", err)
	}
	profileSkills := filepath.Join(profileDir, ".agents", "skills")
	info, err := os.Lstat(profileSkills)
	if err != nil {
		t.Fatalf("expected profile skills symlink, got err: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s", profileSkills)
	}
	if skip, _ := ShouldSkipArchiveEntry(filepath.Join(".agents", "skills"), info); !skip {
		t.Errorf("expected archive/clone skip for host-symlinked .agents/skills")
	}

	// 3. Broken symlink cleanup when host dir disappears
	_ = os.RemoveAll(hostSkills)
	if err := EnsureGlobalSkills(profileDir); err != nil {
		t.Fatalf("EnsureGlobalSkills cleanup failed: %v", err)
	}
	if _, err := os.Lstat(profileSkills); !os.IsNotExist(err) {
		t.Errorf("expected dangling skills symlink to be removed, err=%v", err)
	}

	// 4. Preserve profile-owned .agents/skills directory if user created one manually
	_ = os.MkdirAll(hostSkills, 0755)
	_ = os.MkdirAll(filepath.Join(profileSkills, "local-only"), 0755)
	if err := EnsureGlobalSkills(profileDir); err != nil {
		t.Fatalf("EnsureGlobalSkills failed with existing profile skills: %v", err)
	}
	pInfo, err := os.Lstat(profileSkills)
	if err != nil || pInfo.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected existing profile-local skills directory to be preserved without replacing by symlink")
	}
}

func TestEnsureGlobalSkills_SelfHomeGuard(t *testing.T) {
	sameDir := t.TempDir()
	t.Setenv("AGYS_REAL_HOME", sameDir)
	if err := os.MkdirAll(filepath.Join(sameDir, ".agents", "skills"), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := EnsureGlobalSkills(sameDir); err != nil {
		t.Fatalf("expected no error on self-home guard, got: %v", err)
	}
	info, err := os.Lstat(filepath.Join(sameDir, ".agents", "skills"))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("self-home guard should leave host directory intact")
	}
}
