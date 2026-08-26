package profile

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
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
	agysSep := string(filepath.Separator) + ".agys"
	if idx := strings.Index(homeDir, agysSep); idx != -1 {
		homeDir = homeDir[:idx]
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
	_ = EnsureKeychain(profileDir)
	_ = SyncHerdrIntegration(profileDir)
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

	_ = SyncHerdrIntegration(newDir)
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

	return WithFileLock(context.Background(), func() error {
		agysDir, err := GetAgysDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(agysDir, 0700); err != nil {
			return fmt.Errorf("failed to create agys directory %s: %w", agysDir, err)
		}

		currentFile := filepath.Join(agysDir, currentProfileFilename)
		if err := WriteFileAtomic(currentFile, []byte(name+"\n"), 0600); err != nil {
			return fmt.Errorf("failed to write current profile file: %w", err)
		}

		return nil
	})
}

// UnsetCurrent removes the default active profile setting.
func UnsetCurrent() error {
	return WithFileLock(context.Background(), func() error {
		agysDir, err := GetAgysDir()
		if err != nil {
			return err
		}
		currentFile := filepath.Join(agysDir, currentProfileFilename)
		if err := os.Remove(currentFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove current profile file: %w", err)
		}
		return nil
	})
}

// BuildCmd constructs an exec.Cmd for running `agy` with isolated profile environment variables.
func BuildCmd(profileDir string, args ...string) *exec.Cmd {
	return BuildCmdContext(context.Background(), profileDir, args...)
}

// BuildCmdContext constructs an exec.Cmd with context cancellation for running `agy` with isolated profile environment variables.
func BuildCmdContext(ctx context.Context, profileDir string, args ...string) *exec.Cmd {
	_ = EnsureKeychain(profileDir)
	_ = SyncHerdrIntegration(profileDir)

	agyPath, err := exec.LookPath("agy")
	if err != nil {
		if userHome, errHome := os.UserHomeDir(); errHome == nil {
			agysSep := string(filepath.Separator) + ".agys"
			if idx := strings.Index(userHome, agysSep); idx != -1 {
				userHome = userHome[:idx]
			}
			candidates := []string{
				filepath.Join(userHome, ".local", "bin", "agy"),
				filepath.Join(userHome, "bin", "agy"),
				filepath.Join(userHome, ".gemini", "antigravity-cli", "bin", "agy"),
				"/usr/local/bin/agy",
			}
			for _, candidate := range candidates {
				if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
					agyPath = candidate
					err = nil
					break
				}
			}
		}
	}
	if err != nil {
		agyPath = "agy"
	}

	cmd := exec.CommandContext(ctx, agyPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	envMap := map[string]string{
		"AGYS_PROFILE":    filepath.Base(profileDir),
		"HOME":            profileDir,
		"USERPROFILE":     profileDir,
		"GEMINI_DIR":      filepath.Join(profileDir, ".gemini"),
		"GEMINI_CLI_DIR":  filepath.Join(profileDir, ".gemini", "antigravity-cli"),
		"ANTIGRAVITY_DIR": filepath.Join(profileDir, ".gemini", "antigravity-cli"),
		"XDG_CONFIG_HOME": filepath.Join(profileDir, ".config"),
		"XDG_DATA_HOME":   filepath.Join(profileDir, ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(profileDir, ".cache"),
	}

	env := os.Environ()
	newEnv := make([]string, 0, len(env))
	seen := make(map[string]bool)

	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			if newVal, ok := envMap[parts[0]]; ok {
				newEnv = append(newEnv, parts[0]+"="+newVal)
				seen[parts[0]] = true
				continue
			}
		}
		newEnv = append(newEnv, e)
	}

	for k, v := range envMap {
		if !seen[k] {
			newEnv = append(newEnv, k+"="+v)
		}
	}

	cmd.Env = newEnv
	return cmd
}

// GetTokenFilePaths returns all candidate token file paths for a profile directory in priority order.
func GetTokenFilePaths(profileDir string) []string {
	return []string{
		filepath.Join(profileDir, ".gemini", "antigravity-cli", "antigravity-oauth-token"),
		filepath.Join(profileDir, ".gemini", "antigravity", "antigravity-oauth-token"),
		filepath.Join(profileDir, ".gemini", "jetski-standalone-oauth-token"),
		filepath.Join(profileDir, ".gemini", "antigravity-cli", "jetski-standalone-oauth-token"),
		filepath.Join(profileDir, ".gemini", "antigravity", "jetski-standalone-oauth-token"),
		filepath.Join(profileDir, ".gemini", "antigravity-ide", "antigravity-oauth-token"),
		filepath.Join(profileDir, ".gemini", "oauth_creds.json"),
	}
}

