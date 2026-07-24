package profile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

const (
	lockFilename         = ".agys.lock"
	keychainLockFilename = ".keychain.lock"
)

// GetLockFilePath returns the absolute path to ~/.agys/.agys.lock.
func GetLockFilePath() (string, error) {
	agysDir, err := GetAgysDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(agysDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create agys directory %s: %w", agysDir, err)
	}
	return filepath.Join(agysDir, lockFilename), nil
}

// WithFileLock executes function fn under an exclusive OS file lock with a 5-second default timeout.
// This prevents cross-process race conditions when multiple agys CLI instances run concurrently.
func WithFileLock(ctx context.Context, fn func() error) error {
	lockPath, err := GetLockFilePath()
	if err != nil {
		return err
	}

	fileLock := flock.New(lockPath)

	lockCtx := ctx
	if lockCtx == nil {
		var cancel context.CancelFunc
		lockCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	locked, err := fileLock.TryLockContext(lockCtx, 50*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to acquire file lock %s: %w", lockPath, err)
	}
	if !locked {
		return fmt.Errorf("timeout waiting for agys file lock (%s); another agys process may be running", lockPath)
	}

	defer func() {
		_ = fileLock.Unlock()
	}()

	return fn()
}

// WithKeychainLock executes function fn under an exclusive OS file lock for macOS Keychain synchronization operations.
func WithKeychainLock(ctx context.Context, fn func() error) error {
	agysDir, err := GetAgysDir()
	if err != nil {
		return fn()
	}
	if err := os.MkdirAll(agysDir, 0700); err != nil {
		return fn()
	}
	lockPath := filepath.Join(agysDir, keychainLockFilename)
	fileLock := flock.New(lockPath)

	lockCtx := ctx
	if lockCtx == nil {
		lockCtx = context.Background()
	}
	var cancel context.CancelFunc
	lockCtx, cancel = context.WithTimeout(lockCtx, 5*time.Second)
	defer cancel()

	locked, err := fileLock.TryLockContext(lockCtx, 50*time.Millisecond)
	if err != nil || !locked {
		// If lock fails, proceed with fn to prevent blocking execution completely
		return fn()
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	return fn()
}

