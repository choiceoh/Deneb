package session

import (
	"fmt"
)

// TransitionError is returned when a state transition is invalid.
type TransitionError struct {
	From RunStatus
	To   RunStatus
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid state transition: %s → %s", e.From, e.To)
}

// Unwrap implements the errors.Unwrap interface. TransitionError is purely
// structural (no underlying cause), so this always returns nil.
func (e *TransitionError) Unwrap() error { return nil }

// validTransitions defines which status transitions are allowed.
// Empty string ("") represents no status (new session).
var validTransitions = map[RunStatus][]RunStatus{
	"":            {StatusRunning},
	StatusRunning: {StatusDone, StatusFailed, StatusKilled, StatusTimeout},
	StatusDone:    {StatusRunning}, // restart
	StatusFailed:  {StatusRunning}, // retry
	StatusKilled:  {StatusRunning}, // restart
	StatusTimeout: {StatusRunning}, // retry
}

// IsValidTransition checks if a transition from one status to another is allowed.
func IsValidTransition(from, to RunStatus) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// IsTerminal returns true if the status represents a terminal state.
func IsTerminal(status RunStatus) bool {
	switch status {
	case StatusDone, StatusFailed, StatusKilled, StatusTimeout:
		return true
	default:
		return false
	}
}

// ValidateTransition returns an error if the transition is not allowed.
func ValidateTransition(from, to RunStatus) error {
	if !IsValidTransition(from, to) {
		return &TransitionError{From: from, To: to}
	}
	return nil
}
