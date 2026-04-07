package session

import (
	"errors"
	"testing"
)

func TestValidTransitions(t *testing.T) {
	tests := []struct {
		from  RunStatus
		to    RunStatus
		valid bool
	}{
		// From new (empty)
		{"", StatusRunning, true},
		{"", StatusDone, false},
		{"", StatusFailed, false},

		// From running
		{StatusRunning, StatusDone, true},
		{StatusRunning, StatusFailed, true},
		{StatusRunning, StatusKilled, true},
		{StatusRunning, StatusTimeout, true},
		{StatusRunning, StatusRunning, false}, // can't go running→running

		// From terminal states → restart
		{StatusDone, StatusRunning, true},
		{StatusFailed, StatusRunning, true},
		{StatusKilled, StatusRunning, true},
		{StatusTimeout, StatusRunning, true},

		// Terminal → terminal (not allowed)
		{StatusDone, StatusFailed, false},
		{StatusFailed, StatusDone, false},
		{StatusKilled, StatusTimeout, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			got := IsValidTransition(tt.from, tt.to)
			if got != tt.valid {
				t.Errorf("IsValidTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.valid)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	if IsTerminal(StatusRunning) {
		t.Error("running should not be terminal")
	}
	if IsTerminal("") {
		t.Error("empty should not be terminal")
	}
	for _, s := range []RunStatus{StatusDone, StatusFailed, StatusKilled, StatusTimeout} {
		if !IsTerminal(s) {
			t.Errorf("%s should be terminal", s)
		}
	}
}

func TestValidateTransition(t *testing.T) {
	if err := ValidateTransition("", StatusRunning); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	err := ValidateTransition(StatusRunning, StatusRunning)
	if err == nil {
		t.Fatal("expected error for running→running")
	}
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("expected TransitionError, got %T", err)
	}
	if te.From != StatusRunning || te.To != StatusRunning {
		t.Errorf("wrong fields: from=%s to=%s", te.From, te.To)
	}
}
