package profile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const (
	projectIDFilename               = "project_id"
	emailFilename                   = "email"
	quotaCacheFilename              = "quota_cache.json"
	oauthTokenURL                   = "https://oauth2.googleapis.com/token"
	minAntigravityQuotaClientVer    = "2.35.0"
)

var (
	antigravityUAOnce sync.Once
	antigravityUA     string
)

// compareSimpleVersions compares dotted numeric version strings (e.g. "2.35.0" vs "2.34.0").
// Returns 1 if a > b, -1 if a < b, 0 if equal.
func compareSimpleVersions(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(strings.TrimSpace(a), "v"), ".")
	pb := strings.Split(strings.TrimPrefix(strings.TrimSpace(b), "v"), ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var va, vb int
		if i < len(pa) {
			va, _ = strconv.Atoi(strings.SplitN(pa[i], "-", 2)[0])
		}
		if i < len(pb) {
			vb, _ = strconv.Atoi(strings.SplitN(pb[i], "-", 2)[0])
		}
		if va > vb {
			return 1
		}
		if va < vb {
			return -1
		}
	}
	return 0
}

// getAntigravityUserAgent returns an Antigravity client User-Agent >= 2.35.0 so that
// Cloud Code's retrieveUserQuotaSummary returns all quota groups including Gemini Models.
func getAntigravityUserAgent() string {
	antigravityUAOnce.Do(func() {
		ver := minAntigravityQuotaClientVer
		if runtime.GOOS == "darwin" {
			for _, plist := range []string{
				"/Applications/Antigravity.app/Contents/Info.plist",
				"/Applications/Antigravity IDE.app/Contents/Info.plist",
			} {
				if _, err := os.Stat(plist); err == nil {
					if out, err := exec.Command("defaults", "read", plist, "CFBundleShortVersionString").Output(); err == nil {
						installed := strings.TrimSpace(string(out))
						if installed != "" && compareSimpleVersions(installed, ver) > 0 {
							ver = installed
						}
					}
				}
			}
		}
		antigravityUA = fmt.Sprintf("antigravity/%s %s/%s", ver, runtime.GOOS, runtime.GOARCH)
	})
	return antigravityUA
}

// classifyQuotaKeyWindow classifies an identifier/label/window string into 5h or weekly quota window.
func classifyQuotaKeyWindow(fields ...string) (is5H bool, isWeekly bool) {
	for _, f := range fields {
		s := strings.ToLower(strings.TrimSpace(f))
		if s == "" {
			continue
		}
		if strings.Contains(s, "week") || strings.Contains(s, "7d") {
			isWeekly = true
		}
		if strings.Contains(s, "5h") || strings.Contains(s, "5-hour") || strings.Contains(s, "5_hour") ||
			strings.Contains(s, "five_hour") || strings.Contains(s, "five-hour") || strings.Contains(s, "five hour") {
			is5H = true
		}
	}
	if isWeekly {
		is5H = false
	}
	return is5H, isWeekly
}

// classifyQuotaGroup determines whether a QuotaGroup tracks 3P (Claude/GPT) or Gemini quota.
func classifyQuotaGroup(group *QuotaGroup) (isGroup3P bool, isGroupGemini bool) {
	if group == nil {
		return false, false
	}
	gName := strings.ToLower(strings.TrimSpace(group.DisplayName))
	gDesc := strings.ToLower(strings.TrimSpace(group.Description))
	isGroup3P = Is3PModel(gName) || Is3PModel(gDesc) || strings.Contains(gName, "3p") || strings.Contains(gDesc, "3p")
	isGroupGemini = !isGroup3P && (IsGeminiModel(gName) || IsGeminiModel(gDesc))
	return isGroup3P, isGroupGemini
}

