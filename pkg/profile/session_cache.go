package profile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const sessionCacheFilename = "session_cache.json"

// CachedSessionInfo holds cached metadata for a conversation session.
type CachedSessionInfo struct {
	Profile         string    `json:"profile"`
	ConvID          string    `json:"conversation_id"`
	ModTime         time.Time `json:"mod_time"`
	ProjectPath     string    `json:"project_path"`
	ProjectName     string    `json:"project_name"`
	UserPrompt      string    `json:"user_prompt"`
	TranscriptMTime int64     `json:"transcript_mtime_unix_nano"`
	TranscriptSize  int64     `json:"transcript_size"`
}

// SessionCache maps key (e.g. "profile:convID") to CachedSessionInfo.
type SessionCache map[string]CachedSessionInfo

var (
	projectRootCache sync.Map // in-memory memoization for FindProjectRoot: path -> root
)

// GetSessionCachePath returns the path to session_cache.json in the agys directory.
func GetSessionCachePath() (string, error) {
	agysDir, err := GetAgysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(agysDir, sessionCacheFilename), nil
}

// LoadSessionCache reads the session cache from disk.
func LoadSessionCache() (SessionCache, error) {
	cachePath, err := GetSessionCachePath()
	if err != nil {
		return make(SessionCache), err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(SessionCache), nil
		}
		return make(SessionCache), err
	}

	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		// If cache is corrupted, return empty cache to self-heal
		return make(SessionCache), nil
	}

	if cache == nil {
		cache = make(SessionCache)
	}

	return cache, nil
}

// PruneSessionCache removes entries from cache whose key is not present in validKeys.
func PruneSessionCache(cache SessionCache, validKeys map[string]bool) int {
	if cache == nil || validKeys == nil {
		return 0
	}
	pruned := 0
	for k := range cache {
		if !validKeys[k] {
			delete(cache, k)
			pruned++
		}
	}
	return pruned
}

// SaveSessionCache writes the session cache to disk atomically under WithFileLock.
func SaveSessionCache(cache SessionCache) error {
	if cache == nil {
		return nil
	}

	cachePath, err := GetSessionCachePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return WithFileLock(context.Background(), func() error {
		return WriteFileAtomic(cachePath, data, 0600)
	})
}

// UpdateSessionCache acquires WithFileLock, reloads disk cache, applies updater, and writes atomically.
func UpdateSessionCache(updater func(diskCache SessionCache) error) error {
	cachePath, err := GetSessionCachePath()
	if err != nil {
		return err
	}

	return WithFileLock(context.Background(), func() error {
		diskCache, _ := LoadSessionCache()
		if diskCache == nil {
			diskCache = make(SessionCache)
		}
		if err := updater(diskCache); err != nil {
			return err
		}
		data, err := json.MarshalIndent(diskCache, "", "  ")
		if err != nil {
			return err
		}
		return WriteFileAtomic(cachePath, data, 0600)
	})
}