// ReadRawTokenData reads token data from the first existing candidate file in profileDir.
func ReadRawTokenData(profileDir string) ([]byte, error) {
	for _, p := range GetTokenFilePaths(profileDir) {
		data, err := os.ReadFile(p)
		if err == nil && len(bytes.TrimSpace(data)) > 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("token file not found (not logged in)")
}

// WriteTokenToProfile writes token JSON to standard token file paths in profileDir.
func WriteTokenToProfile(profileDir string, rawJSON string) error {
	trimmed := strings.TrimSpace(rawJSON) + "\n"
	for _, p := range GetTokenFilePaths(profileDir) {
		_ = os.MkdirAll(filepath.Dir(p), 0700)
		_ = WriteFileAtomic(p, []byte(trimmed), 0600)
	}
	return nil
}

// SyncAllTokenLocations synchronizes existing token data across all standard token paths in profileDir.
func SyncAllTokenLocations(profileDir string) error {
	data, err := ReadRawTokenData(profileDir)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return WriteTokenToProfile(profileDir, string(data))
}

// ClearKeychainToken removes the cached generic password item from macOS Keychain.
// This forces `agy` to load the profile-isolated token file from disk instead of using a stale token from another profile.
func ClearKeychainToken() {
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "security", "delete-generic-password", "-s", "gemini", "-a", "antigravity").Run()
	}
}

// SyncDiskTokenToKeychain loads the profile's isolated token file from disk and seeds macOS Keychain if present.
func SyncDiskTokenToKeychain(profileDir string) {
	if runtime.GOOS != "darwin" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = WithKeychainLock(ctx, func() error {
		data, err := ReadRawTokenData(profileDir)
		if err != nil || len(bytes.TrimSpace(data)) == 0 {
			ClearKeychainToken()
			return nil
		}

		var tok struct {
			Token struct {
				AccessToken string `json:"access_token"`
			} `json:"token"`
		}
		if json.Unmarshal(data, &tok) != nil || tok.Token.AccessToken == "" {
			ClearKeychainToken()
			return nil
		}

		b64Val := "go-keyring-base64:" + base64.StdEncoding.EncodeToString(bytes.TrimSpace(data))
		secCtx, secCancel := context.WithTimeout(ctx, 3*time.Second)
		defer secCancel()
		_ = exec.CommandContext(secCtx, "security", "add-generic-password", "-s", "gemini", "-a", "antigravity", "-w", b64Val, "-U").Run()
		return nil
	})
}

// ReadTokenFromDir reads the oauth token file directly from a profile directory path.
func ReadTokenFromDir(profileDir string) (*OAuthToken, error) {
	data, err := ReadRawTokenData(profileDir)
	if err != nil {
		return nil, err
	}

	var oauthToken OAuthToken
	if err := json.Unmarshal(data, &oauthToken); err != nil {
		return nil, fmt.Errorf("failed to parse token JSON: %w", err)
	}
	return &oauthToken, nil
}

// SyncKeychainTokenToDisk captures any new token saved to macOS Keychain (e.g. after login) and persists it to the profile directory.
// If initialRefreshToken was non-empty before agy ran, but the disk token is missing after agy ran (e.g. user logged out),
// it invalidates email cache and refrains from restoring stale Keychain tokens.
func SyncKeychainTokenToDisk(profileDir string, initialRefreshToken string) {
	if runtime.GOOS != "darwin" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = WithKeychainLock(ctx, func() error {
		defer ClearKeychainToken()

		// Read disk token AFTER agy execution
		diskTok, readDiskErr := ReadTokenFromDir(profileDir)

		secCtx, secCancel := context.WithTimeout(ctx, 3*time.Second)
		defer secCancel()
		out, err := exec.CommandContext(secCtx, "security", "find-generic-password", "-s", "gemini", "-a", "antigravity", "-w").Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			// Keychain is empty
			if initialRefreshToken != "" && readDiskErr != nil {
				// Token was present before execution, but disk token was deleted during execution (e.g. user typed /logout)
				// Clean up email cache
				cachePath := filepath.Join(profileDir, emailFilename)
				_ = os.Remove(cachePath)
			}
			return nil
		}

		tokenStr := strings.TrimSpace(string(out))
		rawJSON := tokenStr
		if strings.HasPrefix(tokenStr, "go-keyring-base64:") {
			b64Part := strings.TrimPrefix(tokenStr, "go-keyring-base64:")
			decoded, decodeErr := base64.StdEncoding.DecodeString(b64Part)
			if decodeErr == nil {
				rawJSON = string(decoded)
			}
		}

		var keyTok struct {
			Token struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			} `json:"token"`
		}
		if json.Unmarshal([]byte(rawJSON), &keyTok) != nil || keyTok.Token.AccessToken == "" {
			return nil
		}

		// Case 1: Disk token exists after agy run (agy wrote token to disk during run or updated it)
		if readDiskErr == nil && diskTok != nil && diskTok.Token.AccessToken != "" {
			// If initialRefreshToken was recorded and keyTok's refresh token differs from initialRefreshToken,
			// keyTok MUST belong to another profile that ran concurrently and wrote to macOS Keychain.
			// We must reject keyTok and keep diskTok intact.
			if initialRefreshToken != "" && keyTok.Token.RefreshToken != "" && keyTok.Token.RefreshToken != initialRefreshToken {
				fmt.Fprintf(os.Stderr, "[agys] Warning: Keychain token belongs to another profile (refresh token mismatch). Keeping isolated profile token on disk.\n")
				return nil
			}

			// If disk token already has a refresh token and keyTok has a different refresh token, reject it
			if diskTok.Token.RefreshToken != "" && keyTok.Token.RefreshToken != "" && keyTok.Token.RefreshToken != diskTok.Token.RefreshToken {
				fmt.Fprintf(os.Stderr, "[agys] Warning: Keychain token mismatch detected. Keeping isolated profile token on disk.\n")
				return nil
			}

			// If Keychain token has a refreshed access token (same account/refresh token), persist to disk
			if keyTok.Token.AccessToken != diskTok.Token.AccessToken {
				cachePath := filepath.Join(profileDir, emailFilename)
				_ = os.Remove(cachePath) // Force email re-fetch for updated token

				_ = WriteTokenToProfile(profileDir, rawJSON)
			}
			return nil
		}

		// Case 2: Disk token does NOT exist after agy run (e.g. user typed /logout, or fresh profile)
		// Check if Keychain contains a NEW token from a fresh login during the session
		if keyTok.Token.AccessToken != "" {
			if initialRefreshToken != "" && keyTok.Token.RefreshToken == initialRefreshToken {
				// Keychain still contains the OLD token from before /logout, but user logged out without logging in to a new account.
				// Do NOT restore the old token. Clean up email cache.
				cachePath := filepath.Join(profileDir, emailFilename)
				_ = os.Remove(cachePath)
				return nil
			}

			// User logged in to a new account (or fresh login on empty profile)! Save new token from Keychain to disk.
			cachePath := filepath.Join(profileDir, emailFilename)
			_ = os.Remove(cachePath) // Force email re-fetch for new token

			_ = WriteTokenToProfile(profileDir, rawJSON)
			return nil
		}

		// Case 3: Disk token does not exist AND Keychain is empty
		if initialRefreshToken != "" {
			// Profile had a token before, but user logged out and did not log back in
			cachePath := filepath.Join(profileDir, emailFilename)
			_ = os.Remove(cachePath)
		}

		return nil
	})
}

