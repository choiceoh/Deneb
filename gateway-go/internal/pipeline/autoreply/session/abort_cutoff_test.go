package session

import (
	"testing"
)

func TestShouldSkipMessageByAbortCutoff(t *testing.T) {
	ts := func(v int64) *int64 { return &v }

	tests := []struct {
		name      string
		cutoffSid string
		cutoffTS  *int64
		msgSid    string
		msgTS     *int64
		wantSkip  bool
	}{
		{"both empty", "", nil, "", nil, false},
		{"equal numeric SIDs", "100", nil, "100", nil, true},
		{"msg before cutoff SID", "100", nil, "50", nil, true},
		{"msg after cutoff SID", "100", nil, "200", nil, false},
		{"large numeric SIDs", "99999999999999", nil, "99999999999998", nil, true},
		{"equal string SIDs", "abc", nil, "abc", nil, true},
		{"different string SIDs", "abc", nil, "def", nil, false},
		{"timestamp equal", "", ts(1000), "", ts(1000), true},
		{"timestamp before", "", ts(1000), "", ts(500), true},
		{"timestamp after", "", ts(1000), "", ts(1500), false},
		{"SID takes precedence over timestamp", "100", ts(9999), "200", ts(1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldSkipMessageByAbortCutoff(tt.cutoffSid, tt.cutoffTS, tt.msgSid, tt.msgTS)
			if got != tt.wantSkip {
				t.Errorf("got %v, want %v", got, tt.wantSkip)
			}
		})
	}
}

func TestShouldPersistAbortCutoff(t *testing.T) {
	// Same session.
	if !ShouldPersistAbortCutoff("sess:1", "sess:1") {
		t.Error("same session should persist")
	}
	// Different sessions.
	if ShouldPersistAbortCutoff("sess:1", "sess:2") {
		t.Error("different sessions should not persist")
	}
	// Empty command key defaults to true.
	if !ShouldPersistAbortCutoff("", "sess:1") {
		t.Error("empty command key should persist")
	}
}

func TestAbortCutoffLifecycle(t *testing.T) {
	entry := &SessionAbortCutoffEntry{}

	// Initially no cutoff.
	if HasAbortCutoff(entry) {
		t.Error("should not have cutoff initially")
	}

	// Apply cutoff.
	cutoff := &AbortCutoffContext{MessageSid: "123"}
	ApplyAbortCutoffToSessionEntry(entry, cutoff)
	if !HasAbortCutoff(entry) {
		t.Error("should have cutoff after apply")
	}
	if entry.AbortCutoffMessageSid != "123" {
		t.Errorf("sid = %q", entry.AbortCutoffMessageSid)
	}

	// Read back.
	read := ReadAbortCutoffFromSessionEntry(entry)
	if read == nil || read.MessageSid != "123" {
		t.Errorf("read = %+v", read)
	}

	// Clear.
	if !ClearAbortCutoffInSession(entry) {
		t.Error("clear should succeed")
	}
	if HasAbortCutoff(entry) {
		t.Error("should not have cutoff after clear")
	}

	// Clear again should be no-op.
	if ClearAbortCutoffInSession(entry) {
		t.Error("second clear should return false")
	}
}

func TestFormatTimestampWithAge(t *testing.T) {
	if FormatTimestampWithAge(0) != "n/a" {
		t.Error("zero should be n/a")
	}
	if FormatTimestampWithAge(-1) != "n/a" {
		t.Error("negative should be n/a")
	}
	// Valid timestamp should contain "ago".
	result := FormatTimestampWithAge(1000000000000) // ~2001
	if result == "n/a" {
		t.Error("valid timestamp should not be n/a")
	}
}
