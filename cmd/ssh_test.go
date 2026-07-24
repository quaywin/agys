package cmd

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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
