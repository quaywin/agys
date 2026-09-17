package profile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestSanitizeProfilePath(t *testing.T) {
	tempHome := t.TempDir()
	realLocalBin := filepath.Join(tempHome, ".local", "bin")
	realGoBin := filepath.Join(tempHome, "go", "bin")
	_ = os.MkdirAll(realLocalBin, 0755)
	_ = os.MkdirAll(realGoBin, 0755)

	agysDir := filepath.Join(tempHome, ".agys")
	profilesBaseDir := filepath.Join(agysDir, "profiles")
	t.Setenv("AGYS_DIR", agysDir)

	currentProfileDir := filepath.Join(profilesBaseDir, "golang_dev")
	otherProfileDir := filepath.Join(profilesBaseDir, "quaywin_thang")

	_ = os.MkdirAll(filepath.Join(currentProfileDir, ".local", "bin"), 0755)
	_ = os.MkdirAll(filepath.Join(currentProfileDir, ".gemini", "antigravity-cli", "bin"), 0755)
	_ = os.MkdirAll(filepath.Join(otherProfileDir, ".local", "bin"), 0755)
	_ = os.MkdirAll(filepath.Join(otherProfileDir, ".gemini", "antigravity-cli", "bin"), 0755)

	currentCliBin := filepath.Join(currentProfileDir, ".gemini", "antigravity-cli", "bin")
	otherLocalBin := filepath.Join(otherProfileDir, ".local", "bin")
	otherCliBin := filepath.Join(otherProfileDir, ".gemini", "antigravity-cli", "bin")
	currentLocalBin := filepath.Join(currentProfileDir, ".local", "bin")

	// Construct contaminated PATH where stale profile binaries precede real binary locations
	contaminatedPath := strings.Join([]string{
		otherLocalBin,
		otherCliBin,
		currentLocalBin,
		currentCliBin,
		"/usr/bin",
		realLocalBin,
		"/bin",
	}, string(os.PathListSeparator))

	cleanedPath := SanitizeProfilePath(contaminatedPath, tempHome, currentProfileDir)
	entries := filepath.SplitList(cleanedPath)

	if len(entries) == 0 {
		t.Fatalf("Expected non-empty cleaned path")
	}

	// 1. First entry must be real user .local/bin
	if entries[0] != realLocalBin {
		t.Errorf("Expected first entry to be %s, got %s", realLocalBin, entries[0])
	}

	// 2. Second entry must be real user go/bin
	if len(entries) > 1 && entries[1] != realGoBin {
		t.Errorf("Expected second entry to be %s, got %s", realGoBin, entries[1])
	}

	// 3. Stale .local/bin from any profile must NOT be present
	for _, entry := range entries {
		if entry == otherLocalBin {
			t.Errorf("Contaminated other profile .local/bin %s should have been removed", otherLocalBin)
		}
		if entry == otherCliBin {
			t.Errorf("Contaminated other profile .gemini cli bin %s should have been removed", otherCliBin)
		}
		if entry == currentLocalBin {
			t.Errorf("Contaminated current profile .local/bin %s should have been removed", currentLocalBin)
		}
	}

	// 4. System paths and current profile's cli bin should remain preserved
	foundCurrentCliBin := false
	foundUsrBin := false
	foundBin := false
	for _, entry := range entries {
		if entry == currentCliBin {
			foundCurrentCliBin = true
		}
		if entry == "/usr/bin" {
			foundUsrBin = true
		}
		if entry == "/bin" {
			foundBin = true
		}
	}

	if !foundCurrentCliBin {
		t.Errorf("Expected current profile cli bin %s to be preserved", currentCliBin)
	}
	if !foundUsrBin {
		t.Errorf("Expected /usr/bin to be preserved")
	}
	if !foundBin {
		t.Errorf("Expected /bin to be preserved")
	}
}

