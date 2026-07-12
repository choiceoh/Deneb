package session

import "testing"

func BenchmarkIsValidTransition(b *testing.B) {
	for range b.N {
		IsValidTransition(StatusRunning, StatusDone)
	}
}

func BenchmarkIsTerminal(b *testing.B) {
	statuses := []RunStatus{StatusRunning, StatusDone, StatusFailed, StatusKilled, StatusTimeout}
	b.ResetTimer()
	for i := range b.N {
		IsTerminal(statuses[i%len(statuses)])
	}
}

func BenchmarkValidateTransition(b *testing.B) {
	for range b.N {
		_ = ValidateTransition(StatusRunning, StatusDone)
	}
}
