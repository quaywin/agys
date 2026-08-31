package profile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	os.Setenv("AGYS_SKIP_KEYCHAIN", "1")
	os.Unsetenv("AGYS_REAL_HOME")
	// Keep tests hermetic when developers run them inside Herdr. Otherwise,
	// statusline/title tests can send metadata to the real active pane.
	os.Unsetenv("HERDR_ENV")
	os.Unsetenv("HERDR_PANE_ID")
	os.Unsetenv("HERDR_SOCKET_PATH")
	tempAgys := filepath.Join(os.TempDir(), "agys-test-global")
	_ = os.MkdirAll(tempAgys, 0700)
	os.Setenv("AGYS_DIR", tempAgys)
	code := m.Run()
	_ = os.RemoveAll(tempAgys)
	os.Exit(code)
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"valid-name", false},
		{"valid_name_123", false},
		{"work", false},
		{"auto", true},
		{"AUTO", true},
		{"", true},
		{"invalid name", true},
		{"invalid/slash", true},
		{"invalid@symbol", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestProfileLifecycle(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profileName := "test-profile"

	// Validate non-existent initially
	exists, dir, err := Exists(profileName)
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Fatalf("Expected profile to not exist")
	}

	expectedDir := filepath.Join(tempHome, ".agys", "profiles", profileName)
	if dir != expectedDir {
		t.Errorf("Expected dir %s, got %s", expectedDir, dir)
	}

	// Create profile
	createdDir, err := Create(profileName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if createdDir != expectedDir {
		t.Errorf("Expected created dir %s, got %s", expectedDir, createdDir)
	}

	// List profiles
	profiles, err := List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(profiles) != 1 || profiles[0] != profileName {
		t.Errorf("Expected profiles [%s], got %v", profileName, profiles)
	}

	// Delete profile
	err = Delete(profileName)
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// Re-check existence
	exists, _, err = Exists(profileName)
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("Expected profile to be deleted")
	}
}

func TestProfileRename(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	oldName := "profile-old"
	newName := "profile-new"

	// Create profile
	_, err := Create(oldName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Rename profile
	err = Rename(oldName, newName)
	if err != nil {
		t.Fatalf("Rename error: %v", err)
	}

	// Old should not exist
	existsOld, _, err := Exists(oldName)
	if err != nil {
		t.Fatalf("Exists old error: %v", err)
	}
	if existsOld {
		t.Errorf("Expected old profile to not exist")
	}

	// New should exist
	existsNew, _, err := Exists(newName)
	if err != nil {
		t.Fatalf("Exists new error: %v", err)
	}
	if !existsNew {
		t.Errorf("Expected new profile to exist")
	}

	// Rename non-existent profile should fail
	err = Rename("non-existent", "another-name")
	if err == nil {
		t.Errorf("Expected error when renaming non-existent profile")
	}

	// Create another profile and test collision
	otherName := "other-profile"
	_, err = Create(otherName)
	if err != nil {
		t.Fatalf("Create other profile error: %v", err)
	}

	err = Rename(newName, otherName)
	if err == nil {
		t.Errorf("Expected error when renaming to existing profile name")
	}
}

func TestCurrentProfile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// GetCurrent should be empty initially
	curr, err := GetCurrent()
	if err != nil {
		t.Fatalf("GetCurrent error: %v", err)
	}
	if curr != "" {
		t.Errorf("Expected empty current profile, got %q", curr)
	}

	// SetCurrent on non-existent profile should fail
	err = SetCurrent("nonexistent")
	if err == nil {
		t.Errorf("Expected error setting non-existent current profile")
	}

	// Create profile
	profile1 := "work"
	profile2 := "personal"
	_, _ = Create(profile1)
	_, _ = Create(profile2)

	// Set profile1 as current
	if err := SetCurrent(profile1); err != nil {
		t.Fatalf("SetCurrent error: %v", err)
	}

	curr, err = GetCurrent()
	if err != nil {
		t.Fatalf("GetCurrent error: %v", err)
	}
	if curr != profile1 {
		t.Errorf("Expected current profile %q, got %q", profile1, curr)
	}

	// Rename profile1 to profile3 -> current profile should be updated
	profile3 := "work-new"
	if err := Rename(profile1, profile3); err != nil {
		t.Fatalf("Rename error: %v", err)
	}

	curr, err = GetCurrent()
	if err != nil {
		t.Fatalf("GetCurrent error: %v", err)
	}
	if curr != profile3 {
		t.Errorf("Expected current profile %q after rename, got %q", profile3, curr)
	}

	// Delete profile3 -> current profile should be unset
	if err := Delete(profile3); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	curr, err = GetCurrent()
	if err != nil {
		t.Fatalf("GetCurrent error: %v", err)
	}
	if curr != "" {
		t.Errorf("Expected empty current profile after delete, got %q", curr)
	}

	// Set profile2 as current, then UnsetCurrent
	if err := SetCurrent(profile2); err != nil {
		t.Fatalf("SetCurrent error: %v", err)
	}
	if err := UnsetCurrent(); err != nil {
		t.Fatalf("UnsetCurrent error: %v", err)
	}
	curr, err = GetCurrent()
	if err != nil {
		t.Fatalf("GetCurrent error: %v", err)
	}
	if curr != "" {
		t.Errorf("Expected empty current profile after UnsetCurrent, got %q", curr)
	}
}

