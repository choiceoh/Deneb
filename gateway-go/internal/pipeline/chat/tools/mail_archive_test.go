package tools

import "testing"

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
