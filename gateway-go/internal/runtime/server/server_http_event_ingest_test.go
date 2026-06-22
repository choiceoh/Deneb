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

// Gmail notifications are suppressed (gmail-poll already posts the mail_report
// card), but ONLY Gmail — other mail apps have no poll coverage so they must still
// run. And only notification-type events: a Gmail "source" on a clipboard/context
// event isn't a notification and must pass through.
func TestIsPolledGmailNotification(t *testing.T) {
	type tc struct {
		eventType string
		source    string
		want      bool
	}
	cases := []tc{
		// Gmail package (deneb-notification-watch forwards the package name) → skip.
		{"notification", "com.google.android.gm", true},
		{"notification", "com.google.android.gm.lite", true},
		{"", "com.google.android.gm", true}, // empty type defaults to notification
		{"notification", "Gmail", true},     // display-label fallback
		{"notification", "GMAIL", true},     // case-insensitive
		// Other mail apps: NOT covered by gmail-poll → must still surface.
		{"notification", "com.microsoft.office.outlook", false},
		{"notification", "com.samsung.android.email.provider", false},
		// Non-mail notifications run normally.
		{"notification", "com.kakao.talk", false},
		{"notification", "(미상)", false},
		{"notification", "", false},
		// Non-notification types pass through even from the Gmail app.
		{"clipboard", "com.google.android.gm", false},
		{"context", "com.google.android.gm", false},
		{"location_update", "com.google.android.gm", false},
	}
	for _, c := range cases {
		if got := isPolledGmailNotification(c.eventType, c.source); got != c.want {
			t.Errorf("isPolledGmailNotification(%q, %q) = %v, want %v", c.eventType, c.source, got, c.want)
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
