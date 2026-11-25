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
	// DefaultHydrogenModel is the hardcoded fallback Gemini 4 Hydrogen flagship model.
	DefaultHydrogenModel = "gemini-4-hydrogen"
	// DefaultGeminiEffort is the default reasoning effort for Gemini models.
	DefaultGeminiEffort = "high"
	// ModelCacheTTL is the duration after which cached models are considered stale and refreshed.
	ModelCacheTTL = 12 * time.Hour
	// ModelCacheSchemaVersion tracks schema updates so older caches auto-refresh model efforts and Gemini 4 discovery.
	ModelCacheSchemaVersion = 2
)

var effortOrder = []string{"low", "medium", "high", "xhigh", "max"}

var validEffortLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"xhigh":  true,
	"max":    true,
}

// IsValidEffort reports whether effort is a recognized reasoning effort level.
func IsValidEffort(effort string) bool {
	return validEffortLevels[strings.ToLower(strings.TrimSpace(effort))]
}

var trailingParenRegex = regexp.MustCompile(`\s*\(([^()]+)\)\s*$`)

// SplitModelAndEffort splits a display name or API model ID into its canonical normalized base model ID
// and an optional embedded reasoning effort (e.g. "Gemini 4 Hydrogen (XHigh)" -> ("gemini-4-hydrogen", "xhigh"),
// "gemini-4-hydrogen-xhigh" -> ("gemini-4-hydrogen", "xhigh"), "Claude Opus 4.6 (Thinking)" -> ("claude-opus-4-6-thinking", "")).
func SplitModelAndEffort(modelOrDisplay string) (string, string) {
	name := strings.TrimSpace(modelOrDisplay)
	if name == "" {
		return "", ""
	}

	lowerRaw := strings.ToLower(name)
	if strings.Contains(lowerRaw, "claude") {
		if strings.Contains(lowerRaw, "opus") && (strings.Contains(lowerRaw, "4.6") || strings.Contains(lowerRaw, "4-6")) {
			return "claude-opus-4-6-thinking", ""
		}
		if strings.Contains(lowerRaw, "sonnet") && (strings.Contains(lowerRaw, "4.6") || strings.Contains(lowerRaw, "4-6")) {
			return "claude-sonnet-4-6", ""
		}
	}
	if strings.Contains(lowerRaw, "gpt-oss") || strings.Contains(lowerRaw, "gpt oss") {
		var eff string
		if m := trailingParenRegex.FindStringSubmatch(name); len(m) == 2 && IsValidEffort(m[1]) {
			eff = strings.ToLower(strings.TrimSpace(m[1]))
		} else {
			for _, suffix := range effortOrder {
				if strings.HasSuffix(lowerRaw, "-"+suffix) {
					eff = suffix
					break
				}
			}
		}
		return "gpt-oss-120b", eff
	}

	var effort string
	if m := trailingParenRegex.FindStringSubmatch(name); len(m) == 2 {
		qual := strings.ToLower(strings.TrimSpace(m[1]))
		if IsValidEffort(qual) {
			effort = qual
			name = strings.TrimSpace(name[:len(name)-len(m[0])])
		} else if qual == "thinking" || qual == "preview" || qual == "experimental" || qual == "beta" {
			name = strings.TrimSpace(name[:len(name)-len(m[0])])
		}
	}

	lower := strings.ToLower(name)
	if !strings.Contains(lower, " ") {
		if effort == "" {
			for _, suffix := range []string{"-xhigh", "-high", "-medium", "-low", "-max"} {
				if strings.HasSuffix(lower, suffix) {
					effort = strings.TrimPrefix(suffix, "-")
					lower = strings.TrimSuffix(lower, suffix)
					break
				}
			}
		}
		return lower, effort
	}

	// Handle display names with non-effort parentheticals like "Orchestrator (Neon)"
	cleanForFields := strings.NewReplacer("(", " ", ")", " ").Replace(lower)
	parts := strings.Fields(cleanForFields)
	result := strings.Join(parts, "-")

	// Heuristic: trim trailing minor version ".0"/".Y" on major >= 3 (e.g. "4.0" -> "4"), keeping "2.5" intact.
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if dotIdx := strings.Index(last, "."); dotIdx > 0 {
			major := last[:dotIdx]
			if len(major) > 0 && major[0] >= '3' && major[0] <= '9' {
				result = strings.Join(parts[:len(parts)-1], "-") + "-" + major
			}
		}
	}

	return result, effort
}

