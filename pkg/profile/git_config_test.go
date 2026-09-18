package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitConfig_Symlink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agys-test-gitconfig-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	realHome := filepath.Join(tmpDir, "realuser")
	profileDir := filepath.Join(tmpDir, "profiles", "testprof")
	if err := os.MkdirAll(realHome, 0755); err != nil {
		t.Fatalf("failed to create realHome: %v", err)
	}
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatalf("failed to create profileDir: %v", err)
	}

	realGitConfig := filepath.Join(realHome, ".gitconfig")
	initialContent := "[user]\n\tname = Test User\n\temail = test@example.com\n"
	if err := os.WriteFile(realGitConfig, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write realGitConfig: %v", err)
	}

	t.Setenv("AGYS_REAL_HOME", realHome)

	if err := EnsureGitConfig(profileDir); err != nil {
		t.Fatalf("EnsureGitConfig failed: %v", err)
	}

	profileGitConfig := filepath.Join(profileDir, ".gitconfig")
	data, err := os.ReadFile(profileGitConfig)
	if err != nil {
		t.Fatalf("failed to read profileGitConfig: %v", err)
	}
	if string(data) != initialContent {
		t.Errorf("content mismatch: got %q, expected %q", string(data), initialContent)
	}

	// Verify live sync / symlink behavior
	updatedContent := "[user]\n\tname = Updated User\n\temail = updated@example.com\n"
	if err := os.WriteFile(realGitConfig, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("failed to update realGitConfig: %v", err)
	}

	dataAfter, err := os.ReadFile(profileGitConfig)
	if err != nil {
		t.Fatalf("failed to re-read profileGitConfig: %v", err)
	}
	// If symlink is supported, it reflects update immediately
	if info, lerr := os.Lstat(profileGitConfig); lerr == nil && (info.Mode()&os.ModeSymlink != 0) {
		if string(dataAfter) != updatedContent {
			t.Errorf("symlinked content did not reflect update: got %q, expected %q", string(dataAfter), updatedContent)
		}
	}
}

func TestEnsureGitConfig_ExistingPreserved(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agys-test-gitconfig-exist-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	realHome := filepath.Join(tmpDir, "realuser")
	profileDir := filepath.Join(tmpDir, "profiles", "customprof")
	_ = os.MkdirAll(realHome, 0755)
	_ = os.MkdirAll(profileDir, 0755)

	_ = os.WriteFile(filepath.Join(realHome, ".gitconfig"), []byte("[user]\n\tname = Host\n"), 0644)
	customContent := "[user]\n\tname = ProfileCustom\n\temail = custom@corp.com\n"
	_ = os.WriteFile(filepath.Join(profileDir, ".gitconfig"), []byte(customContent), 0644)

	t.Setenv("AGYS_REAL_HOME", realHome)

	if err := EnsureGitConfig(profileDir); err != nil {
		t.Fatalf("EnsureGitConfig failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(profileDir, ".gitconfig"))
	if string(data) != customContent {
		t.Errorf("existing profile .gitconfig was overwritten: got %q, expected %q", string(data), customContent)
	}
}

func TestEnsureGitConfig_NoRealGitConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agys-test-gitconfig-none-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	realHome := filepath.Join(tmpDir, "realuser")
	profileDir := filepath.Join(tmpDir, "profiles", "prof")
	_ = os.MkdirAll(realHome, 0755)
	_ = os.MkdirAll(profileDir, 0755)

	t.Setenv("AGYS_REAL_HOME", realHome)

	if err := EnsureGitConfig(profileDir); err != nil {
		t.Fatalf("EnsureGitConfig should not fail when host has no .gitconfig: %v", err)
	}

	if _, err := os.Stat(filepath.Join(profileDir, ".gitconfig")); !os.IsNotExist(err) {
		t.Errorf("expected profile .gitconfig to not exist")
	}
}

func TestGetGitEnv(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agys-test-gitenv-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	realHome := filepath.Join(tmpDir, "realhome")
	_ = os.MkdirAll(realHome, 0755)
	realGitConfig := filepath.Join(realHome, ".gitconfig")
	_ = os.WriteFile(realGitConfig, []byte("[user]\n\tname = Test\n"), 0644)

	t.Setenv("AGYS_REAL_HOME", realHome)
	t.Setenv("HOME", filepath.Join(tmpDir, "isolated_profile")) // no .gitconfig here
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	env := GetGitEnv()
	found := false
	expected := "GIT_CONFIG_GLOBAL=" + realGitConfig
	for _, e := range env {
		if e == expected {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GetGitEnv to contain %q, got: %v", expected, env)
	}

	// When GIT_CONFIG_GLOBAL is already set, it should not be overwritten
	customGlobal := "/custom/gitconfig"
	t.Setenv("GIT_CONFIG_GLOBAL", customGlobal)
	env2 := GetGitEnv()
	foundCustom := false
	for _, e := range env2 {
		if strings.HasPrefix(e, "GIT_CONFIG_GLOBAL="+customGlobal) {
			foundCustom = true
			break
		}
	}
	if !foundCustom {
		t.Errorf("expected GetGitEnv to preserve existing GIT_CONFIG_GLOBAL")
	}
}
