package mailarchive

import (
	"strings"
	"testing"
	"time"
)

// The archive's recency window must filter on the Date header (SENTSINCE) with
// a one-day back-margin — INTERNALDATE (SINCE) missed same-day and previous-day
// mail in production (2026-07-04: `list days:1` at 17:00 → "오늘 수신 메일 없음"
// while a 09:50 mail existed; morning `days:2` missed all of yesterday's mail).
func TestArchiveSentSinceCriteria_DateHeaderWithMargin(t *testing.T) {
	since := time.Date(2026, 7, 4, 17, 0, 0, 0, time.FixedZone("KST", 9*3600))
	got := archiveSentSinceCriteria(since)
	want := "SENTSINCE 03-Jul-2026"
	if got != want {
		t.Fatalf("archiveSentSinceCriteria = %q, want %q", got, want)
	}
}

func TestArchiveTextCriteria_SingleTokenKeepsShape(t *testing.T) {
	got := archiveTextCriteria("당진솔라빌리지")
	want := `OR OR FROM "당진솔라빌리지" SUBJECT "당진솔라빌리지" TEXT "당진솔라빌리지"`
	if got != want {
		t.Fatalf("single token criteria = %q, want %q", got, want)
	}
}

// Multi-word mixed-language queries must AND per-token field-ORs instead of
// requiring the whole query as one verbatim phrase. The old phrase form made
// the production query below return 0 hits even though every word matched
// some message individually.
func TestArchiveTextCriteria_MultiTokenANDsPerTokenORs(t *testing.T) {
	got := archiveTextCriteria("당진솔라빌리지 EPC Taiwoo")
	wantParts := []string{
		`OR OR FROM "당진솔라빌리지" SUBJECT "당진솔라빌리지" TEXT "당진솔라빌리지"`,
		`OR OR FROM "EPC" SUBJECT "EPC" TEXT "EPC"`,
		`OR OR FROM "Taiwoo" SUBJECT "Taiwoo" TEXT "Taiwoo"`,
	}
	if got != strings.Join(wantParts, " ") {
		t.Fatalf("multi token criteria = %q", got)
	}
}

func TestArchiveTextCriteria_CapsTokenCount(t *testing.T) {
	got := archiveTextCriteria("a b c d e f g h")
	if n := strings.Count(got, "FROM"); n != 6 {
		t.Fatalf("token cap: got %d FROM groups, want 6 (criteria %q)", n, got)
	}
	if strings.Contains(got, `"g"`) || strings.Contains(got, `"h"`) {
		t.Fatalf("tokens beyond cap leaked into criteria %q", got)
	}
}