// FormatModelWithEffort builds a canonical variant model ID when effort is supported and non-empty
// (e.g. ("gemini-4-hydrogen", "xhigh") -> "gemini-4-hydrogen-xhigh").
func FormatModelWithEffort(model, effort string) string {
	base, embedded := SplitModelAndEffort(model)
	if base == "" {
		return ""
	}
	eff := strings.ToLower(strings.TrimSpace(effort))
	if eff == "" {
		eff = embedded
	}
	if eff != "" && ModelSupportsEffort(base) && IsValidEffort(eff) {
		return base + "-" + eff
	}
	return base
}

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

var geminiVersionRegex = regexp.MustCompile(`(?i)gemini-(\d+)(?:\.(\d+))?(?:\.(\d+))?-(flash|pro|hydrogen|carbon|neon|argon)`)

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
	SchemaVersion  int                 `json:"schema_version,omitempty"`
	FetchedAt      time.Time           `json:"fetched_at"`
	LatestFlash    string              `json:"latest_flash"`
	LatestPro      string              `json:"latest_pro"`
	LatestHydrogen string              `json:"latest_hydrogen,omitempty"`
	AllModels      []string            `json:"all_models"`
	ModelEfforts   map[string][]string `json:"model_efforts,omitempty"`
}

var (
	modelCacheLock   sync.RWMutex
	cachedModels     *DiscoveredModels
	cachedModelsPath string
	discoveryMu      sync.Mutex
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

func sortEffortsCanonical(set map[string]bool) []string {
	var ordered []string
	for _, eff := range effortOrder {
		if set[eff] {
			ordered = append(ordered, eff)
		}
	}
	return ordered
}

func flagshipRank(model string) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "hydrogen"):
		return 4
	case strings.Contains(m, "carbon"):
		return 3
	case strings.Contains(m, "neon"):
		return 2
	case strings.Contains(m, "argon"):
		return 1
	default:
		return 0
	}
}

