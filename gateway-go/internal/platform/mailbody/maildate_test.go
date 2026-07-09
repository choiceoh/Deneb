package mailbody

import (
	"testing"
	"time"
)

func TestParseMailDate(t *testing.T) {
	// Standard RFC 5322.
	got := ParseMailDate("Thu, 10 Jul 2026 14:30:00 +0900")
	want := time.Date(2026, 7, 10, 14, 30, 0, 0, time.FixedZone("+0900", 9*3600))
	if !got.Equal(want) {
		t.Errorf("RFC5322: got %v, want %v", got, want)
	}

	// Empty / whitespace → zero time (fail-open).
	if got := ParseMailDate("   "); !got.IsZero() {
		t.Errorf("whitespace should be zero, got %v", got)
	}
	if got := ParseMailDate(""); !got.IsZero() {
		t.Errorf("empty should be zero, got %v", got)
	}
}

// TestParseMailDateBareKST is the regression that motivated the canonical
// helper: Korean groupware sends a bare "... 23:30:00 KST" zone token which
// net/mail parses as UTC+0, shifting late-evening mail to the next calendar
// day. The bare-token rewrite to "+0900" must keep it on the same KST day.
// (The archive path previously lacked this fix and mis-filed by day.)
func TestParseMailDateBareKST(t *testing.T) {
	got := ParseMailDate("2026-07-10 23:30:00 KST")
	kst := time.FixedZone("KST", 9*3600)
	want := time.Date(2026, 7, 10, 23, 30, 0, 0, kst)
	// Same wall-clock day (10th) in KST — the rewrite prevents a 11th roll-over.
	if got.In(kst).Day() != 10 {
		t.Errorf("bare KST shifted day: got %v (KST day %d), want day 10",
			got, got.In(kst).Day())
	}
	if !got.Equal(want) {
		t.Errorf("bare KST: got %v, want %v", got, want)
	}

	// "(KST)" comment after a numeric offset is NOT rewritten — left intact.
	if got := ParseMailDate("Thu, 10 Jul 2026 23:30:00 +0900 (KST)"); got.IsZero() {
		t.Error("numeric-offset + (KST) comment should still parse")
	}
}