// CachedProfileQuota stores cached user quota summary with a timestamp.
type CachedProfileQuota struct {
	Summary   *QuotaSummary `json:"summary"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// SaveCachedQuota saves the quota summary for a profile to local disk cache.
func SaveCachedQuota(profileName string, summary *QuotaSummary) error {
	if profileName == "" || summary == nil {
		return nil
	}
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return err
	}
	cached := CachedProfileQuota{
		Summary:   summary,
		UpdatedAt: time.Now(),
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	return WriteFileAtomic(filepath.Join(profileDir, quotaCacheFilename), data, 0600)
}

// UpdateCachedQuotaFractions updates the cached quota summary with newly parsed real-time fractions.
func UpdateCachedQuotaFractions(profileName, modelName string, details *ModelQuotaDetails) error {
	if profileName == "" || details == nil {
		return nil
	}
	summary, ok := GetCachedQuota(profileName, 0)
	if !ok || summary == nil {
		summary = &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini Models",
					Buckets: []QuotaBucket{
						{BucketID: "gemini-5h", Window: "5h"},
						{BucketID: "gemini-weekly", Window: "weekly"},
					},
				},
				{
					DisplayName: "Claude and GPT models",
					Buckets: []QuotaBucket{
						{BucketID: "3p-5h", Window: "5h"},
						{BucketID: "3p-weekly", Window: "weekly"},
					},
				},
			},
		}
	}

	is3P := Is3PModel(modelName)
	targetIdx := -1
	for i := range summary.Groups {
		isGroup3P, _ := classifyQuotaGroup(&summary.Groups[i])
		if is3P == isGroup3P {
			targetIdx = i
			if details.GroupName != "" && strings.EqualFold(summary.Groups[i].DisplayName, details.GroupName) {
				break
			}
		}
	}

	if targetIdx == -1 {
		dispName := "Gemini Models"
		if is3P {
			dispName = "Claude and GPT models"
		}
		summary.Groups = append(summary.Groups, QuotaGroup{
			DisplayName: dispName,
		})
		targetIdx = len(summary.Groups) - 1
	}

	group := &summary.Groups[targetIdx]
	updated5H, updatedWeekly := false, false
	for j := range group.Buckets {
		b := &group.Buckets[j]
		is5H, isWeekly := classifyQuotaKeyWindow(b.Window, b.DisplayName, b.BucketID)
		if is5H && details.Fraction5H >= 0 {
			b.RemainingFraction = details.Fraction5H
			if !details.ResetTime5H.IsZero() {
				b.ResetTime = details.ResetTime5H
			}
			updated5H = true
		}
		if isWeekly && details.FractionWeekly >= 0 {
			b.RemainingFraction = details.FractionWeekly
			if !details.ResetTimeWeekly.IsZero() {
				b.ResetTime = details.ResetTimeWeekly
			}
			updatedWeekly = true
		}
	}
	if !updated5H && details.Fraction5H >= 0 {
		prefix := "gemini"
		if is3P {
			prefix = "3p"
		}
		group.Buckets = append(group.Buckets, QuotaBucket{
			BucketID:          prefix + "-5h",
			Window:            "5h",
			RemainingFraction: details.Fraction5H,
			ResetTime:         details.ResetTime5H,
		})
	}
	if !updatedWeekly && details.FractionWeekly >= 0 {
		prefix := "gemini"
		if is3P {
			prefix = "3p"
		}
		group.Buckets = append(group.Buckets, QuotaBucket{
			BucketID:          prefix + "-weekly",
			Window:            "weekly",
			RemainingFraction: details.FractionWeekly,
			ResetTime:         details.ResetTimeWeekly,
		})
	}

	return SaveCachedQuota(profileName, summary)
}

// GetCachedQuota returns the cached quota summary if available. If maxAge > 0 and the cache is older than maxAge,
// it returns (nil, false). If maxAge <= 0, it returns the summary regardless of age.
func GetCachedQuota(profileName string, maxAge time.Duration) (*QuotaSummary, bool) {
	if profileName == "" {
		return nil, false
	}
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(profileDir, quotaCacheFilename))
	if err != nil {
		return nil, false
	}
	var cached CachedProfileQuota
	if err := json.Unmarshal(data, &cached); err != nil || cached.Summary == nil {
		return nil, false
	}
	if maxAge > 0 && time.Since(cached.UpdatedAt) > maxAge {
		return nil, false
	}
	return cached.Summary, true
}

func calculateTokenFingerprint(token *OAuthToken) string {
	if token == nil || token.Token.AccessToken == "" {
		return ""
	}
	h := sha256.Sum256([]byte(token.Token.RefreshToken + ":" + token.Token.AccessToken))
	return fmt.Sprintf("%x", h[:16])
}

// GetCachedEmail reads the cached Google account email for a given profile, verifying it matches the current token.
func GetCachedEmail(profileName string) (string, error) {
	token, err := ReadToken(profileName)
	if err != nil {
		// Token doesn't exist or is unreadable -> invalidate cached email if present
		if profileDir, dirErr := GetProfileDir(profileName); dirErr == nil {
			_ = os.Remove(filepath.Join(profileDir, emailFilename))
		}
		return "", err
	}

	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return "", err
	}

	cachePath := filepath.Join(profileDir, emailFilename)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("empty email cache file")
	}

	email := strings.TrimSpace(lines[0])
	if email == "" {
		return "", fmt.Errorf("empty email in cache file")
	}

	currentFP := calculateTokenFingerprint(token)
	if len(lines) < 2 {
		// Legacy single-line email cache file (no fingerprint) -> force re-verification
		return "", fmt.Errorf("legacy email cache file without token fingerprint")
	}

	cachedFP := strings.TrimSpace(lines[1])
	if currentFP == "" || cachedFP != currentFP {
		// Fingerprint mismatch -> token changed or cache stale
		return "", fmt.Errorf("token fingerprint mismatch (cached: %s, current: %s)", cachedFP, currentFP)
	}

	return email, nil
}

// SaveCachedEmail saves the cached Google account email for a given profile with token fingerprint.
func SaveCachedEmail(profileName string, email string) error {
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return err
	}

	token, _ := ReadToken(profileName)
	fp := calculateTokenFingerprint(token)

	cachePath := filepath.Join(profileDir, emailFilename)
	content := strings.TrimSpace(email) + "\n" + fp + "\n"
	return WriteFileAtomic(cachePath, []byte(content), 0600)
}

// FetchProfileEmail retrieves the Google account email for a profile via userinfo endpoint.
func FetchProfileEmail(ctx context.Context, profileName string) (string, error) {
	cached, err := GetCachedEmail(profileName)
	if err == nil && cached != "" {
		return cached, nil
	}

	token, err := ReadToken(profileName)
	if err != nil {
		return "", err
	}
	if IsTokenExpired(token) {
		if refreshErr := RefreshToken(ctx, profileName); refreshErr == nil {
			if refreshedToken, readErr := ReadToken(profileName); readErr == nil && refreshedToken.Token.AccessToken != "" {
				token = refreshedToken
			}
		}
	}
	if token.Token.AccessToken == "" {
		return "", fmt.Errorf("access token is empty")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token.Token.AccessToken)
	req.Header.Set("User-Agent", getAntigravityUserAgent())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", formatHTTPError(resp.StatusCode, body)
	}

	var uinfo struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uinfo); err != nil {
		return "", err
	}

	if uinfo.Email != "" {
		_ = SaveCachedEmail(profileName, uinfo.Email)
	}

	return uinfo.Email, nil
}

var (
	cidBytes = []byte{107, 106, 109, 107, 106, 106, 108, 106, 108, 106, 111, 99, 107, 119, 46, 55, 50, 41, 41, 51, 52, 104, 50, 104, 107, 54, 57, 40, 63, 104, 105, 111, 44, 46, 53, 54, 53, 48, 50, 110, 61, 110, 106, 105, 63, 42, 116, 59, 42, 42, 41, 116, 61, 53, 53, 61, 54, 63, 47, 41, 63, 40, 57, 53, 52, 46, 63, 52, 46, 116, 57, 53, 55}
	secBytes = []byte{29, 21, 25, 9, 10, 2, 119, 17, 111, 98, 28, 13, 8, 110, 98, 108, 22, 62, 22, 16, 107, 55, 22, 24, 98, 41, 2, 25, 110, 32, 108, 43, 30, 27, 60}
)

func getOAuthCredentials() (string, string) {
	cid := make([]byte, len(cidBytes))
	sec := make([]byte, len(secBytes))
	for i, b := range cidBytes {
		cid[i] = b ^ 0x5A
	}
	for i, b := range secBytes {
		sec[i] = b ^ 0x5A
	}
	return string(cid), string(sec)
}

// ErrUnauthenticated indicates that the session has expired or credentials are invalid.
var ErrUnauthenticated = errors.New("session expired or invalid credentials (re-login required: agys switch <profile>)")

// OAuthToken represents the structure of antigravity-oauth-token file.
type OAuthToken struct {
	Token struct {
		AccessToken  string    `json:"access_token"`
		TokenType    string    `json:"token_type"`
		RefreshToken string    `json:"refresh_token"`
		Expiry       time.Time `json:"expiry"`
	} `json:"token"`
	AuthMethod string `json:"auth_method"`
}

// QuotaBucket represents a single quota limit (5-hour or weekly).
type QuotaBucket struct {
	BucketID          string    `json:"bucketId"`
	DisplayName       string    `json:"displayName"`
	Window            string    `json:"window"`
	ResetTime         time.Time `json:"resetTime"`
	Description       string    `json:"description"`
	RemainingFraction float64   `json:"remainingFraction"`
}

// QuotaGroup represents a group of models sharing the same quota limits.
type QuotaGroup struct {
	DisplayName string        `json:"displayName"`
	Description string        `json:"description"`
	Buckets     []QuotaBucket `json:"buckets"`
}

// QuotaSummary represents the response from retrieveUserQuotaSummary endpoint.
type QuotaSummary struct {
	Groups      []QuotaGroup `json:"groups"`
	Description string       `json:"description"`
}

// ProfileQuotaInfo represents the collected quota data for a specific profile.
type ProfileQuotaInfo struct {
	ProfileName   string        `json:"profileName"`
	Email         string        `json:"email,omitempty"`
	Active        bool          `json:"active"`
	Error         string        `json:"error,omitempty"`
	Quota         *QuotaSummary `json:"quota,omitempty"`
	CLIConfigured bool          `json:"cliConfigured"`
	IDEConfigured bool          `json:"ideConfigured"`
	Configured    string        `json:"configured,omitempty"`
}

// ReadToken reads the oauth token for a given profile.
func ReadToken(profileName string) (*OAuthToken, error) {
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return nil, err
	}
	return ReadTokenFromDir(profileDir)
}

// IsTokenExpired checks if the token is expired or will expire in less than 2 minutes.
func IsTokenExpired(token *OAuthToken) bool {
	if token.Token.AccessToken == "" {
		return true
	}
	return time.Now().Add(2 * time.Minute).After(token.Token.Expiry)
}

// refreshOAuthTokenDirect refreshes the OAuth access token using Google's OAuth endpoint and saves the new token to disk.
func refreshOAuthTokenDirect(ctx context.Context, profileName string) error {
	token, err := ReadToken(profileName)
	if err != nil {
		return err
	}

	refreshToken := token.Token.RefreshToken
	if refreshToken == "" {
		return fmt.Errorf("refresh_token is empty")
	}

	clientID, clientSecret := getOAuthCredentials()

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, "POST", oauthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("OAuth token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(body), "invalid_grant") {
			return fmt.Errorf("%w (%s)", ErrUnauthenticated, string(body))
		}
		return fmt.Errorf("OAuth token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var res struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("failed to parse OAuth token response: %w", err)
	}

	if res.AccessToken == "" {
		return fmt.Errorf("received empty access_token from OAuth endpoint")
	}

	// Update token fields
	token.Token.AccessToken = res.AccessToken
	if res.ExpiresIn > 0 {
		token.Token.Expiry = time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
	} else {
		token.Token.Expiry = time.Now().Add(1 * time.Hour)
	}
	if res.RefreshToken != "" {
		token.Token.RefreshToken = res.RefreshToken
	}
	if res.TokenType != "" {
		token.Token.TokenType = res.TokenType
	}

	// Save updated token to file
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return err
	}
	updatedData, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated token: %w", err)
	}

	return WriteTokenToProfile(profileDir, string(updatedData))
}

// RefreshToken refreshes the OAuth access token for a given profile using Google's OAuth endpoint.
func RefreshToken(ctx context.Context, profileName string) error {
	return refreshOAuthTokenDirect(ctx, profileName)
}

// GetCachedProjectID reads the cached project ID for a given profile, if it exists.
func GetCachedProjectID(profileName string) (string, error) {
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return "", err
	}
	cachePath := filepath.Join(profileDir, projectIDFilename)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// SaveCachedProjectID saves the project ID for a given profile.
func SaveCachedProjectID(profileName string, projectID string) error {
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return err
	}
	cachePath := filepath.Join(profileDir, projectIDFilename)
	return os.WriteFile(cachePath, []byte(strings.TrimSpace(projectID)+"\n"), 0600)
}

// isUnauthenticatedError checks if an error indicates a 401 or unauthenticated response.
func isUnauthenticatedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnauthenticated) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "401") || strings.Contains(strings.ToUpper(msg), "UNAUTHENTICATED")
}

// formatHTTPError formats HTTP response errors cleanly.
func formatHTTPError(statusCode int, body []byte) error {
	if statusCode == http.StatusUnauthorized {
		var errResp struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error.Status != "" {
			return fmt.Errorf("HTTP status 401 (%s): %w", errResp.Error.Status, ErrUnauthenticated)
		}
		return fmt.Errorf("HTTP status 401: %w", ErrUnauthenticated)
	}
	return fmt.Errorf("HTTP status %d: %s", statusCode, string(body))
}

// FetchQuota retrieves the quota summary for a specific profile.
// It returns fresh cached quota (< 45 seconds old) immediately, or fetches from API and caches the result.
func FetchQuota(ctx context.Context, profileName string) (*QuotaSummary, error) {
	// 0. Fast-path: Check fresh cache (TTL 45s) for instant 0ms latency
	if cached, fresh := GetCachedQuota(profileName, 45*time.Second); fresh && cached != nil {
		return cached, nil
	}

	// 1. Read token
	token, err := ReadToken(profileName)
	if err != nil {
		// If read token failed, fallback to stale cached quota if available (< 4 hours)
		if stale, ok := GetCachedQuota(profileName, 4*time.Hour); ok && stale != nil {
			return stale, nil
		}
		return nil, err
	}

	// Pre-flight check: If token is already marked expired, attempt refresh first
	if IsTokenExpired(token) {
		if refreshErr := RefreshToken(ctx, profileName); refreshErr == nil {
			if refreshedToken, readErr := ReadToken(profileName); readErr == nil && refreshedToken.Token.AccessToken != "" {
				token = refreshedToken
			}
		}
	}

	accessToken := token.Token.AccessToken
	if accessToken == "" {
		if stale, ok := GetCachedQuota(profileName, 4*time.Hour); ok && stale != nil {
			return stale, nil
		}
		return nil, fmt.Errorf("access token is empty (not logged in)")
	}

	// 2. First attempt: try using existing access_token directly
	projectID, err := GetCachedProjectID(profileName)
	if err != nil || projectID == "" {
		projectID, err = loadCodeAssist(ctx, accessToken)
		if err == nil && projectID != "" {
			_ = SaveCachedProjectID(profileName, projectID)
		}
	}

	var summary *QuotaSummary
	if err == nil && projectID != "" {
		summary, err = retrieveUserQuotaSummary(ctx, accessToken, projectID)
	}

	// 3. If direct fetch failed with unauthenticated error or token expired, attempt refresh once
	if err != nil && (isUnauthenticatedError(err) || IsTokenExpired(token)) {
		if refreshErr := RefreshToken(ctx, profileName); refreshErr == nil {
			if refreshedToken, readErr := ReadToken(profileName); readErr == nil && refreshedToken.Token.AccessToken != "" {
				accessToken = refreshedToken.Token.AccessToken
				// Invalidate potentially stale cached project ID & re-fetch
				newProjectID, loadErr := loadCodeAssist(ctx, accessToken)
				if loadErr == nil && newProjectID != "" {
					_ = SaveCachedProjectID(profileName, newProjectID)
					summary, err = retrieveUserQuotaSummary(ctx, accessToken, newProjectID)
				} else if loadErr != nil {
					err = loadErr
				}
			}
		}
	}

	if err == nil && summary != nil {
		_ = SaveCachedQuota(profileName, summary)
		return summary, nil
	}

	// 4. Fallback to stale cached quota if API network request failed (< 4 hours)
	if stale, ok := GetCachedQuota(profileName, 4*time.Hour); ok && stale != nil {
		return stale, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user quota: %w", err)
	}

	return summary, nil
}

// ModelQuotaDetails contains 5H and Weekly quota metrics for a specific model group.
type ModelQuotaDetails struct {
	Fraction5H         float64
	ResetStr5H         string
	CompactReset5H     string
	ResetTime5H        time.Time
	FractionWeekly     float64
	ResetStrWeekly     string
	CompactResetWeekly string
	ResetTimeWeekly    time.Time
	GroupName          string
}

// ExtractModelQuotaDetails extracts 5H and Weekly quota metrics from a QuotaSummary for a given model.
func ExtractModelQuotaDetails(summary *QuotaSummary, modelName string) *ModelQuotaDetails {
	if summary == nil {
		return nil
	}

	mBase, _ := SplitModelAndEffort(modelName)
	mLower := strings.ToLower(strings.TrimSpace(mBase))
	if mLower == "" {
		mLower = strings.ToLower(strings.TrimSpace(modelName))
	}

	details := &ModelQuotaDetails{
		Fraction5H:     -1.0,
		FractionWeekly: -1.0,
	}

	is3P := Is3PModel(mLower)
	isExplicitGemini := !is3P && (IsGeminiModel(mLower) || IsOrchestratorModel(mLower))

	// 1. Dynamic pass: score groups by specificity within the requested model family
	// so dedicated model groups beat generic matches and 3P vs Gemini quotas never cross-bleed.
	var matchedGroup *QuotaGroup
	bestScore := 0
	ambiguous := map[string]bool{"pro": true, "max": true, "api": true, "all": true, "new": true, "old": true}
	for i := range summary.Groups {
		group := &summary.Groups[i]
		gName := strings.ToLower(strings.TrimSpace(group.DisplayName))
		gDesc := strings.ToLower(strings.TrimSpace(group.Description))
		groupIs3P, groupIsGemini := classifyQuotaGroup(group)

		score := 0
		if mLower != "" && mLower != "auto" {
			if is3P && !groupIs3P {
				continue
			}
			if isExplicitGemini && groupIs3P {
				continue
			}

			if is3P && groupIs3P {
				score = 60
			} else if isExplicitGemini {
				if groupIsGemini {
					score = 50
					if IsOrchestratorModel(mLower) && (strings.Contains(gName, "hydrogen") || strings.Contains(gDesc, "hydrogen")) {
						score = 60
					}
				} else if !groupIs3P {
					score = 20
				}
			}

			if strings.Contains(gDesc, mLower) || strings.Contains(gName, mLower) {
				if 100 > score {
					score = 100
				}
			} else {
				words := strings.FieldsFunc(mLower, func(r rune) bool {
					return r == '-' || r == '_' || r == '.' || r == ' ' || r == '/'
				})
				for _, w := range words {
					if len(w) >= 5 && !ambiguous[w] && (strings.Contains(gDesc, w) || strings.Contains(gName, w)) {
						tokenScore := 80
						if w == "gemini" {
							tokenScore = 50
						}
						if tokenScore > score {
							score = tokenScore
						}
					}
				}
			}
		} else {
			// If no model or "auto" specified, match default Gemini group
			if groupIsGemini {
				score = 50
			}
		}

		if score > bestScore {
			bestScore = score
			matchedGroup = group
		}
	}

	// 2. Extract 5H and Weekly from matched group
	if matchedGroup != nil {
		details.GroupName = matchedGroup.DisplayName
		for _, bucket := range matchedGroup.Buckets {
			is5H, isWeekly := classifyQuotaKeyWindow(bucket.Window, bucket.DisplayName, bucket.BucketID)

			if details.Fraction5H < 0 && is5H {
				details.Fraction5H = bucket.RemainingFraction
				details.ResetTime5H = bucket.ResetTime
				details.ResetStr5H = FormatResetTime(bucket.ResetTime, bucket.RemainingFraction)
			}
			if details.FractionWeekly < 0 && isWeekly {
				details.FractionWeekly = bucket.RemainingFraction
				details.ResetTimeWeekly = bucket.ResetTime
				details.ResetStrWeekly = FormatResetTime(bucket.ResetTime, bucket.RemainingFraction)
			}
		}
	}

	groupAllowed := func(group *QuotaGroup) bool {
		groupIs3P, _ := classifyQuotaGroup(group)
		if is3P {
			return groupIs3P
		}
		if isExplicitGemini {
			return !groupIs3P
		}
		return true
	}

	// 3. Family-scoped fallbacks if a window is missing in the primary matched group
	fillMissingFromGroups := func(onlyFamily bool) {
		for i := range summary.Groups {
			group := &summary.Groups[i]
			if onlyFamily && !groupAllowed(group) {
				continue
			}
			for _, bucket := range group.Buckets {
				is5H, isWeekly := classifyQuotaKeyWindow(bucket.Window, bucket.DisplayName, bucket.BucketID)
				if details.Fraction5H < 0 && is5H {
					details.Fraction5H = bucket.RemainingFraction
					details.ResetTime5H = bucket.ResetTime
					details.ResetStr5H = FormatResetTime(bucket.ResetTime, bucket.RemainingFraction)
					if details.GroupName == "" {
						details.GroupName = group.DisplayName
					}
				}
				if details.FractionWeekly < 0 && isWeekly {
					details.FractionWeekly = bucket.RemainingFraction
					details.ResetTimeWeekly = bucket.ResetTime
					details.ResetStrWeekly = FormatResetTime(bucket.ResetTime, bucket.RemainingFraction)
					if details.GroupName == "" {
						details.GroupName = group.DisplayName
					}
				}
			}
		}
	}

	if details.Fraction5H < 0 || details.FractionWeekly < 0 {
		fillMissingFromGroups(true)
	}
	// Only fall back across families when caller gave no explicit/auto model family
	if details.Fraction5H < 0 && details.FractionWeekly < 0 && !is3P && !isExplicitGemini {
		fillMissingFromGroups(false)
	}
	// When an account reports full 100% weekly quota without a separate 5h bucket yet
	// (e.g. fresh Gemini profiles before first 5h consumption), reflect 100% 5H with no reset timer.
	if details.Fraction5H < 0 && details.FractionWeekly >= 1.0 {
		details.Fraction5H = 1.0
		details.ResetStr5H = "-"
	}

	if details.Fraction5H < 0 && details.FractionWeekly < 0 {
		return nil
	}

	details.CompactReset5H = FormatCompactResetTime(details.ResetTime5H, details.Fraction5H)
	details.CompactResetWeekly = FormatCompactResetTime(details.ResetTimeWeekly, details.FractionWeekly)

	return details
}

// GetProfileFullQuotaDetailsForModel returns both 5H and Weekly quota details matching the specified model name.
func GetProfileFullQuotaDetailsForModel(ctx context.Context, profileName, modelName string) (*ModelQuotaDetails, error) {
	summary, err := FetchQuota(ctx, profileName)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, fmt.Errorf("no quota summary available")
	}

	details := ExtractModelQuotaDetails(summary, modelName)
	if details == nil || (details.Fraction5H < 0 && details.FractionWeekly < 0) {
		return nil, fmt.Errorf("no quota bucket found in summary")
	}

	return details, nil
}

// GetProfileFullQuotaDetailsFast returns quota details from local cache immediately (0ms).
// If fresh cache (< 45s) is available, it returns it directly.
// If stale cache (< 4 hours) is available, it returns it as a fallback without any network calls.
// If no cache exists, it returns (nil, false).
func GetProfileFullQuotaDetailsFast(profileName, modelName string) (*ModelQuotaDetails, bool) {
	if profileName == "" {
		return nil, false
	}

	// 1. Fresh cache check (< 45s) -> instant 0ms
	if cached, fresh := GetCachedQuota(profileName, 45*time.Second); fresh && cached != nil {
		if details := ExtractModelQuotaDetails(cached, modelName); details != nil && (details.Fraction5H >= 0 || details.FractionWeekly >= 0) {
			return details, true
		}
	}

	// 2. Stale cache check (< 4 hours) -> instant 0ms fallback
	if stale, ok := GetCachedQuota(profileName, 4*time.Hour); ok && stale != nil {
		if details := ExtractModelQuotaDetails(stale, modelName); details != nil && (details.Fraction5H >= 0 || details.FractionWeekly >= 0) {
			return details, true
		}
	}

	return nil, false
}


func loadCodeAssist(ctx context.Context, accessToken string) (string, error) {
	url := "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"

	reqBody := map[string]interface{}{
		"metadata": map[string]string{
			"ideType": "ANTIGRAVITY",
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", getAntigravityUserAgent())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", formatHTTPError(resp.StatusCode, body)
	}

	var res struct {
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	if res.CloudaicompanionProject == "" {
		return "", fmt.Errorf("cloudaicompanionProject is empty in response")
	}

	return res.CloudaicompanionProject, nil
}

func retrieveUserQuotaSummary(ctx context.Context, accessToken, projectID string) (*QuotaSummary, error) {
	url := "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"

	reqBody := map[string]string{
		"project": projectID,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", getAntigravityUserAgent())

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, formatHTTPError(resp.StatusCode, body)
	}

	var summary QuotaSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, err
	}

	return &summary, nil
}

// FormatResetTime formats a reset timestamp into a human-readable remaining time string.
func FormatResetTime(resetTime time.Time, fraction float64) string {
	if fraction >= 1.0 || resetTime.IsZero() {
		return "-"
	}

	duration := time.Until(resetTime)
	if duration <= 0 {
		return "refreshing"
	}

	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("in %dd %dh", days, hours)
		}
		return fmt.Sprintf("in %dd", days)
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("in %dh %dm", hours, minutes)
		}
		return fmt.Sprintf("in %dh", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("in %dm", minutes)
	}
	return "in <1m"
}

// FormatCompactResetTime formats a reset timestamp into a concise, compact remaining time string (e.g. "2h15m", "3d", "45m").
func FormatCompactResetTime(resetTime time.Time, fraction float64) string {
	if fraction >= 1.0 || resetTime.IsZero() {
		return ""
	}

	duration := time.Until(resetTime)
	if duration <= 0 {
		return "due"
	}

	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh%02dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return "<1m"
}

// ProgressBar generates a text progress bar of given width representing fraction (0.0 to 1.0).
func ProgressBar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	} else if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction * float64(width))
	empty := width - filled

	var bar strings.Builder
	for i := 0; i < filled; i++ {
		bar.WriteString("█")
	}
	for i := 0; i < empty; i++ {
		bar.WriteString("░")
	}
	return bar.String()
}

// RenderQuotaTable renders a clean tabular view of profile quota information with remaining reset times.
func RenderQuotaTable(w io.Writer, results []ProfileQuotaInfo, currentProfile string, priorities map[string]int) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tPRIO\tEMAIL\tCONFIG\tMODEL GROUP\t5H QUOTA\tRESET (5H)\tWEEKLY QUOTA\tRESET (WEEKLY)")

	for _, res := range results {
		pName := res.ProfileName
		if pName == currentProfile {
			pName += " (default)"
		}

		prioStr := "-"
		if prio, ok := priorities[res.ProfileName]; ok {
			prioStr = strconv.Itoa(prio)
		}

		emailStr := res.Email
		if emailStr == "" {
			emailStr = "-"
		}

		cfgStr := res.Configured
		if cfgStr == "" {
			cfgStr = GetConfigSummary(res.ProfileName)
		}

		if !res.Active {
			errStr := res.Error
			if errStr == "" {
				errStr = "Inactive or not logged in"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t[!] Error: %s\t-\t-\t-\t-\n", pName, prioStr, emailStr, cfgStr, errStr)
			continue
		}

		if res.Quota == nil || len(res.Quota.Groups) == 0 {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t[!] No quota info available\t-\t-\t-\t-\n", pName, prioStr, emailStr, cfgStr)
			continue
		}

		for gIdx, group := range res.Quota.Groups {
			dispProfile := pName
			dispPrio := prioStr
			dispEmail := emailStr
			dispConfig := cfgStr
			if gIdx > 0 {
				dispProfile = ""
				dispPrio = ""
				dispEmail = ""
				dispConfig = ""
			}

			var b5h, bWeekly *QuotaBucket

			// First pass: exact match
			for i := range group.Buckets {
				b := &group.Buckets[i]
				winLower := strings.ToLower(b.Window)
				if winLower == "5h" {
					b5h = b
				} else if winLower == "weekly" {
					bWeekly = b
				}
			}

			// Second pass: semantic match across Window, DisplayName, and BucketID
			if b5h == nil || bWeekly == nil {
				for i := range group.Buckets {
					b := &group.Buckets[i]
					is5H, isWeekly := classifyQuotaKeyWindow(b.Window, b.DisplayName, b.BucketID)
					if b5h == nil && is5H {
						b5h = b
					}
					if bWeekly == nil && isWeekly {
						bWeekly = b
					}
				}
			}

			// Third pass: fallback by index
			if b5h == nil && bWeekly == nil && len(group.Buckets) > 0 {
				b5h = &group.Buckets[0]
				if len(group.Buckets) > 1 {
					bWeekly = &group.Buckets[1]
				}
			}

			// Fourth pass: only mirror a missing window when the available bucket is 100% full
			// (e.g. fresh accounts with only weekly=1.0), avoiding misleading non-100% cross-window values.
			isFallback5h := false
			isFallbackWeekly := false
			if b5h == nil && bWeekly != nil && bWeekly.RemainingFraction >= 1.0 {
				b5h = bWeekly
				isFallback5h = true
			} else if bWeekly == nil && b5h != nil && b5h.RemainingFraction >= 1.0 {
				bWeekly = b5h
				isFallbackWeekly = true
			}

			q5h := "N/A"
			r5h := "-"
			if b5h != nil {
				pct := b5h.RemainingFraction * 100
				bar := ProgressBar(b5h.RemainingFraction, 10)
				q5h = fmt.Sprintf("%5.1f%% [%s]", pct, bar)
				if !isFallback5h {
					r5h = FormatResetTime(b5h.ResetTime, b5h.RemainingFraction)
				}
			}

			qWeekly := "N/A"
			rWeekly := "-"
			if bWeekly != nil {
				pct := bWeekly.RemainingFraction * 100
				bar := ProgressBar(bWeekly.RemainingFraction, 10)
				qWeekly = fmt.Sprintf("%5.1f%% [%s]", pct, bar)
				if !isFallbackWeekly {
					rWeekly = FormatResetTime(bWeekly.ResetTime, bWeekly.RemainingFraction)
				}
			}

			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				dispProfile, dispPrio, dispEmail, dispConfig, group.DisplayName, q5h, r5h, qWeekly, rWeekly)
		}
	}

	tw.Flush()
}

// DetectDuplicateTokens scans all profiles and returns a map of profile names that share duplicate OAuth tokens.
func DetectDuplicateTokens() (map[string][]string, error) {
	profiles, err := List()
	if err != nil {
		return nil, err
	}

	fpMap := make(map[string][]string) // fingerprint -> []profileName
	for _, p := range profiles {
		if IsAuto(p) {
			continue
		}
		tok, err := ReadToken(p)
		if err == nil && tok != nil {
			fp := calculateTokenFingerprint(tok)
			if fp != "" {
				fpMap[fp] = append(fpMap[fp], p)
			}
		}
	}

	duplicates := make(map[string][]string)
	for _, pList := range fpMap {
		if len(pList) > 1 {
			for _, p := range pList {
				duplicates[p] = pList
			}
		}
	}

	return duplicates, nil
}



