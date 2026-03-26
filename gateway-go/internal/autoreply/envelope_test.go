package autoreply

import (
	"strings"
	"testing"
	"time"
)

func TestFormatEnvelopeTimestamp(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC)

	t.Run("utc", func(t *testing.T) {
		got := FormatEnvelopeTimestamp(ts, "utc")
		if !strings.Contains(got, "2024-06-15") || !strings.Contains(got, "12:30") {
			t.Errorf("unexpected: %q", got)
		}
	})

	t.Run("empty defaults to utc", func(t *testing.T) {
		got := FormatEnvelopeTimestamp(ts, "")
		if !strings.Contains(got, "UTC") {
			t.Errorf("expected UTC, got %q", got)
		}
	})
}

func TestFormatInboundFromLabel(t *testing.T) {
	tests := []struct {
		from     string
		isGroup  bool
		senderID string
		want     string
	}{
		{"Alice", false, "", "Alice"},
		{"Alice", true, "12345", "Alice (12345)"},
		{"", false, "", "User"},
	}
	for _, tt := range tests {
		got := FormatInboundFromLabel(tt.from, tt.isGroup, tt.senderID)
		if got != tt.want {
			t.Errorf("FormatInboundFromLabel(%q, %v, %q) = %q, want %q", tt.from, tt.isGroup, tt.senderID, got, tt.want)
		}
	}
}

func TestFormatInboundEnvelope(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC)
	opts := DefaultEnvelopeOptions()
	opts.ShowChannel = true

	got := FormatInboundEnvelope(InboundEnvelopeParams{
		Channel:   "telegram",
		From:      "Alice",
		Timestamp: ts,
		Options:   &opts,
	})
	if !strings.Contains(got, "telegram") {
		t.Errorf("expected channel in envelope: %q", got)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("expected sender in envelope: %q", got)
	}
}

func TestSanitizeEnvelopeHeaderPart(t *testing.T) {
	got := sanitizeEnvelopeHeaderPart("Hello\nWorld [test]")
	if strings.Contains(got, "\n") {
		t.Error("newlines should be collapsed")
	}
	if strings.Contains(got, "[") || strings.Contains(got, "]") {
		t.Error("brackets should be removed")
	}
}