// ParseAgyModelsOutput extracts base model IDs, supported effort levels, and highest Flash / Pro / Hydrogen models.
func ParseAgyModelsOutput(output string) *DiscoveredModels {
	res := &DiscoveredModels{
		SchemaVersion: ModelCacheSchemaVersion,
		FetchedAt:     time.Now(),
		AllModels:     []string{},
		ModelEfforts:  make(map[string][]string),
	}
	seen := make(map[string]bool)
	effortsSet := make(map[string]map[string]bool)

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

		// Strip -max, -xhigh, -high, -medium, -low suffixes for base model ID while recording supported efforts
		baseModel := modelID
		var effort string
		lowerID := strings.ToLower(modelID)
		for _, suffix := range []string{"-max", "-xhigh", "-high", "-medium", "-low"} {
			if strings.HasSuffix(lowerID, suffix) {
				baseModel = modelID[:len(modelID)-len(suffix)]
				effort = strings.TrimPrefix(suffix, "-")
				break
			}
		}

		if effort != "" {
			if effortsSet[baseModel] == nil {
				effortsSet[baseModel] = make(map[string]bool)
			}
			effortsSet[baseModel][effort] = true
		}

		if !seen[baseModel] {
			seen[baseModel] = true
			res.AllModels = append(res.AllModels, baseModel)
		}
	}

	for base, set := range effortsSet {
		if ordered := sortEffortsCanonical(set); len(ordered) > 0 {
			res.ModelEfforts[base] = ordered
		}
	}

	// Numerically evaluate highest version for Flash, Pro, and Gemini 4+ Flagship (Hydrogen/Carbon/Neon/Argon) series
	var highestFlashVer GeminiModelVersion
	var highestProVer GeminiModelVersion
	var highestHydrogenVer GeminiModelVersion
	var highestHydrogenRank int

	for _, m := range res.AllModels {
		v, ok := ExtractGeminiVersion(m)
		if !ok {
			continue
		}
		lower := strings.ToLower(m)
		if strings.Contains(lower, "flash") {
			isLite := strings.Contains(lower, "lite")
			if res.LatestFlash == "" || v.GreaterThan(highestFlashVer) {
				res.LatestFlash = m
				highestFlashVer = v
			} else if v == highestFlashVer && !isLite && strings.Contains(strings.ToLower(res.LatestFlash), "lite") {
				// Prefer full Flash over Flash Lite if version matches
				res.LatestFlash = m
			}
		} else if strings.Contains(lower, "pro") {
			if res.LatestPro == "" || v.GreaterThan(highestProVer) {
				res.LatestPro = m
				highestProVer = v
			}
		} else if rank := flagshipRank(lower); rank > 0 {
			if res.LatestHydrogen == "" || v.GreaterThan(highestHydrogenVer) || (v == highestHydrogenVer && rank > highestHydrogenRank) {
				res.LatestHydrogen = m
				highestHydrogenVer = v
				highestHydrogenRank = rank
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
	cachePath := GetModelCachePath()

	modelCacheLock.RLock()
	if cachedModels != nil && cachedModelsPath == cachePath && time.Since(cachedModels.FetchedAt) < ModelCacheTTL {
		m := cachedModels
		modelCacheLock.RUnlock()
		return m
	}
	modelCacheLock.RUnlock()

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
	cachedModelsPath = cachePath
	modelCacheLock.Unlock()

	return &dm
}

// SaveCachedDiscoveredModels saves the discovered models to disk.
func SaveCachedDiscoveredModels(dm *DiscoveredModels) error {
	if dm == nil {
		return nil
	}
	if dm.SchemaVersion == 0 {
		dm.SchemaVersion = ModelCacheSchemaVersion
	}
	if dm.ModelEfforts == nil {
		dm.ModelEfforts = make(map[string][]string)
	}
	cachePath := GetModelCachePath()
	modelCacheLock.Lock()
	cachedModels = dm
	cachedModelsPath = cachePath
	modelCacheLock.Unlock()

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

	agyPath := GetAgyPath()
	cmd := exec.CommandContext(ctx, agyPath, "models")
	cmd.Env = SanitizeAgyEnv(os.Environ(), map[string]string{"AGYS_INTERNAL_EXEC": "1"})
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
	agyPath := GetAgyPath()
	if agyPath == "agy" {
		var err error
		agyPath, err = exec.LookPath("agy")
		if err != nil {
			return false
		}
	}
	fi, err := os.Stat(agyPath)
	if err != nil {
		return false
	}
	return fi.ModTime().After(t)
}

var (
	isHookProcessLock sync.RWMutex
	isHookProcess     bool
)

// SetHookProcess marks the current process as a short-lived hook process (e.g. statusline or herdr hook)
// where background goroutines should not be spawned because the process terminates immediately.
func SetHookProcess(val bool) {
	isHookProcessLock.Lock()
	defer isHookProcessLock.Unlock()
	isHookProcess = val
}

// IsHookProcess reports whether the current process is running as a short-lived hook.
func IsHookProcess() bool {
	if os.Getenv("AGYS_HOOK_PROCESS") == "1" {
		return true
	}
	isHookProcessLock.RLock()
	defer isHookProcessLock.RUnlock()
	return isHookProcess
}

// GetOrRefreshModels retrieves the latest model metadata, auto-refreshing if stale, schema-outdated, or agy was updated.
func GetOrRefreshModels() *DiscoveredModels {
	cached := ReadCachedDiscoveredModels()

	// Upgrade legacy caches synchronously outside short-lived hooks so `agys models` / `agys doctor`
	// immediately populate Gemini 4 models and ModelEfforts without getting cut off on process exit.
	if cached != nil && cached.SchemaVersion < ModelCacheSchemaVersion {
		if !IsHookProcess() {
			if fresh, err := DiscoverLatestModels(); err == nil && fresh != nil {
				return fresh
			}
		}
		return cached
	}

	// Check if agy binary was updated after cache was generated
	if cached != nil && IsAgyBinaryNewerThan(cached.FetchedAt) {
		if !IsHookProcess() {
			go func() {
				_, _ = DiscoverLatestModels()
			}()
		}
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
			if !IsHookProcess() {
				go func() {
					_, _ = DiscoverLatestModels()
				}()
			}
			return &dm
		}
	}

	// Cold start with zero cache: discover synchronously
	if dm, err := DiscoverLatestModels(); err == nil && dm != nil {
		return dm
	}

	return &DiscoveredModels{
		SchemaVersion: ModelCacheSchemaVersion,
		FetchedAt:     time.Now(),
		LatestFlash:   DefaultGeminiModel,
		AllModels:     []string{DefaultGeminiModel},
		ModelEfforts:  map[string][]string{},
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

// GetLatestHydrogenModel returns the highest discovered Gemini 4+ flagship model (Hydrogen/Carbon/Neon/Argon).
func GetLatestHydrogenModel() string {
	dm := GetOrRefreshModels()
	if dm != nil && dm.LatestHydrogen != "" {
		return dm.LatestHydrogen
	}
	return DefaultHydrogenModel
}

// GetLatestProOrHydrogenModel returns the highest discovered Pro or Gemini 4+ Hydrogen flagship model.
func GetLatestProOrHydrogenModel() string {
	dm := GetOrRefreshModels()
	if dm != nil {
		if dm.LatestHydrogen != "" {
			return dm.LatestHydrogen
		}
		if dm.LatestPro != "" {
			return dm.LatestPro
		}
	}
	return DefaultHydrogenModel
}

// ResolveModelAlias resolves friendly model aliases ("auto", "latest", "flash", "hydrogen", "pro")
// into concrete discovered model IDs, returning empty string if input is not a known alias.
func ResolveModelAlias(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "auto", "latest", "flash":
		return GetLatestGeminiModel()
	case "hydrogen":
		return GetLatestHydrogenModel()
	case "pro":
		return GetLatestProOrHydrogenModel()
	default:
		return ""
	}
}

// IsClaudeModel reports whether modelName refers to an Anthropic Claude model.
func IsClaudeModel(modelName string) bool {
	m := strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(m, "claude") || strings.Contains(m, "sonnet") ||
		strings.Contains(m, "opus") || strings.Contains(m, "haiku") || strings.Contains(m, "anthropic")
}

// IsOpenAIModel reports whether modelName refers to an OpenAI/GPT model.
func IsOpenAIModel(modelName string) bool {
	m := strings.ToLower(strings.TrimSpace(modelName))
	if strings.Contains(m, "gpt") || strings.Contains(m, "openai") {
		return true
	}
	tokens := strings.FieldsFunc(m, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' ' || r == '/'
	})
	for _, t := range tokens {
		if t == "o1" || t == "o3" || t == "o4" {
			return true
		}
	}
	return false
}

// Is3PModel reports whether modelName belongs to the third-party (Claude / GPT) quota pool.
func Is3PModel(modelName string) bool {
	m := strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(m, "3p") || IsClaudeModel(m) || IsOpenAIModel(m)
}

// IsGeminiModel reports whether modelName belongs to Google Gemini (including Gemini 4 Hydrogen/Carbon/Neon/Argon).
func IsGeminiModel(modelName string) bool {
	m := strings.ToLower(strings.TrimSpace(modelName))
	if m == "" {
		return false
	}
	if strings.Contains(m, "gemini") {
		return true
	}
	tokens := strings.FieldsFunc(m, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' ' || r == '/'
	})
	for _, t := range tokens {
		switch t {
		case "flash", "hydrogen", "carbon", "neon", "argon":
			return true
		}
	}
	return false
}

