package profile

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestRunWithFailover_Interactive(t *testing.T) {
	outWriter := NewQuotaInterceptorWriter(io.Discard)
	errWriter := NewQuotaInterceptorWriter(io.Discard)

	cmd := exec.Command("echo", "test interactive mode")
	err := RunWithFailover(cmd, outWriter, errWriter, true, true)
	if err != nil {
		t.Fatalf("expected no error running interactive command, got: %v", err)
	}
}

func TestRunWithFailover_NonInteractive(t *testing.T) {
	outWriter := NewQuotaInterceptorWriter(io.Discard)
	errWriter := NewQuotaInterceptorWriter(io.Discard)

	cmd := exec.Command("echo", "RESOURCE_EXHAUSTED test non-interactive mode")
	err := RunWithFailover(cmd, outWriter, errWriter, true, false)
	if err != nil {
		t.Fatalf("expected no error running non-interactive command, got: %v", err)
	}

	captured := outWriter.String()
	if !strings.Contains(captured, "RESOURCE_EXHAUSTED") {
		t.Errorf("expected outWriter to capture 'RESOURCE_EXHAUSTED', got: %q", captured)
	}

	if !IsQuotaError(captured) {
		t.Errorf("expected IsQuotaError to be true for captured output")
	}
}

func TestRunWithFailover_NoFailover(t *testing.T) {
	outWriter := NewQuotaInterceptorWriter(io.Discard)
	errWriter := NewQuotaInterceptorWriter(io.Discard)

	cmd := exec.Command("echo", "test no-failover mode")
	err := RunWithFailover(cmd, outWriter, errWriter, false, false)
	if err != nil {
		t.Fatalf("expected no error running command with failover disabled, got: %v", err)
	}
}
