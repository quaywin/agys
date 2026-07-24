package profile

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestWithFileLock_ExecutesFunction(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)

	executed := false
	err := WithFileLock(context.Background(), func() error {
		executed = true
		return nil
	})

	if err != nil {
		t.Fatalf("WithFileLock returned error: %v", err)
	}

	if !executed {
		t.Errorf("expected function to be executed under lock")
	}
}

func TestWithFileLock_MutualExclusion(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)

	var counter int
	var wg sync.WaitGroup

	numGoroutines := 5
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithFileLock(context.Background(), func() error {
				current := counter
				time.Sleep(10 * time.Millisecond)
				counter = current + 1
				return nil
			})
			if err != nil {
				t.Errorf("WithFileLock failed in goroutine: %v", err)
			}
		}()
	}

	wg.Wait()

	if counter != numGoroutines {
		t.Errorf("expected counter to be %d under serial lock, got %d", numGoroutines, counter)
	}
}

func TestWithFileLock_TimeoutWhenLocked(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)

	lockPath := filepath.Join(tempDir, lockFilename)
	fLock := flock.New(lockPath)

	locked, err := fLock.TryLock()
	if err != nil || !locked {
		t.Fatalf("failed to manually acquire lock for test: %v", err)
	}
	defer func() {
		_ = fLock.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = WithFileLock(ctx, func() error {
		return nil
	})

	if err == nil {
		t.Fatalf("expected WithFileLock to fail due to lock timeout, but succeeded")
	}
}
