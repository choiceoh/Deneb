package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sessionsOlderThan is the backward-walk primitive: given a newest-first list it
// returns the tail strictly older than the frontier, treating frontier<=0 as a
// fresh sweep from the top.
func TestSessionsOlderThan(t *testing.T) {
	sessions := []sessionMod{ // newest-first
		{key: "a", modMs: 300},
		{key: "b", modMs: 200},
		{key: "c", modMs: 100},
	}
	if got := sessionsOlderThan(sessions, 0); len(got) != 3 {
		t.Errorf("frontier 0 (fresh sweep): got %d, want all 3", len(got))
	}
	if got := sessionsOlderThan(sessions, 999); len(got) != 3 {
		t.Errorf("frontier above newest: got %d, want all 3", len(got))
	}
	got := sessionsOlderThan(sessions, 200) // strictly older than 200 → only c
	if len(got) != 1 || got[0].key != "c" {
		t.Errorf("frontier 200: got %v, want [c]", got)
	}
	if got := sessionsOlderThan(sessions, 100); len(got) != 0 {
		t.Errorf("frontier at the oldest (bottom reached): got %v, want empty", got)
	}
}

// reviewableSessionsByMtime lists reviewable sessions newest-first with mtimes,
// applying the same client-only filter as the idle-review lane.
func TestReviewableSessionsByMtimeNewestFirstAndFiltered(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	write := func(name string, mt time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	write("client:one.jsonl", base.Add(1*time.Second))
	write("client:two.jsonl", base.Add(3*time.Second))      // newest reviewable
	write("cron:system.jsonl", base.Add(5*time.Second))     // filtered: non-client
	write("client:puppet-x.jsonl", base.Add(4*time.Second)) // filtered: puppet
	write("notes.txt", base.Add(6*time.Second))             // filtered: not .jsonl

	got, err := reviewableSessionsByMtime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d reviewable sessions, want 2 (%v)", len(got), got)
	}
	if got[0].key != "client:two" || got[1].key != "client:one" {
		t.Fatalf("order = %v, want newest-first [client:two, client:one]", got)
	}
	if !(got[0].modMs > got[1].modMs) {
		t.Errorf("mtimes not newest-first: %d then %d", got[0].modMs, got[1].modMs)
	}
}

// The cursor survives a save/load round-trip; a missing file starts a fresh
// sweep (zero frontier) without surfacing an error.
func TestProspectCursorRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	if err := saveProspectCursor(path, prospectCursor{FrontierMs: 4242}); err != nil {
		t.Fatal(err)
	}
	if got := loadProspectCursor(path); got.FrontierMs != 4242 {
		t.Errorf("FrontierMs = %d, want 4242", got.FrontierMs)
	}
	if got := loadProspectCursor(filepath.Join(t.TempDir(), "absent.json")); got.FrontierMs != 0 {
		t.Errorf("missing cursor FrontierMs = %d, want 0 (fresh sweep)", got.FrontierMs)
	}
}
