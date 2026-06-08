package localcal

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "calendar.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestCreateGetDelete(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	ev, err := s.Create(CreateInput{Summary: "회의", Location: "본사", Start: start})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !IsLocalID(ev.ID) {
		t.Errorf("id %q lacks local prefix", ev.ID)
	}
	// Timed event with no end → default +1h.
	if !ev.End.Equal(start.Add(time.Hour)) {
		t.Errorf("end = %v, want +1h", ev.End)
	}
	if got := s.Get(ev.ID); got == nil || got.Summary != "회의" {
		t.Fatalf("Get = %+v", got)
	}
	if err := s.Delete(ev.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Get(ev.ID) != nil {
		t.Error("event present after delete")
	}
}

func TestCreateAllDayDefaultsToNextDayEnd(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	ev, err := s.Create(CreateInput{Summary: "워크숍", Start: start, AllDay: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !ev.AllDay {
		t.Error("AllDay not set")
	}
	if !ev.End.Equal(start.Add(24 * time.Hour)) {
		t.Errorf("all-day end = %v, want +24h", ev.End)
	}
}

func TestCreateValidates(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(CreateInput{Start: time.Now()}); err == nil {
		t.Error("expected error for blank summary")
	}
	if _, err := s.Create(CreateInput{Summary: "x"}); err == nil {
		t.Error("expected error for zero start")
	}
}

func TestListRangeStartInWindowSorted(t *testing.T) {
	s := newTestStore(t)
	mk := func(sum string, day int) {
		if _, err := s.Create(CreateInput{Summary: sum, Start: time.Date(2026, 6, day, 9, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatalf("seed %s: %v", sum, err)
		}
	}
	mk("c", 20)  // after window
	mk("a", 5)   // in window
	mk("b", 10)  // in window
	mk("out", 1) // June 1 — before window

	from := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	got := s.ListRange(from, to)
	if len(got) != 2 {
		t.Fatalf("range returned %d, want 2 (a,b)", len(got))
	}
	if got[0].Summary != "a" || got[1].Summary != "b" {
		t.Errorf("not sorted by start: %q, %q", got[0].Summary, got[1].Summary)
	}
}

func TestUpdatePreservesCreatedAndChangesFields(t *testing.T) {
	s := newTestStore(t)
	ev, err := s.Create(CreateInput{Summary: "원래", Start: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := s.Update(ev.ID, CreateInput{Summary: "수정", Location: "강남", Start: time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Summary != "수정" || updated.Location != "강남" {
		t.Errorf("update = %+v", updated)
	}
	if got := s.Get(ev.ID); got == nil || got.Summary != "수정" {
		t.Errorf("store not updated: %+v", got)
	}
}

func TestUpdateDeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Update("local:nope", CreateInput{Summary: "x", Start: time.Now()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing = %v, want ErrNotFound", err)
	}
	if err := s.Delete("local:nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing = %v, want ErrNotFound", err)
	}
}

// Persistence round-trips: a second store reading the same file sees the events.
func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.json")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s1.Create(CreateInput{Summary: "지속", Start: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	s2, err := New(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := s2.ListRange(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC))
	if len(got) != 1 || got[0].Summary != "지속" {
		t.Fatalf("reloaded = %+v", got)
	}
}

func TestIsLocalID(t *testing.T) {
	if !IsLocalID("local:123") {
		t.Error("local: prefix should be local")
	}
	if IsLocalID("google-abc") {
		t.Error("google id should not be local")
	}
}