// IsOrchestratorModel reports whether modelName identifies an Orchestrator multi-role model
// (e.g. "Orchestrator", "Orchestrator (Carbon)", "orchestrator-neon").
func IsOrchestratorModel(modelName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "orchestrator")
}

// ModelSupportsEffort returns true if the specified model supports the `--effort` reasoning flag
// (e.g. Gemini 3.x Flash/Pro, Gemini 4.x Hydrogen/Carbon/Neon/Argon).
func ModelSupportsEffort(model string) bool {
	base, _ := SplitModelAndEffort(model)
	m := strings.ToLower(strings.TrimSpace(base))
	if m == "" {
		return true
	}
	if IsClaudeModel(m) || IsOpenAIModel(m) || strings.Contains(m, "deepseek") || strings.Contains(m, "qwen") || IsOrchestratorModel(m) {
		return false
	}
	if strings.Contains(m, "flash") || strings.Contains(m, "hydrogen") || strings.Contains(m, "carbon") || strings.Contains(m, "neon") || strings.Contains(m, "argon") {
		return true
	}
	if v, ok := ExtractGeminiVersion(m); ok && v.Major >= 3 {
		return true
	}
	if dm := ReadCachedDiscoveredModels(); dm != nil && len(dm.ModelEfforts[m]) > 1 {
		return true
	}
	return false
}

