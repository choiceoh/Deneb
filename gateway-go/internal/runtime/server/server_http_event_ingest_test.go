package server

import (
	"strings"
	"testing"
)

// The usage event type must skip the notification-specific tiny gate (its ads/OTP
// classifier doesn't fit a usage digest) and run the full default-silence judgment,
// like context/clipboard. notification/sms keep the gate.
func TestNotificationLikeEvent(t *testing.T) {
	cases := map[string]bool{
		"notification": true,
		"sms":          true,
		"":             true, // defaults to notification
		"freeform":     true,
		"context":      false,
		"clipboard":    false,
		"usage":        false,
		"USAGE":        false, // case-insensitive
	}
	for typ, want := range cases {
		if got := notificationLikeEvent(typ); got != want {
			t.Errorf("notificationLikeEvent(%q) = %v, want %v", typ, got, want)
		}
	}
}

// The usage type carries its own label + guidance, and the guidance embeds the
// NO_REPLY placeholder so its default-silence branch can be filled by the caller.
func TestUsageEventLabelAndGuidance(t *testing.T) {
	if got := phoneEventKindLabel("usage"); got != "앱 사용 리듬" {
		t.Errorf("phoneEventKindLabel(usage) = %q", got)
	}
	guidance := phoneEventGuidance("usage")
	if !strings.Contains(guidance, "%s") {
		t.Errorf("usage guidance missing NO_REPLY placeholder: %q", guidance)
	}
}
