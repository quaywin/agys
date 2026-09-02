package cmd

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/quaywin/agys/pkg/profile"
)

func TestStartLocalHTTPProxy_Connect(t *testing.T) {
	// Create dummy target HTTPS server
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from target server")
	}))
	defer ts.Close()

	// Start local HTTP proxy
	proxyPort, cleanup, err := startLocalHTTPProxy()
	if err != nil {
		t.Fatalf("failed to start local proxy: %v", err)
	}
	defer cleanup()

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", proxyPort))

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("failed to make request through HTTP proxy: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if string(body) != "hello from target server\n" {
		t.Errorf("unexpected body: %q", string(body))
	}
}

func TestSSHCommandModelDefaults(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)
	_ = profile.SaveCachedDiscoveredModels(&profile.DiscoveredModels{
		FetchedAt:   time.Now(),
		LatestFlash: profile.DefaultGeminiModel,
		LatestPro:   "gemini-3.1-pro",
		AllModels:   []string{profile.DefaultGeminiModel, "gemini-3.1-pro"},
	})

	t.Run("Default model and effort appended when agyArgs empty", func(t *testing.T) {
		var agyArgs []string
		res := EnsureDefaultModelAndEffort(agyArgs)
		expected := []string{"--model", "gemini-3.8-flash", "--effort", "high"}
		if len(res) != len(expected) {
			t.Fatalf("expected %v, got %v", expected, res)
		}
		for i, v := range expected {
			if res[i] != v {
				t.Errorf("at index %d: expected %q, got %q", i, v, res[i])
			}
		}
	})

	t.Run("Custom model preserved for ssh args", func(t *testing.T) {
		agyArgs := []string{"--model", "gemini-2.5-pro"}
		res := EnsureDefaultModelAndEffort(agyArgs)
		if len(res) != 2 || res[1] != "gemini-2.5-pro" {
			t.Errorf("expected custom model to be preserved, got %v", res)
		}
	})
}