// GetSupportedEfforts returns the ordered list of reasoning efforts supported by model.
// Uses cached `agy models` discovery first, falling back to static model family capabilities.
func GetSupportedEfforts(model string) []string {
	base, _ := SplitModelAndEffort(model)
	m := strings.ToLower(strings.TrimSpace(base))
	if !ModelSupportsEffort(m) {
		return nil
	}
	if dm := ReadCachedDiscoveredModels(); dm != nil && len(dm.ModelEfforts[m]) > 0 {
		out := make([]string, len(dm.ModelEfforts[m]))
		copy(out, dm.ModelEfforts[m])
		return out
	}
	if strings.Contains(m, "hydrogen") || strings.Contains(m, "neon") {
		return []string{"low", "medium", "high", "xhigh"}
	}
	return []string{"low", "medium", "high"}
}

// ModelSupportsEffortLevel reports whether model accepts the given reasoning effort level.
func ModelSupportsEffortLevel(model, effort string) bool {
	eff := strings.ToLower(strings.TrimSpace(effort))
	if eff == "" {
		return false
	}
	for _, supported := range GetSupportedEfforts(model) {
		if supported == eff {
			return true
		}
	}
	return false
}

// ClampEffortForModel ensures a resolved or cached effort is valid for model,
// downgrading unsupported higher efforts (e.g. cached "xhigh" when running "gemini-3.8-flash" -> "high").
func ClampEffortForModel(model, effort string) string {
	eff := strings.ToLower(strings.TrimSpace(effort))
	if eff == "" || !ModelSupportsEffort(model) {
		return ""
	}
	supported := GetSupportedEfforts(model)
	if len(supported) == 0 {
		return eff
	}
	for _, s := range supported {
		if s == eff {
			return eff
		}
	}
	rankOf := func(level string) int {
		for i, e := range effortOrder {
			if e == level {
				return i
			}
		}
		return -1
	}
	targetRank := rankOf(eff)
	if targetRank < 0 {
		for _, s := range supported {
			if s == DefaultGeminiEffort {
				return s
			}
		}
		return supported[len(supported)-1]
	}
	// Pick highest supported level <= requested rank
	best := ""
	bestRank := -1
	for _, s := range supported {
		r := rankOf(s)
		if r <= targetRank && r > bestRank {
			best = s
			bestRank = r
		}
	}
	if best != "" {
		return best
	}
	return supported[0]
}