// EnsureKeychain links the profile's Library/Keychains directory to the user's main Library/Keychains on macOS.
// This prevents macOS SecurityAgent from showing system UI popup dialogs ("A keychain cannot be found")
// while avoiding running security CLI commands that prompt for passwords.
func EnsureKeychain(profileDir string) error {
	SyncDiskTokenToKeychain(profileDir)

	if runtime.GOOS != "darwin" {
		return nil
	}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Determine the real user home directory if HOME is currently overridden to a profile folder
	agysSep := string(filepath.Separator) + ".agys"
	if idx := strings.Index(userHome, agysSep); idx != -1 {
		userHome = userHome[:idx]
	}

	realKeychainsDir := filepath.Join(userHome, "Library", "Keychains")
	if _, err := os.Stat(realKeychainsDir); os.IsNotExist(err) {
		return nil
	}

	profileLibDir := filepath.Join(profileDir, "Library")
	if err := os.MkdirAll(profileLibDir, 0700); err != nil {
		return fmt.Errorf("failed to create Library directory in profile: %w", err)
	}

	profileKeychainsDir := filepath.Join(profileLibDir, "Keychains")

	// If profileKeychainsDir exists, check if it's already symlinked correctly
	if info, err := os.Lstat(profileKeychainsDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(profileKeychainsDir)
			if err == nil && target == realKeychainsDir {
				return nil
			}
			// Remove outdated symlink
			_ = os.Remove(profileKeychainsDir)
		} else {
			// Remove existing directory to replace with symlink
			_ = os.RemoveAll(profileKeychainsDir)
		}
	}

	// Create symlink from profile's Library/Keychains -> user's main Library/Keychains
	if err := os.Symlink(realKeychainsDir, profileKeychainsDir); err != nil {
		return fmt.Errorf("failed to symlink Keychains directory: %w", err)
	}

	return nil
}

// IsCLIConfigured checks if a profile has CLI authentication configured.
func IsCLIConfigured(name string) bool {
	profileDir, err := GetProfileDir(name)
	if err != nil {
		return false
	}
	for _, p := range GetTokenFilePaths(profileDir) {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Size() > 0 {
			return true
		}
	}
	return false
}

// IsIDEConfigured checks if a profile has IDE user data configured/initialized.
func IsIDEConfigured(name string) bool {
	profileDir, err := GetProfileDir(name)
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(profileDir, "ide-data", "User")); err == nil {
		return true
	}
	entries, err := os.ReadDir(filepath.Join(profileDir, "ide-data"))
	return err == nil && len(entries) > 0
}

// GetConfigSummary returns a formatted summary of configured tools for a profile ("CLI, IDE", "CLI", "IDE", or "-").
func GetConfigSummary(name string) string {
	cli := IsCLIConfigured(name)
	ide := IsIDEConfigured(name)
	if cli && ide {
		return "CLI, IDE"
	}
	if cli {
		return "CLI"
	}
	if ide {
		return "IDE"
	}
	return "-"
}

