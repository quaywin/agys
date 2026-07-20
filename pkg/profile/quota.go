package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

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

// RefreshToken runs a harmless agy command to trigger agy's auto-refresh logic.
func RefreshToken(profileName string) error {
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return err
	}

	// Create context with timeout to run agy models
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := BuildCmd(profileDir, "models")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.Run()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("token refresh timed out")
	case err := <-errChan:
		return err
	}
}

// FetchQuota retrieves the quota summary for a specific profile.
func FetchQuota(ctx context.Context, profileName string) (*QuotaSummary, error) {
	// 1. Read token
	token, err := ReadToken(profileName)
	if err != nil {
		return nil, err
	}

	// 2. If token is expired or close to it, try to refresh it
	if IsTokenExpired(token) {
		_ = RefreshToken(profileName) // Ignore error, try anyway
		// Re-read token
		token, err = ReadToken(profileName)
		if err != nil {
			return nil, fmt.Errorf("token expired and failed to refresh: %w", err)
		}
	}

	accessToken := token.Token.AccessToken
	if accessToken == "" {
		return nil, fmt.Errorf("access token is empty")
	}

	// 3. loadCodeAssist to get project ID
	projectID, err := loadCodeAssist(ctx, accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to load code assist project: %w", err)
	}

	// 4. retrieveUserQuotaSummary
	summary, err := retrieveUserQuotaSummary(ctx, accessToken, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user quota: %w", err)
	}

	return summary, nil
}

func loadCodeAssist(ctx context.Context, accessToken string) (string, error) {
	url := "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"

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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP status %d: %s", resp.StatusCode, string(body))
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
	url := "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"

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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP status %d: %s", resp.StatusCode, string(body))
	}

	var summary QuotaSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, err
	}

	return &summary, nil
}
