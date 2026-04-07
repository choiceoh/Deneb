package session

import (
	"math/rand"
	"testing"
	"testing/quick"
)

// TestTransitionsFromTerminalOnlyToRunning verifies that terminal states can
// only transition back to Running (the restart/retry path).
func TestTransitionsFromTerminalOnlyToRunning(t *testing.T) {
	terminals := []RunStatus{StatusDone, StatusFailed, StatusKilled, StatusTimeout}
	allStatuses := []RunStatus{StatusRunning, StatusDone, StatusFailed, StatusKilled, StatusTimeout}

	for _, from := range terminals {
		for _, to := range allStatuses {
			valid := IsValidTransition(from, to)
			if to == StatusRunning {
				if !valid {
					t.Errorf("terminal %q should transition to Running", from)
				}
			} else {
				if valid {
					t.Errorf("terminal %q should NOT transition to %q", from, to)
				}
			}
		}
	}
}

// TestRunningAlwaysReachesTerminal applies random valid transitions starting
// from Running. Since Running can only go to terminal states, a single
// transition from Running must always reach a terminal state.
func TestRunningAlwaysReachesTerminal(t *testing.T) {
	f := func(seed uint32) bool {
		rng := rand.New(rand.NewSource(int64(seed)))
		status := StatusRunning

		// Apply one valid transition from Running.
		targets := validTransitions[status]
		if len(targets) == 0 {
			return false
		}
		status = targets[rng.Intn(len(targets))]
		return IsTerminal(status)
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Errorf("property violated: %v", err)
	}
}

// TestValidateTransitionConsistency checks that ValidateTransition and
// IsValidTransition always agree.
func TestValidateTransitionConsistency(t *testing.T) {
	allStatuses := []RunStatus{"", StatusRunning, StatusDone, StatusFailed, StatusKilled, StatusTimeout}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			valid := IsValidTransition(from, to)
			err := ValidateTransition(from, to)
			if valid && err != nil {
				t.Errorf("IsValidTransition(%q→%q)=true but ValidateTransition returned error: %v", from, to, err)
			}
			if !valid && err == nil {
				t.Errorf("IsValidTransition(%q→%q)=false but ValidateTransition returned nil", from, to)
			}
		}
	}
}

// TestRandomTransitionSequencesNeverPanic applies random transitions and
// verifies no panics occur, even with invalid transitions.
func TestRandomTransitionSequencesNeverPanic(t *testing.T) {
	allStatuses := []RunStatus{"", StatusRunning, StatusDone, StatusFailed, StatusKilled, StatusTimeout}

	f := func(seed uint32) bool {
		rng := rand.New(rand.NewSource(int64(seed)))
		status := RunStatus("")

		for i := 0; i < 100; i++ {
			next := allStatuses[rng.Intn(len(allStatuses))]
			// These must never panic regardless of input.
			_ = IsValidTransition(status, next)
			_ = ValidateTransition(status, next)
			_ = IsTerminal(next)

			// Only advance if transition is valid.
			if IsValidTransition(status, next) {
				status = next
			}
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Errorf("property violated: %v", err)
	}
}

// TestIsTerminalMatchesValidTransitions verifies that IsTerminal is consistent
// with the valid transitions map — terminal states should allow transition to
// Running only, and Running should not be terminal.
func TestIsTerminalMatchesValidTransitions(t *testing.T) {
	for status, targets := range validTransitions {
		if status == "" {
			continue // initial state, not a real status
		}
		terminal := IsTerminal(status)
		if terminal {
			// Terminal states should only go to Running.
			if len(targets) != 1 || targets[0] != StatusRunning {
				t.Errorf("terminal %q has unexpected targets: %v", status, targets)
			}
		}
		if status == StatusRunning && terminal {
			t.Error("Running should not be terminal")
		}
	}
}
