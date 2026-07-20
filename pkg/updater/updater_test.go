package updater

import (
	"testing"
)

func TestCleanVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"v1.2.3", "1.2.3"},
		{"V0.1.0", "0.1.0"},
		{" 0.5.0 ", "0.5.0"},
		{"v2.0.0-beta.1", "2.0.0-beta.1"},
	}

	for _, tt := range tests {
		got := CleanVersion(tt.input)
		if got != tt.expected {
			t.Errorf("CleanVersion(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "0.1.1", -1},
		{"0.2.0", "0.1.9", 1},
		{"v1.0.0", "1.0.0", 0},
		{"v1.1.0", "v1.2.0", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"0.1.0-dev", "0.1.0", -1},
		{"0.1.0", "0.1.0-dev", 1},
	}

	for _, tt := range tests {
		got := CompareVersions(tt.v1, tt.v2)
		if got != tt.expected {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.expected)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("0.1.0", "0.1.1") {
		t.Errorf("Expected IsNewer(0.1.0, 0.1.1) to be true")
	}
	if IsNewer("0.2.0", "0.1.9") {
		t.Errorf("Expected IsNewer(0.2.0, 0.1.9) to be false")
	}
	if IsNewer("1.0.0", "1.0.0") {
		t.Errorf("Expected IsNewer(1.0.0, 1.0.0) to be false")
	}
}

func TestGetArchiveName(t *testing.T) {
	got := GetArchiveName("v0.2.0", "darwin", "arm64")
	expected := "agys_0.2.0_darwin_arm64.tar.gz"
	if got != expected {
		t.Errorf("GetArchiveName() = %q, want %q", got, expected)
	}
}
