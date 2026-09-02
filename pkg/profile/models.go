package profile

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultGeminiModel is the hardcoded fallback Gemini model if no discovery has occurred and offline.
	DefaultGeminiModel = "gemini-3.8-flash"
	// DefaultGeminiEffort is the default reasoning effort for Gemini models.
	DefaultGeminiEffort = "high"
	// ModelCacheTTL is the duration after which cached models are considered stale and refreshed.
	ModelCacheTTL = 12 * time.Hour
)

// GeminiModelVersion represents parsed major, minor, patch version numbers.
type GeminiModelVersion struct {
	Major int
	Minor int
	Patch int
}

// GreaterThan returns true if v is strictly greater than other numerically.
func (v GeminiModelVersion) GreaterThan(other GeminiModelVersion) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch > other.Patch
}

var geminiVersionRegex = regexp.MustCompile(`(?i)gemini-(\d+)(?:\.(\d+))?(?:\.(\d+))?-(flash|pro)`)

// ExtractGeminiVersion extracts the numerical version components from a model ID like "gemini-3.8-flash".
func ExtractGeminiVersion(modelID string) (GeminiModelVersion, bool) {
	matches := geminiVersionRegex.FindStringSubmatch(modelID)
	if len(matches) < 2 {
		return GeminiModelVersion{}, false
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return GeminiModelVersion{}, false
	}
	minor := 0
	if len(matches) > 2 && matches[2] != "" {
		minor, _ = strconv.Atoi(matches[2])
	}
	patch := 0
	if len(matches) > 3 && matches[3] != "" {
		patch, _ = strconv.Atoi(matches[3])
	}
	return GeminiModelVersion{Major: major, Minor: minor, Patch: patch}, true
}

// DiscoveredModels stores the models detected from the agy CLI.
type DiscoveredModels struct {
	FetchedAt   time.Time `json:"fetched_at"`
	LatestFlash string    `json:"latest_flash"`
	LatestPro   string    `json:"latest_pro"`
	AllModels   []string  `json:"all_models"`
}

var (
	modelCacheLock sync.RWMutex
	cachedModels   *DiscoveredModels
	discoveryMu    sync.Mutex
)

// GetModelCachePath returns the path to the cached discovered models file.
func GetModelCachePath() string {
	agysDir, err := GetAgysDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		agysDir = filepath.Join(home, ".agys")
	}
	return filepath.Join(agysDir, "models_cache.json")
}

// ParseAgyModelsOutput extracts model IDs and finds the numerically highest Flash and Pro models.
func ParseAgyModelsOutput(output string) *DiscoveredModels {
	res := &DiscoveredModels{
		FetchedAt: time.Now(),
		AllModels: []string{},
	}
	seen := make(map[string]bool)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Fetching") || strings.HasPrefix(line, "Available") {
			continue
		}
		parts := strings.Split(line, "\t")
		modelID := strings.TrimSpace(parts[0])
		if modelID == "" {
			continue
		}

		// Strip -high, -medium, -low suffixes for base model ID
		baseModel := modelID
		for _, suffix := range []string{"-high", "-medium", "-low"} {
			if strings.HasSuffix(baseModel, suffix) {
				baseModel = strings.TrimSuffix(baseModel, suffix)
				break
			}
		}

		if !seen[baseModel] {
			seen[baseModel] = true
			res.AllModels = append(res.AllModels, baseModel)
		}
	}

	// Numerically evaluate highest version for Flash and Pro series
	var highestFlashVer GeminiModelVersion
	var highestProVer GeminiModelVersion

	for _, m := range res.AllModels {
		v, ok := ExtractGeminiVersion(m)
		if !ok {
			continue
		}
		if strings.Contains(m, "flash") {
			isLite := strings.Contains(m, "lite")
			if res.LatestFlash == "" || v.GreaterThan(highestFlashVer) {
				res.LatestFlash = m
				highestFlashVer = v
			} else if v == highestFlashVer && !isLite && strings.Contains(res.LatestFlash, "lite") {
				// Prefer full Flash over Flash Lite if version matches
				res.LatestFlash = m
			}
		} else if strings.Contains(m, "pro") {
			if res.LatestPro == "" || v.GreaterThan(highestProVer) {
				res.LatestPro = m
				highestProVer = v
			}
		}
	}

	if res.LatestFlash == "" {
		res.LatestFlash = DefaultGeminiModel
	}
	return res
}

