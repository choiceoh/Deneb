package session

import "testing"

func BenchmarkIsValidTransition(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IsValidTransition(StatusRunning, StatusDone)
	}
}

func BenchmarkIsTerminal(b *testing.B) {
	statuses := []RunStatus{StatusRunning, StatusDone, StatusFailed, StatusKilled, StatusTimeout}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsTerminal(statuses[i%len(statuses)])
	}
}

func BenchmarkValidateTransition(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ValidateTransition(StatusRunning, StatusDone)
	}
}
