package shadow

import "testing"

func TestNormalizeErrorPattern(t *testing.T) {
	p1 := normalizeErrorPattern("connection refused at port 8080", "connection refused")
	p2 := normalizeErrorPattern("connection refused at port 9090", "connection refused")
	// After normalization, port numbers should be replaced with N.
	if p1 != p2 {
		t.Errorf("patterns should match after normalization: %q vs %q", p1, p2)
	}
}

func TestErrorLearnerFormatForPrompt(t *testing.T) {
	svc := NewService(Config{})
	el := svc.errorLearner

	// No errors → empty.
	if got := el.FormatForPrompt(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Record same error twice → should appear in prompt.
	el.recordError("connection refused", "connection refused at port 8080", "test")
	if got := el.FormatForPrompt(); got != "" {
		t.Errorf("expected empty for 1 occurrence, got %q", got)
	}

	el.recordError("connection refused", "connection refused at port 9090", "test")
	got := el.FormatForPrompt()
	if got == "" {
		t.Error("expected non-empty for 2+ occurrences")
	}
	if !contains(got, "2회 발생") {
		t.Errorf("expected '2회 발생' in output, got %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
