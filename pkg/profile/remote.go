package profile

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultHubPort is the default port used by Antigravity Remote Control / hub server.
const DefaultHubPort = 4400

const daemonInfoFilename = "remote_daemon.json"
const daemonLogFilename = "remote.log"

// RemoteDaemonInfo contains runtime tracking info for a running background remote control daemon.
type RemoteDaemonInfo struct {
	Profile   string    `json:"profile"`
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Name      string    `json:"name,omitempty"`
	StartedAt time.Time `json:"started_at"`
	LogPath   string    `json:"log_path"`
}

// GetDaemonInfoPath returns the metadata file path for a profile's remote daemon.
func GetDaemonInfoPath(profileDir string) string {
	return filepath.Join(profileDir, daemonInfoFilename)
}

// GetDaemonLogPath returns the log file path for a profile's remote daemon.
func GetDaemonLogPath(profileDir string) string {
	return filepath.Join(profileDir, daemonLogFilename)
}

// GetRemoteDaemonInfo retrieves the active daemon info for profileName if it is currently running.
// If the process is no longer alive, it cleans up stale metadata and returns false.
func GetRemoteDaemonInfo(profileName string) (*RemoteDaemonInfo, bool, error) {
	exists, profileDir, err := Exists(profileName)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, fmt.Errorf("profile %q does not exist", profileName)
	}

	infoPath := GetDaemonInfoPath(profileDir)
	data, err := os.ReadFile(infoPath)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to read daemon info file: %w", err)
	}

	var info RemoteDaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		_ = os.Remove(infoPath)
		return nil, false, nil
	}

	if !isProcessAlive(info.PID) {
		_ = os.Remove(infoPath)
		return nil, false, nil
	}

	return &info, true, nil
}

// SaveRemoteDaemonInfo writes daemon metadata atomically.
func SaveRemoteDaemonInfo(profileDir string, info *RemoteDaemonInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(GetDaemonInfoPath(profileDir), data, 0600)
}

// RemoveRemoteDaemonInfo removes the daemon metadata file.
func RemoveRemoteDaemonInfo(profileDir string) error {
	return os.Remove(GetDaemonInfoPath(profileDir))
}

// ListRunningRemoteDaemons returns a sorted list of all active remote daemons across all profiles.
func ListRunningRemoteDaemons() ([]RemoteDaemonInfo, error) {
	profiles, err := List()
	if err != nil {
		return nil, err
	}

	var running []RemoteDaemonInfo
	for _, p := range profiles {
		info, isRunning, _ := GetRemoteDaemonInfo(p)
		if isRunning && info != nil {
			running = append(running, *info)
		}
	}

	sort.SliceStable(running, func(i, j int) bool {
		return running[i].Profile < running[j].Profile
	})

	return running, nil
}

// StopRemoteDaemon gracefully stops the running remote daemon for profileName.
func StopRemoteDaemon(profileName string) (*RemoteDaemonInfo, error) {
	info, isRunning, err := GetRemoteDaemonInfo(profileName)
	if err != nil {
		return nil, err
	}
	if !isRunning || info == nil {
		return nil, fmt.Errorf("no active remote control daemon running for profile %q", profileName)
	}

	proc, err := os.FindProcess(info.PID)
	if err == nil && proc != nil {
		// Send SIGTERM for graceful termination
		_ = proc.Signal(syscall.SIGTERM)

		// Wait up to 2 seconds for graceful shutdown
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !isProcessAlive(info.PID) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Force kill if still running
		if isProcessAlive(info.PID) {
			_ = proc.Kill()
			time.Sleep(100 * time.Millisecond)
		}
	}

	profileDir, _ := GetProfileDir(profileName)
	_ = RemoveRemoteDaemonInfo(profileDir)

	return info, nil
}