func TestAgysDirEnv(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("AGYS_DIR", customDir)

	baseDir, err := GetBaseDir()
	if err != nil {
		t.Fatalf("GetBaseDir error: %v", err)
	}
	expected := filepath.Join(customDir, "profiles")
	if baseDir != expected {
		t.Errorf("Expected base dir %s, got %s", expected, baseDir)
	}
}

func TestProjectIDCache(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profileName := "test-cache-profile"
	_, err := Create(profileName)
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// Initially no cached project ID
	projectID, err := GetCachedProjectID(profileName)
	if err == nil && projectID != "" {
		t.Errorf("Expected empty cached project ID, got %q", projectID)
	}

	// Save project ID
	expectedID := "cloudaicompanion-test-12345"
	if err := SaveCachedProjectID(profileName, expectedID); err != nil {
		t.Fatalf("SaveCachedProjectID error: %v", err)
	}

	// Read project ID back
	cachedID, err := GetCachedProjectID(profileName)
	if err != nil {
		t.Fatalf("GetCachedProjectID error: %v", err)
	}
	if cachedID != expectedID {
		t.Errorf("Expected cached project ID %q, got %q", expectedID, cachedID)
	}
}

func TestSetCurrentAuto(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	if err := SetCurrent("auto"); err != nil {
		t.Fatalf("SetCurrent('auto') error = %v", err)
	}

	curr, err := GetCurrent()
	if err != nil {
		t.Fatalf("GetCurrent error = %v", err)
	}
	if curr != "auto" {
		t.Errorf("Expected current profile 'auto', got %q", curr)
	}
}

