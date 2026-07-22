package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const (
	projectIDFilename = "project_id"
	oauthTokenURL     = "https://oauth2.googleapis.com/token"
)

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
	ProfileName string        `json:"profileName"`
	Active      bool          `json:"active"`
	Error       string        `json:"error,omitempty"`
	Quota       *QuotaSummary `json:"quota,omitempty"`
}

// ReadToken reads the oauth token for a given profile.
func ReadToken(profileName string) (*OAuthToken, error) {
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return nil, err
	}
	tokenPath := filepath.Join(profileDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("token file not found (not logged in)")
		}
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var oauthToken OAuthToken
	if err := json.Unmarshal(data, &oauthToken); err != nil {
		return nil, fmt.Errorf("failed to parse token JSON: %w", err)
	}
	return &oauthToken, nil
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
	tokenPath := filepath.Join(profileDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	updatedData, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated token: %w", err)
	}

	return os.WriteFile(tokenPath, updatedData, 0600)
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
func FetchQuota(ctx context.Context, profileName string) (*QuotaSummary, error) {
	// 1. Read token
	token, err := ReadToken(profileName)
	if err != nil {
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

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user quota: %w", err)
	}

	return summary, nil
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
	req.Header.Set("User-Agent", "antigravity/1.11.9 darwin/arm64")

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
	req.Header.Set("User-Agent", "antigravity/1.11.9 darwin/arm64")

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