// StartRemoteDaemon initializes profile environment, checks ports, and launches the remote daemon.
func StartRemoteDaemon(ctx context.Context, profileName string, port int, instanceName string, skipPermissions bool, foreground bool, extraArgs []string) (*RemoteDaemonInfo, error) {
	var targetProfile string
	if IsAuto(profileName) {
		selected, score, err := SelectBestProfile(ctx)
		if err != nil {
			return nil, fmt.Errorf("auto profile selection failed: %w", err)
		}
		targetProfile = selected
		scoreStr := fmt.Sprintf("%.1f%%", score*100)
		if score < 0 {
			scoreStr = "N/A"
		}
		fmt.Fprintf(os.Stderr, "[agys] Auto-selected profile %q (5h Gemini quota: %s)\n", targetProfile, scoreStr)
	} else {
		exists, _, err := Exists(profileName)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("profile %q does not exist", profileName)
		}
		targetProfile = profileName
	}

	profileDir, err := GetProfileDir(targetProfile)
	if err != nil {
		return nil, err
	}

	// Check if already running
	existingInfo, isRunning, _ := GetRemoteDaemonInfo(targetProfile)
	if isRunning && existingInfo != nil {
		return existingInfo, fmt.Errorf("remote daemon for profile %q is already running (PID: %d, Port: %d). Stop it first with `agys remote stop %s` or use --force", targetProfile, existingInfo.PID, existingInfo.Port, targetProfile)
	}

	// Synchronize tokens, onboarding status, and trusted workspaces
	_ = SyncAllTokenLocations(profileDir)
	_ = EnsureOnboardingCompleted(profileDir)
	_ = EnsureKeychain(profileDir)
	SyncDiskTokenToKeychain(profileDir)
	_ = SyncTrustedWorkspaces()

	// Build arguments
	args := []string{"--remote-control"}
	if port > 0 {
		args = append(args, "--hub-port", strconv.Itoa(port))
	}
	if instanceName != "" {
		args = append(args, "--remote-control-name", instanceName)
	}
	if skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, extraArgs...)

	// Ensure port availability
	finalArgs, actualPort, err := EnsureAvailableHubPort(args)
	if err != nil {
		return nil, err
	}

	logPath := filepath.Join(profileDir, daemonLogFilename)

	if foreground {
		runErr := RunCmdWithSignals(ctx, profileDir, finalArgs...)
		return &RemoteDaemonInfo{
			Profile:   targetProfile,
			Port:      actualPort,
			Name:      instanceName,
			StartedAt: time.Now(),
			LogPath:   logPath,
		}, runErr
	}

	// Background detached launch
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}

	execCmd := BuildCmd(profileDir, finalArgs...)
	execCmd.Stdout = logFile
	execCmd.Stderr = logFile
	execCmd.Stdin = nil

	setupDetachedProcess(execCmd)

	if err := execCmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("failed to start remote daemon: %w", err)
	}
	_ = logFile.Close()

	info := &RemoteDaemonInfo{
		Profile:   targetProfile,
		PID:       execCmd.Process.Pid,
		Port:      actualPort,
		Name:      instanceName,
		StartedAt: time.Now(),
		LogPath:   logPath,
	}

	if err := SaveRemoteDaemonInfo(profileDir, info); err != nil {
		_ = execCmd.Process.Kill()
		return nil, fmt.Errorf("failed to save daemon tracking info: %w", err)
	}

	return info, nil
}

// IsRemoteControlRequested checks if --remote-control is present in args.
func IsRemoteControlRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--remote-control" || strings.HasPrefix(arg, "--remote-control=") {
			return true
		}
	}
	return false
}