func TestCleanStaleProfileBinaries(t *testing.T) {
	tempHome := t.TempDir()
	profileDir := filepath.Join(tempHome, "test-profile")

	localBin := filepath.Join(profileDir, ".local", "bin")
	goBin := filepath.Join(profileDir, "go", "bin")
	_ = os.MkdirAll(localBin, 0755)
	_ = os.MkdirAll(goBin, 0755)

	stale1 := filepath.Join(localBin, "agys")
	stale2 := filepath.Join(goBin, "agys")
	_ = os.WriteFile(stale1, []byte("#!/bin/sh\necho old"), 0755)
	_ = os.WriteFile(stale2, []byte("#!/bin/sh\necho old"), 0755)

	if _, err := os.Stat(stale1); err != nil {
		t.Fatalf("Failed to create mock binary %s", stale1)
	}
	if _, err := os.Stat(stale2); err != nil {
		t.Fatalf("Failed to create mock binary %s", stale2)
	}

	CleanStaleProfileBinaries(profileDir)

	if _, err := os.Stat(stale1); !os.IsNotExist(err) {
		t.Errorf("Expected %s to be deleted, but still exists", stale1)
	}
	if _, err := os.Stat(stale2); !os.IsNotExist(err) {
		t.Errorf("Expected %s to be deleted, but still exists", stale2)
	}
}

func TestWriteTokenToProfile_SkipsIdenticalContent(t *testing.T) {
	tempHome := t.TempDir()
	profileDir := filepath.Join(tempHome, "test-profile")

	tokenJSON := `{"token": {"access_token": "test-token-123", "refresh_token": "refresh-123"}}`
	
	// First write creates all candidate files
	if err := WriteTokenToProfile(profileDir, tokenJSON); err != nil {
		t.Fatalf("WriteTokenToProfile initial write failed: %v", err)
	}

	// Record mod times and verify files exist
	paths := GetTokenFilePaths(profileDir)
	modTimes := make(map[string]time.Time)
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected token file %s to exist: %v", p, err)
		}
		modTimes[p] = info.ModTime()
	}

	// Sleep slightly to ensure time tick
	time.Sleep(20 * time.Millisecond)

	// Second write with identical content should skip rewriting
	if err := WriteTokenToProfile(profileDir, tokenJSON); err != nil {
		t.Fatalf("WriteTokenToProfile second write failed: %v", err)
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected token file %s to exist: %v", p, err)
		}
		if !info.ModTime().Equal(modTimes[p]) {
			t.Errorf("expected file %s modtime to remain untouched, but changed from %v to %v", p, modTimes[p], info.ModTime())
		}
	}

	// Third write with changed content should update files
	newTokenJSON := `{"token": {"access_token": "new-token-456", "refresh_token": "new-refresh-456"}}`
	time.Sleep(20 * time.Millisecond)
	if err := WriteTokenToProfile(profileDir, newTokenJSON); err != nil {
		t.Fatalf("WriteTokenToProfile third write failed: %v", err)
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected token file %s to exist: %v", p, err)
		}
		if info.ModTime().Equal(modTimes[p]) {
			t.Errorf("expected file %s modtime to be updated for new content", p)
		}
	}
}