func TestEnsureKeychain(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profileName := "test-keychain-profile"
	profileDir, err := Create(profileName)
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	if runtime.GOOS == "darwin" {
		realKeychainsDir := filepath.Join(tempHome, "Library", "Keychains")
		if err := os.MkdirAll(realKeychainsDir, 0700); err != nil {
			t.Fatalf("Failed to create mock keychains dir: %v", err)
		}

		err = EnsureKeychain(profileDir)
		if err != nil {
			t.Fatalf("EnsureKeychain error: %v", err)
		}

		profileKeychainsDir := filepath.Join(profileDir, "Library", "Keychains")
		info, err := os.Lstat(profileKeychainsDir)
		if err != nil {
			t.Fatalf("Failed to lstat profile Keychains dir: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("Expected %s to be a symlink", profileKeychainsDir)
		}
	} else {
		err = EnsureKeychain(profileDir)
		if err != nil {
			t.Fatalf("EnsureKeychain error: %v", err)
		}
	}
}

func TestUnauthenticatedError(t *testing.T) {
	if isUnauthenticatedError(nil) {
		t.Errorf("Expected false for nil error")
	}
	if !isUnauthenticatedError(ErrUnauthenticated) {
		t.Errorf("Expected true for ErrUnauthenticated")
	}
	if !isUnauthenticatedError(errors.New("HTTP status 401: unauthorized")) {
		t.Errorf("Expected true for 401 error string")
	}
	if isUnauthenticatedError(errors.New("HTTP status 500: internal server error")) {
		t.Errorf("Expected false for 500 error string")
	}
}

func TestFormatHTTPError(t *testing.T) {
	err401 := formatHTTPError(401, []byte(`{"error":{"status":"UNAUTHENTICATED"}}`))
	if !errors.Is(err401, ErrUnauthenticated) {
		t.Errorf("Expected err401 to wrap ErrUnauthenticated")
	}

	err500 := formatHTTPError(500, []byte("Server error"))
	if errors.Is(err500, ErrUnauthenticated) {
		t.Errorf("Expected err500 not to wrap ErrUnauthenticated")
	}
}

func TestWithKeychainLock(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	executed := false
	err := WithKeychainLock(nil, func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithKeychainLock error: %v", err)
	}
	if !executed {
		t.Errorf("Expected fn to be executed inside WithKeychainLock")
	}
}

func TestDetectDuplicateTokens(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	p1Dir, err := Create("profile-1")
	if err != nil {
		t.Fatalf("Create profile-1 error: %v", err)
	}
	p2Dir, err := Create("profile-2")
	if err != nil {
		t.Fatalf("Create profile-2 error: %v", err)
	}

	tokJSON := []byte(`{
		"token": {
			"access_token": "acc_123",
			"refresh_token": "ref_123"
		}
	}`)

	_ = os.MkdirAll(filepath.Join(p1Dir, ".gemini", "antigravity-cli"), 0700)
	_ = os.MkdirAll(filepath.Join(p2Dir, ".gemini", "antigravity-cli"), 0700)

	_ = os.WriteFile(filepath.Join(p1Dir, ".gemini", "antigravity-cli", "antigravity-oauth-token"), tokJSON, 0600)
	_ = os.WriteFile(filepath.Join(p2Dir, ".gemini", "antigravity-cli", "antigravity-oauth-token"), tokJSON, 0600)

	dups, err := DetectDuplicateTokens()
	if err != nil {
		t.Fatalf("DetectDuplicateTokens error: %v", err)
	}
	if len(dups) != 2 {
		t.Errorf("Expected 2 duplicate profiles, got %d: %v", len(dups), dups)
	}
	if _, ok := dups["profile-1"]; !ok {
		t.Errorf("Expected profile-1 to be flagged as duplicate")
	}
	if _, ok := dups["profile-2"]; !ok {
		t.Errorf("Expected profile-2 to be flagged as duplicate")
	}
}

func TestConfiguredStatus(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pName := "config-test-profile"
	pDir, err := Create(pName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Initially neither CLI nor IDE is configured
	if IsCLIConfigured(pName) {
		t.Errorf("Expected CLI to not be configured initially")
	}
	if IsIDEConfigured(pName) {
		t.Errorf("Expected IDE to not be configured initially")
	}
	if summary := GetConfigSummary(pName); summary != "-" {
		t.Errorf("Expected summary '-', got %q", summary)
	}

	// Add CLI token file
	cliDir := filepath.Join(pDir, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(cliDir, 0700); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	tokPath := filepath.Join(cliDir, "antigravity-oauth-token")
	if err := os.WriteFile(tokPath, []byte(`{"token":{}}`), 0600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	if !IsCLIConfigured(pName) {
		t.Errorf("Expected CLI to be configured after adding token")
	}
	if IsIDEConfigured(pName) {
		t.Errorf("Expected IDE to not be configured yet")
	}
	if summary := GetConfigSummary(pName); summary != "CLI" {
		t.Errorf("Expected summary 'CLI', got %q", summary)
	}

	// Add IDE user directory
	ideUserDir := filepath.Join(pDir, "ide-data", "User")
	if err := os.MkdirAll(ideUserDir, 0700); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	if !IsIDEConfigured(pName) {
		t.Errorf("Expected IDE to be configured after creating ide-data/User")
	}
	if summary := GetConfigSummary(pName); summary != "CLI, IDE" {
		t.Errorf("Expected summary 'CLI, IDE', got %q", summary)
	}
}

func TestSyncKeychainTokenToDisk_Isolation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping Keychain test on non-macOS")
	}

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pName := "test-isolation-p1"
	pDir, err := Create(pName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// 1. Initial disk token for Profile A
	initialTokJSON := []byte(`{
		"token": {
			"access_token": "acc_profileA",
			"refresh_token": "ref_profileA"
		}
	}`)
	cliDir := filepath.Join(pDir, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(cliDir, 0700)
	tokPath := filepath.Join(cliDir, "antigravity-oauth-token")
	_ = os.WriteFile(tokPath, initialTokJSON, 0600)

	// Simulate Profile B writing to Keychain
	p2Dir, err := Create("test-isolation-p2")
	if err != nil {
		t.Fatalf("Create p2 error: %v", err)
	}
	p2TokJSON := []byte(`{
		"token": {
			"access_token": "acc_profileB",
			"refresh_token": "ref_profileB"
		}
	}`)
	p2TokPath := filepath.Join(p2Dir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	_ = os.MkdirAll(filepath.Dir(p2TokPath), 0700)
	_ = os.WriteFile(p2TokPath, p2TokJSON, 0600)
	SyncDiskTokenToKeychain(p2Dir)

	// Now Profile A finishes and calls SyncKeychainTokenToDisk
	SyncKeychainTokenToDisk(pDir, "ref_profileA")

	// Verify Profile A's disk token was NOT overwritten by Profile B's token
	afterTok, err := ReadTokenFromDir(pDir)
	if err != nil {
		t.Fatalf("ReadTokenFromDir error: %v", err)
	}
	if afterTok.Token.RefreshToken != "ref_profileA" {
		t.Errorf("SECURITY/ISOLATION VIOLATION: Expected refresh_token 'ref_profileA', got %q", afterTok.Token.RefreshToken)
	}
	if afterTok.Token.AccessToken != "acc_profileA" {
		t.Errorf("Expected access_token 'acc_profileA', got %q", afterTok.Token.AccessToken)
	}
}