// IsPortAvailable checks if a TCP port on localhost is available to bind.
func IsPortAvailable(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// FindNextAvailablePort scans starting from startPort up to (startPort + maxScan) for an open TCP port.
func FindNextAvailablePort(startPort int, maxScan int) (int, error) {
	for p := startPort; p <= startPort+maxScan && p <= 65535; p++ {
		if IsPortAvailable(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no available port found in range %d-%d", startPort, startPort+maxScan)
}

// EnsureAvailableHubPort checks if --remote-control is used, and verifies/resolves --hub-port availability.
// If port 4400 or the user-specified port is already in use, it automatically allocates the next available port.
func EnsureAvailableHubPort(args []string) ([]string, int, error) {
	if !IsRemoteControlRequested(args) {
		return args, 0, nil
	}

	hasHubPort := false
	var specifiedPort int
	portArgIndex := -1
	isFlagWithValue := false // e.g. --hub-port=4401

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--hub-port" || arg == "-hub-port" {
			hasHubPort = true
			portArgIndex = i
			if i+1 < len(args) {
				p, err := strconv.Atoi(args[i+1])
				if err == nil {
					specifiedPort = p
				}
			}
			break
		} else if strings.HasPrefix(arg, "--hub-port=") || strings.HasPrefix(arg, "-hub-port=") {
			hasHubPort = true
			portArgIndex = i
			isFlagWithValue = true
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) == 2 {
				p, err := strconv.Atoi(parts[1])
				if err == nil {
					specifiedPort = p
				}
			}
			break
		}
	}

	targetPort := DefaultHubPort
	if hasHubPort && specifiedPort > 0 {
		targetPort = specifiedPort
	}

	if IsPortAvailable(targetPort) {
		return args, targetPort, nil
	}

	// Port is in use, find next available port
	freePort, err := FindNextAvailablePort(targetPort+1, 100)
	if err != nil {
		// Fallback to scan starting from DefaultHubPort + 1
		freePort, err = FindNextAvailablePort(DefaultHubPort+1, 100)
		if err != nil {
			return args, targetPort, fmt.Errorf("failed to allocate free hub port: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "[agys] Hub port %d is in use, auto-allocated available port: %d\n", targetPort, freePort)

	finalArgs := make([]string, 0, len(args)+2)
	if !hasHubPort {
		finalArgs = append(finalArgs, args...)
		finalArgs = append(finalArgs, "--hub-port", strconv.Itoa(freePort))
	} else {
		for i := 0; i < len(args); i++ {
			if i == portArgIndex {
				if isFlagWithValue {
					finalArgs = append(finalArgs, fmt.Sprintf("--hub-port=%d", freePort))
				} else {
					finalArgs = append(finalArgs, "--hub-port", strconv.Itoa(freePort))
					i++ // Skip old value
				}
			} else {
				finalArgs = append(finalArgs, args[i])
			}
		}
	}

	return finalArgs, freePort, nil
}

const standardStatePbtxtContent = `post_onboarding: {
  completed_steps: POST_ONBOARDING_STEP_TYPE_MANAGER_WELCOME
  completed_steps: POST_ONBOARDING_STEP_TYPE_USAGE_MODE
  completed_steps: POST_ONBOARDING_STEP_TYPE_AGENT_CONFIGURATION
  completed_steps: POST_ONBOARDING_STEP_TYPE_ADD_WORKSPACE
}
seen_nuxs: {
  uids: 31
  uids: 29
  uids: 27
  uids: 38
  uids: 24
}
agent_onboarding_completed: AGENT_ONBOARDING_STATE_COMPLETED
migrate_convos_into_projects: MIGRATION_STATUS_COMPLETED
`

// EnsureOnboardingCompleted ensures the profile has completed onboarding state,
// preventing web UI from getting stuck on disabled welcome / sign-in wizard buttons.
func EnsureOnboardingCompleted(profileDir string) error {
	stateDirs := []string{
		filepath.Join(profileDir, ".gemini"),
		filepath.Join(profileDir, ".gemini", "antigravity-cli"),
		filepath.Join(profileDir, ".gemini", "antigravity"),
	}

	for _, dir := range stateDirs {
		_ = os.MkdirAll(dir, 0700)

		stateFiles := []string{
			filepath.Join(dir, "antigravity_state.pbtxt"),
			filepath.Join(dir, "jetski_state.pbtxt"),
		}

		for _, stateFile := range stateFiles {
			existingData, err := os.ReadFile(stateFile)
			if err != nil || len(existingData) == 0 {
				_ = WriteFileAtomic(stateFile, []byte(standardStatePbtxtContent), 0600)
			} else if !strings.Contains(string(existingData), "AGENT_ONBOARDING_STATE_COMPLETED") {
				appended := strings.TrimSpace(string(existingData)) + "\nagent_onboarding_completed: AGENT_ONBOARDING_STATE_COMPLETED\n"
				_ = WriteFileAtomic(stateFile, []byte(appended), 0600)
			}
		}

		// Ensure installation_id exists
		installIDPath := filepath.Join(dir, "installation_id")
		if _, err := os.Stat(installIDPath); os.IsNotExist(err) {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			uuidStr := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
			_ = WriteFileAtomic(installIDPath, []byte(uuidStr+"\n"), 0600)
		}
	}

	return nil
}