func TestSanitizeAgyEnv(t *testing.T) {
	t.Run("BasicSanitizationAndSSHStripping", func(t *testing.T) {
		baseEnv := []string{
			"USER=testuser",
			"PATH=/usr/bin:/bin",
			"SSH_CLIENT=100.70.188.99 56622 22",
			"SSH_CONNECTION=100.70.188.99 56622 100.81.62.112 22",
			"SSH_TTY=/dev/pts/1",
			"SSH_AUTH_SOCK=/tmp/ssh-agent.sock",
			"LANG=en_US.UTF-8",
		}

		envMap := map[string]string{
			"HOME":         "/custom/home",
			"AGYS_PROFILE": "myprofile",
		}

		sanitized := SanitizeAgyEnv(baseEnv, envMap)

		envLookup := make(map[string]string)
		for _, e := range sanitized {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envLookup[parts[0]] = parts[1]
			}
		}

		// 1. Verify SSH variables causing DA2 probe are stripped
		if _, ok := envLookup["SSH_CLIENT"]; ok {
			t.Errorf("expected SSH_CLIENT to be stripped, but found: %s", envLookup["SSH_CLIENT"])
		}
		if _, ok := envLookup["SSH_CONNECTION"]; ok {
			t.Errorf("expected SSH_CONNECTION to be stripped, but found: %s", envLookup["SSH_CONNECTION"])
		}
		if _, ok := envLookup["SSH_TTY"]; ok {
			t.Errorf("expected SSH_TTY to be stripped, but found: %s", envLookup["SSH_TTY"])
		}

		// 2. Verify SSH_AUTH_SOCK (git SSH key forwarding) is PRESERVED
		if envLookup["SSH_AUTH_SOCK"] != "/tmp/ssh-agent.sock" {
			t.Errorf("expected SSH_AUTH_SOCK to be preserved, got: %s", envLookup["SSH_AUTH_SOCK"])
		}

		// 3. Verify TERM_PROGRAM is populated with non-empty fallback
		if tp, ok := envLookup["TERM_PROGRAM"]; !ok || tp == "" {
			t.Errorf("expected TERM_PROGRAM to be populated, got: %q", tp)
		}

		// 4. Verify overrides are applied
		if envLookup["HOME"] != "/custom/home" {
			t.Errorf("expected HOME to be /custom/home, got %s", envLookup["HOME"])
		}
		if envLookup["AGYS_PROFILE"] != "myprofile" {
			t.Errorf("expected AGYS_PROFILE to be myprofile, got %s", envLookup["AGYS_PROFILE"])
		}

		// 5. Verify regular env vars are preserved
		if envLookup["USER"] != "testuser" {
			t.Errorf("expected USER to be testuser, got %s", envLookup["USER"])
		}
		if envLookup["LANG"] != "en_US.UTF-8" {
			t.Errorf("expected LANG to be en_US.UTF-8, got %s", envLookup["LANG"])
		}
	})

	t.Run("PreserveExistingTermProgramFromBaseEnv", func(t *testing.T) {
		baseEnv := []string{
			"USER=testuser",
			"TERM_PROGRAM=ghostty",
		}
		sanitized := SanitizeAgyEnv(baseEnv, nil)
		envLookup := make(map[string]string)
		for _, e := range sanitized {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envLookup[parts[0]] = parts[1]
			}
		}
		if envLookup["TERM_PROGRAM"] != "ghostty" {
			t.Errorf("expected TERM_PROGRAM=ghostty from baseEnv to be preserved, got: %q", envLookup["TERM_PROGRAM"])
		}
	})

	t.Run("EmptyTermProgramInBaseEnvFallsBack", func(t *testing.T) {
		baseEnv := []string{
			"USER=testuser",
			"TERM_PROGRAM=",
		}
		sanitized := SanitizeAgyEnv(baseEnv, nil)
		envLookup := make(map[string]string)
		for _, e := range sanitized {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envLookup[parts[0]] = parts[1]
			}
		}
		if envLookup["TERM_PROGRAM"] == "" {
			t.Errorf("expected empty TERM_PROGRAM to be replaced with fallback, got empty string")
		}
	})

	t.Run("EnvMapTermProgramOverridePrecedence", func(t *testing.T) {
		baseEnv := []string{
			"TERM_PROGRAM=apple_terminal",
		}
		sanitized := SanitizeAgyEnv(baseEnv, map[string]string{"TERM_PROGRAM": "custom_override"})
		envLookup := make(map[string]string)
		for _, e := range sanitized {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envLookup[parts[0]] = parts[1]
			}
		}
		if envLookup["TERM_PROGRAM"] != "custom_override" {
			t.Errorf("expected envMap to override TERM_PROGRAM, got: %q", envLookup["TERM_PROGRAM"])
		}
	})

	t.Run("DeduplicateOverriddenKeys", func(t *testing.T) {
		baseEnv := []string{
			"PATH=/usr/bin",
			"PATH=/bin",
			"USER=testuser",
		}
		sanitized := SanitizeAgyEnv(baseEnv, map[string]string{"PATH": "/custom/bin"})
		pathCount := 0
		for _, e := range sanitized {
			if strings.HasPrefix(e, "PATH=") {
				pathCount++
				if e != "PATH=/custom/bin" {
					t.Errorf("unexpected PATH entry: %s", e)
				}
			}
		}
		if pathCount != 1 {
			t.Errorf("expected exactly 1 PATH entry after override, got %d", pathCount)
		}
	})

	t.Run("NilInputsHandling", func(t *testing.T) {
		sanitized := SanitizeAgyEnv(nil, nil)
		if len(sanitized) == 0 {
			t.Errorf("expected non-empty environment when baseEnv is nil")
		}
		hasTermProgram := false
		for _, e := range sanitized {
			if strings.HasPrefix(e, "TERM_PROGRAM=") && len(e) > len("TERM_PROGRAM=") {
				hasTermProgram = true
			}
		}
		if !hasTermProgram {
			t.Errorf("expected TERM_PROGRAM to be present in default environment")
		}
	})
}
