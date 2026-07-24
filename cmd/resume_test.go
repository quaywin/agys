package cmd

import (
	"reflect"
	"testing"

	"github.com/quaywin/agys/pkg/profile"
)

func TestBuildResumeAgyArgs(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	convID := "test-conv-999"
	savedFlags := []string{"--dangerously-skip-permissions", "--model=flash"}
	_ = profile.SaveSessionFlags(convID, savedFlags)

	// Case 1: No extra args passed -> should restore saved flags
	args1 := buildResumeAgyArgs(convID, nil)
	expected1 := []string{"--conversation=" + convID, "--dangerously-skip-permissions", "--model=flash"}
	if !reflect.DeepEqual(args1, expected1) {
		t.Errorf("Case 1 failed: expected %v, got %v", expected1, args1)
	}

	// Case 2: Extra args passed overriding a flag -> should prioritize extra args
	extra2 := []string{"--model=pro"}
	args2 := buildResumeAgyArgs(convID, extra2)
	expected2 := []string{"--conversation=" + convID, "--dangerously-skip-permissions", "--model=pro"}
	if !reflect.DeepEqual(args2, expected2) {
		t.Errorf("Case 2 failed: expected %v, got %v", expected2, args2)
	}
}
