package mailtool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
)

// mailArchivePath drives the per-call phase-timing log: a "store" hit is the fast
// in-memory path, while "imap-fallback" / "attachment" are the slow paths that
// surface at Info so a slow mail_archive call is attributable from the log.
func TestMailArchivePath(t *testing.T) {
	cases := []struct {
		action   string
		usedIMAP bool
		want     string
	}{
		{"search", false, "store"},        // local store answered
		{"search", true, "imap-fallback"}, // store miss → full IMAP fetch+parse
		{"read", true, "imap-fallback"},   // stale/older mail not in store
		{"thread", false, "store"},        // in-memory thread graph
		{"project_history", true, "imap-fallback"},
		{"list", false, "store"},
		{"", false, "store"},                // empty action = list, store hit
		{"attachment", false, "attachment"}, // always IMAP + OCR, regardless of usedIMAP
		{"attachment", true, "attachment"},
	}
	for _, c := range cases {
		if got := mailArchivePath(c.action, c.usedIMAP); got != c.want {
			t.Errorf("mailArchivePath(%q, %v) = %q, want %q", c.action, c.usedIMAP, got, c.want)
		}
	}
}

// TestMailArchiveSearchWidensPastDaysWindow: the model routinely attaches a
// `days` window to keyword searches even when the user asked for no recency, so
// a match older than the window returned zero from the fast store and paid a
// slow IMAP round-trip (which applies the same window). The search must instead
// widen to all-time in the store and surface the older match, labeled so the
// model does not present it as recent.
func TestMailArchiveSearchWidensPastDaysWindow(t *testing.T) {
	// Store-only: empty IMAP creds ⇒ imapReady=false, so a store miss cannot fall
	// back — the widen is the only way the older mail can surface.
	t.Setenv("DENEB_ARCHIVE_IMAP_USER", "")
	t.Setenv("DENEB_ARCHIVE_IMAP_PASS", "")
	t.Setenv("DENEB_ARCHIVE_IMAP_MAILBOXES", "") // default [INBOX, Gmail]

	dir := t.TempDir()
	s, err := mailstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 400 days old ⇒ always outside a 7-day window, whenever the test runs.
	old := mailarchive.ContextMessage{
		ID: "old1", Locator: "INBOX:old1", Mailbox: "INBOX", UID: "old1",
		MessageID: "<old1@x>", From: "sender@example.com",
		Subject: "진코 Jinko 모듈 견적", Body: "Jinko 550W 모듈 단가 문의",
		Date: time.Now().AddDate(0, 0, -400).Format(time.RFC1123Z),
	}
	if _, err := s.Put(old); err != nil {
		t.Fatal(err)
	}
	tool := ToolMailArchive(MailArchiveDeps{Store: s})

	// days=7 excludes the old mail from the bounded pass; the widen retrieves it.
	out, err := tool(context.Background(), json.RawMessage(`{"action":"search","query":"진코 모듈","days":7}`))
	if err != nil {
		t.Fatalf("bounded search: %v", err)
	}
	if !strings.Contains(out, "진코 Jinko 모듈 견적") {
		t.Errorf("widened search dropped the older match:\n%s", out)
	}
	if !strings.Contains(out, "전체 기간") {
		t.Errorf("widened search must label results as outside the window:\n%s", out)
	}

	// The same query without a window returns the match plainly — no widen label.
	out2, err := tool(context.Background(), json.RawMessage(`{"action":"search","query":"진코 모듈"}`))
	if err != nil {
		t.Fatalf("unbounded search: %v", err)
	}
	if !strings.Contains(out2, "진코 Jinko 모듈 견적") {
		t.Errorf("unbounded search missed the match:\n%s", out2)
	}
	if strings.Contains(out2, "전체 기간") {
		t.Errorf("unbounded search must not carry the widen label:\n%s", out2)
	}
}

func TestHasHangul(t *testing.T) {
	cases := map[string]bool{
		"황승민 한화생명":    true,
		"진코 Jinko":    true, // mixed script → true
		"Jinko EPC":   false,
		"EPC O&M 250": false,
		"":            false,
		"ㅎㅇ":          true, // compatibility jamo
	}
	for in, want := range cases {
		if got := hasHangul(in); got != want {
			t.Errorf("hasHangul(%q) = %v, want %v", in, got, want)
		}
	}
}

// A Korean text-search miss must NOT pay the CJK-blind IMAP fallback: the local
// mirror is authoritative for Hangul (Dovecot has no CJK full-text index, so the
// fallback is both slower and lower-recall). The tool trusts a 0-hit and skips
// IMAP. An ASCII miss still falls back — IMAP TEXT matches Latin and may reach
// mail not yet mirrored.
func TestMailArchiveSearchGatesHangulFallback(t *testing.T) {
	dir := t.TempDir()
	s, err := mailstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// One English-only message: the store is ready but nothing matches the probes.
	if _, err := s.Put(mailarchive.ContextMessage{
		ID: "m1", Locator: "INBOX:m1", Mailbox: "INBOX", UID: "m1",
		MessageID: "<m1@x>", From: "a@example.com",
		Subject: "Quarterly EPC report", Body: "Solar EPC scope only",
		Date: time.Now().Format(time.RFC1123Z),
	}); err != nil {
		t.Fatal(err)
	}

	newQuery := func(query string) (mailArchiveQuery, *bool, *bool, *int) {
		usedIMAP, fallbackSkipped, storeHits := false, false, -1
		q := mailArchiveQuery{
			deps: MailArchiveDeps{Store: s},
			args: mailArchiveArgs{Action: "search", Query: query},
			// A dead archive addr makes any *attempted* fallback fail fast, so the
			// test tells "skipped" (no error) apart from "attempted" (dial error).
			cfg:             mailarchive.Config{Addr: "127.0.0.1:1", User: "u", Pass: "p"},
			opts:            mailarchive.ContextOptions{Limit: 50},
			storeReady:      true,
			imapReady:       true,
			usedIMAP:        &usedIMAP,
			storeHits:       &storeHits,
			fallbackSkipped: &fallbackSkipped,
		}
		return q, &usedIMAP, &fallbackSkipped, &storeHits
	}

	// Hangul miss → gated: no IMAP, no error.
	qk, usedK, skipK, hitsK := newQuery("없는회사 한화생명")
	if _, err := qk.search(context.Background()); err != nil {
		t.Fatalf("hangul search must not error (fallback gated): %v", err)
	}
	if *usedK {
		t.Error("hangul miss must NOT use IMAP fallback")
	}
	if !*skipK {
		t.Error("hangul miss must record fallbackSkipped=true")
	}
	if *hitsK != 0 {
		t.Errorf("expected storeHits=0, got %d", *hitsK)
	}

	// ASCII miss → not gated: fallback attempted (and errors on the dead addr).
	qa, usedA, skipA, _ := newQuery("nonexistentcorp")
	if _, err := qa.search(context.Background()); err == nil {
		t.Error("ascii miss must attempt IMAP fallback and error on the dead addr")
	}
	if !*usedA {
		t.Error("ascii miss must attempt IMAP fallback (usedIMAP=true)")
	}
	if *skipA {
		t.Error("ascii miss must NOT set fallbackSkipped")
	}
}