// ReadCachedDiscoveredModels reads the cached discovered models from disk.
func ReadCachedDiscoveredModels() *DiscoveredModels {
	modelCacheLock.RLock()
	if cachedModels != nil && time.Since(cachedModels.FetchedAt) < ModelCacheTTL {
		m := cachedModels
		modelCacheLock.RUnlock()
		return m
	}
	modelCacheLock.RUnlock()

	cachePath := GetModelCachePath()
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}

	var dm DiscoveredModels
	if err := json.Unmarshal(data, &dm); err != nil {
		return nil
	}

	if time.Since(dm.FetchedAt) > ModelCacheTTL {
		return nil
	}

	modelCacheLock.Lock()
	cachedModels = &dm
	modelCacheLock.Unlock()

	return &dm
}

// SaveCachedDiscoveredModels saves the discovered models to disk.
func SaveCachedDiscoveredModels(dm *DiscoveredModels) error {
	if dm == nil {
		return nil
	}
	modelCacheLock.Lock()
	cachedModels = dm
	modelCacheLock.Unlock()

	cachePath := GetModelCachePath()
	_ = os.MkdirAll(filepath.Dir(cachePath), 0755)
	data, err := json.MarshalIndent(dm, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(cachePath, append(data, '\n'), 0600)
}

// DiscoverLatestModels queries `agy models` and caches the parsed result.
func DiscoverLatestModels() (*DiscoveredModels, error) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "agy", "models")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	dm := ParseAgyModelsOutput(string(out))
	_ = SaveCachedDiscoveredModels(dm)
	return dm, nil
}

// IsAgyBinaryNewerThan checks if the agy CLI binary has been updated since the given timestamp.
func IsAgyBinaryNewerThan(t time.Time) bool {
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		return false
	}
	fi, err := os.Stat(agyPath)
	if err != nil {
		return false
	}
	return fi.ModTime().After(t)
}

// GetOrRefreshModels retrieves the latest model metadata, auto-refreshing in background if stale or agy was updated.
func GetOrRefreshModels() *DiscoveredModels {
	cached := ReadCachedDiscoveredModels()

	// Check if agy binary was updated after cache was generated
	if cached != nil && IsAgyBinaryNewerThan(cached.FetchedAt) {
		// agy was updated: refresh cache in background
		go func() {
			_, _ = DiscoverLatestModels()
		}()
		return cached
	}

	if cached != nil {
		return cached
	}

	// No cache or expired: attempt discovery or fallback
	// If disk cache exists (even if older than TTL), use it and refresh in background
	cachePath := GetModelCachePath()
	if data, err := os.ReadFile(cachePath); err == nil {
		var dm DiscoveredModels
		if json.Unmarshal(data, &dm) == nil && dm.LatestFlash != "" {
			go func() {
				_, _ = DiscoverLatestModels()
			}()
			return &dm
		}
	}

	// Cold start with zero cache: discover synchronously with 3s timeout
	if dm, err := DiscoverLatestModels(); err == nil && dm != nil {
		return dm
	}

	return &DiscoveredModels{
		FetchedAt:   time.Now(),
		LatestFlash: DefaultGeminiModel,
		AllModels:   []string{DefaultGeminiModel},
	}
}

// GetLatestGeminiModel returns the highest discovered Gemini Flash model,
// automatically detecting newer versions (3.8, 3.9, 4.0, etc.) from `agy`.
func GetLatestGeminiModel() string {
	dm := GetOrRefreshModels()
	if dm != nil && dm.LatestFlash != "" {
		return dm.LatestFlash
	}
	return DefaultGeminiModel
}

// ModelSupportsEffort returns true if the specified model supports reasoning effort (e.g. Gemini 3.x Flash/Pro, Gemini Flash).
func ModelSupportsEffort(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return true
	}
	if strings.Contains(m, "claude") || strings.Contains(m, "gpt") || strings.Contains(m, "deepseek") || strings.Contains(m, "qwen") {
		return false
	}
	if strings.Contains(m, "flash") {
		return true
	}
	if v, ok := ExtractGeminiVersion(m); ok && v.Major >= 3 {
		return true
	}
	return false
}
