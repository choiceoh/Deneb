package calprop

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "calendar_proposals.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestCreateIfAbsent_DedupsBySource(t *testing.T) {
	s := newTestStore(t)
	in := CreateInput{Title: "킥오프 미팅", Start: "2026-06-30", AllDay: true, Kind: "meeting", Source: "mail:m1|킥오프 미팅"}

	p1, created, err := s.CreateIfAbsent(in)
	if err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	if p1.Status != StatusPending || p1.ID == "" {
		t.Fatalf("bad proposal: %+v", p1)
	}
	p2, created2, err := s.CreateIfAbsent(in)
	if err != nil {
		t.Fatalf("second create err: %v", err)
	}
	if created2 {
		t.Error("second CreateIfAbsent with same Source should not create")
	}
	if p2.ID != p1.ID {
		t.Errorf("dedup returned a different proposal: %s vs %s", p2.ID, p1.ID)
	}
}

func TestListPending_AndDecide(t *testing.T) {
	s := newTestStore(t)
	a, _, _ := s.CreateIfAbsent(CreateInput{Title: "A", Start: "2026-07-02", AllDay: true, Source: "s:a"})
	b, _, _ := s.CreateIfAbsent(CreateInput{Title: "B", Start: "2026-07-01", AllDay: true, Source: "s:b"})

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	// sorted by Start: B (07-01) before A (07-02)
	if pending[0].ID != b.ID || pending[1].ID != a.ID {
		t.Errorf("pending not sorted by start: %s,%s", pending[0].Title, pending[1].Title)
	}

	// accept A → no longer pending, carries the event id
	if _, err := s.Decide(a.ID, StatusAccepted, "local:evt1"); err != nil {
		t.Fatalf("Decide accept: %v", err)
	}
	// reject B
	if _, err := s.Decide(b.ID, StatusRejected, ""); err != nil {
		t.Fatalf("Decide reject: %v", err)
	}
	pending, _ = s.ListPending()
	if len(pending) != 0 {
		t.Fatalf("want 0 pending after decisions, got %d", len(pending))
	}
	got, _ := s.Get(a.ID)
	if got == nil || got.Status != StatusAccepted || got.CalendarEventID != "local:evt1" {
		t.Errorf("accepted proposal: %+v", got)
	}
}

func TestCreateIfAbsent_RejectedNotReproposed(t *testing.T) {
	s := newTestStore(t)
	p, _, _ := s.CreateIfAbsent(CreateInput{Title: "X", Start: "2026-07-02", AllDay: true, Source: "s:x"})
	if _, err := s.Decide(p.ID, StatusRejected, ""); err != nil {
		t.Fatalf("reject: %v", err)
	}
	// same Source again must NOT create a new pending proposal
	_, created, _ := s.CreateIfAbsent(CreateInput{Title: "X", Start: "2026-07-02", AllDay: true, Source: "s:x"})
	if created {
		t.Error("a rejected proposal should not be re-proposed")
	}
	if pending, _ := s.ListPending(); len(pending) != 0 {
		t.Errorf("want 0 pending, got %d", len(pending))
	}
}

func TestPersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "calendar_proposals.json")
	s1, _ := New(path)
	s1.CreateIfAbsent(CreateInput{Title: "P", Start: "2026-07-02", AllDay: true, Source: "s:p"})

	s2, _ := New(path) // fresh store, same file
	if _, created, _ := s2.CreateIfAbsent(CreateInput{Title: "P", Start: "2026-07-02", AllDay: true, Source: "s:p"}); created {
		t.Error("persisted Source should dedup across reload")
	}
}

func TestCreateIfAbsent_PrunesOldTerminal(t *testing.T) {
	s := newTestStore(t)
	orig := nowMs
	defer func() { nowMs = orig }()
	base := int64(1_700_000_000_000)
	nowMs = func() int64 { return base }

	p, _, _ := s.CreateIfAbsent(CreateInput{Title: "old", Start: "2026-07-01", AllDay: true, Source: "s:old"})
	if _, err := s.Decide(p.ID, StatusRejected, ""); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	// Advance past the retention window, then a new create triggers the prune.
	nowMs = func() int64 { return base + terminalRetention.Milliseconds() + 1 }
	s.CreateIfAbsent(CreateInput{Title: "new", Start: "2026-07-02", AllDay: true, Source: "s:new"})

	// The pruned terminal's Source is forgotten → re-proposable.
	_, created, _ := s.CreateIfAbsent(CreateInput{Title: "old again", Start: "2026-07-01", AllDay: true, Source: "s:old"})
	if !created {
		t.Error("a long-decided proposal's Source should be re-proposable after prune")
	}
}

func TestCreateIfAbsent_KeepsRecentTerminal(t *testing.T) {
	s := newTestStore(t)
	p, _, _ := s.CreateIfAbsent(CreateInput{Title: "r", Start: "2026-07-01", AllDay: true, Source: "s:r"})
	s.Decide(p.ID, StatusRejected, "")
	// A new create runs the prune, but a recently-decided terminal must stay.
	s.CreateIfAbsent(CreateInput{Title: "other", Start: "2026-07-02", AllDay: true, Source: "s:other"})
	if _, created, _ := s.CreateIfAbsent(CreateInput{Title: "r2", Start: "2026-07-01", AllDay: true, Source: "s:r"}); created {
		t.Error("a recently-rejected proposal should still dedup (not be re-proposed)")
	}
}
