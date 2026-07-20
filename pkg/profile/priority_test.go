package profile

import (
	"testing"
)

func TestPriorityManagement(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	profile1 := "work"
	profile2 := "personal"

	_, _ = Create(profile1)
	_, _ = Create(profile2)

	// Default priority should be 0
	if p := GetPriority(profile1); p != 0 {
		t.Errorf("expected default priority 0 for %s, got %d", profile1, p)
	}

	// Set priority for profile1 to 10
	if err := SetPriority(profile1, 10); err != nil {
		t.Fatalf("SetPriority error: %v", err)
	}

	// Set priority for profile2 to 5
	if err := SetPriority(profile2, 5); err != nil {
		t.Fatalf("SetPriority error: %v", err)
	}

	if p := GetPriority(profile1); p != 10 {
		t.Errorf("expected priority 10 for %s, got %d", profile1, p)
	}

	if p := GetPriority(profile2); p != 5 {
		t.Errorf("expected priority 5 for %s, got %d", profile2, p)
	}

	// Setting priority on auto reserved keyword should fail
	if err := SetPriority("auto", 10); err == nil {
		t.Errorf("expected error when setting priority for reserved keyword auto")
	}

	// Setting priority on non-existent profile should fail
	if err := SetPriority("nonexistent", 10); err == nil {
		t.Errorf("expected error when setting priority for non-existent profile")
	}
}
