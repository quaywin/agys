package profile

import (
	"bytes"
	"testing"
)

func TestIsQuotaError(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Normal output without errors", false},
		{"Error: 429 Too Many Requests from server", true},
		{"RESOURCE_EXHAUSTED: quota exceeded for metric", true},
		{"You have reached your quota limit for gemini-2.5-pro", true},
		{"Rate limit reached, please wait", true},
		{"Syntax error near token 'foo'", false},
	}

	for _, tt := range tests {
		result := IsQuotaError(tt.input)
		if result != tt.expected {
			t.Errorf("IsQuotaError(%q) = %v; want %v", tt.input, result, tt.expected)
		}
	}
}

func TestQuotaInterceptorWriter(t *testing.T) {
	var targetBuf bytes.Buffer
	writer := NewQuotaInterceptorWriter(&targetBuf)

	input := "Hello, this is a test line.\nRESOURCE_EXHAUSTED\n"
	n, err := writer.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != len(input) {
		t.Fatalf("expected written bytes %d, got %d", len(input), n)
	}

	if targetBuf.String() != input {
		t.Errorf("target writer content mismatch: got %q, want %q", targetBuf.String(), input)
	}

	captured := writer.String()
	if captured != input {
		t.Errorf("captured buffer mismatch: got %q, want %q", captured, input)
	}

	if !IsQuotaError(captured) {
		t.Errorf("expected IsQuotaError to return true for captured buffer")
	}
}
