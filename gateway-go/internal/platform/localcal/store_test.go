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

func TestDocsRoundTripAndPreservedAcrossUpdate(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	ev, err := s.Create(CreateInput{Summary: "ZTT 미팅", Start: start, Docs: []string{"견적서.pdf", "계약서.docx"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(ev.Docs) != 2 || ev.Docs[0] != "견적서.pdf" {
		t.Fatalf("create dropped docs: %+v", ev.Docs)
	}
	// A normal edit (no docs in input) preserves them.
	up, err := s.Update(ev.ID, CreateInput{Summary: "ZTT 미팅 (변경)", Start: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(up.Docs) != 2 || up.Docs[1] != "계약서.docx" {
		t.Errorf("update dropped docs: %+v", up.Docs)
	}
	// Survives a reload from disk.
	if s2, _ := New(s.path); s2.Get(ev.ID) == nil || len(s2.Get(ev.ID).Docs) != 2 {
		t.Error("docs lost across reload")
	}
}

func TestUpdatePreservesProvenance(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	ev, err := s.Create(CreateInput{
		Summary:     "ZTT 미팅",
		Start:       start,
		Source:      "mail:abc123",
		SourceLabel: "비금 154kV 통관",
		Kind:        "meeting",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A normal edit (time + title) carries no provenance — it must survive.
	newStart := start.Add(2 * time.Hour)
	updated, err := s.Update(ev.ID, CreateInput{Summary: "ZTT 미팅 (시간 변경)", Start: newStart})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Source != "mail:abc123" || updated.SourceLabel != "비금 154kV 통관" || updated.Kind != "meeting" {
		t.Fatalf("update dropped provenance: %+v", updated)
	}
	if updated.Summary != "ZTT 미팅 (시간 변경)" || !updated.Start.Equal(newStart) {
		t.Fatalf("update didn't apply the edit: %+v", updated)
	}
	// Survives a reload from disk.
	if s2, _ := New(s.path); s2.Get(ev.ID) == nil || s2.Get(ev.ID).Source != "mail:abc123" {
		t.Fatalf("provenance lost across reload")
	}
	// An explicitly supplied value still overrides, per field.
	over, err := s.Update(ev.ID, CreateInput{
		Summary: "ZTT 미팅", Start: newStart,
		Source: "mail:zzz", SourceLabel: "새 출처", Kind: "deadline",
	})
	if err != nil {
		t.Fatalf("Update override: %v", err)
	}
	if over.Source != "mail:zzz" || over.SourceLabel != "새 출처" || over.Kind != "deadline" {
		t.Errorf("explicit provenance override not applied: %+v", over)
	}
}

func TestCreatePreservesProvenance(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	ev, err := s.Create(CreateInput{
		Summary:     "ZTT 미팅",
		Start:       start,
		Source:      "mail:abc123",
		SourceLabel: "비금 154kV 통관",
		Kind:        "meeting",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ev.Source != "mail:abc123" || ev.SourceLabel != "비금 154kV 통관" || ev.Kind != "meeting" {
		t.Fatalf("provenance not returned on create: %+v", ev)
	}
	if got := s.Get(ev.ID); got == nil || got.Source != "mail:abc123" || got.Kind != "meeting" {
		t.Fatalf("Get lost provenance: %+v", got)
	}
	// Survives a reload from disk (the on-disk shape carries the fields).
	s2, err := New(s.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := s2.Get(ev.ID); got == nil || got.SourceLabel != "비금 154kV 통관" {
		t.Fatalf("provenance lost across reload: %+v", got)
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

// A multi-day event must appear for any window it overlaps, even one that opens
// after its start day — and must vanish once the window clears its end.
func TestListRangeIncludesOverlappingMultiDay(t *testing.T) {
	s := newTestStore(t)
	// All-day workshop spanning June 8 → June 10, stored with an exclusive end at
	// June 11 00:00 (the convention the native form and Google both use).
	start := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	if _, err := s.Create(CreateInput{Summary: "워크숍", Start: start, End: end, AllDay: true}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Window opens after the start day but still overlaps the span → included.
	got := s.ListRange(
		time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
	)
	if len(got) != 1 || got[0].Summary != "워크숍" {
		t.Fatalf("overlap window = %+v, want the multi-day workshop", got)
	}

	// Window starting at the exclusive end must NOT include it.
	none := s.ListRange(
		time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
	)
	if len(none) != 0 {
		t.Fatalf("post-event window = %+v, want empty", none)
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

// The change observer fires once per successful mutation with the event's ID, so
// the server can mirror it onto native-sync. A failed mutation (not-found) must
// not fire it.
func TestChangeObserverFires(t *testing.T) {
	s := newTestStore(t)
	var got []string
	s.SetChangeObserver(func(id string) { got = append(got, id) })

	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	ev, err := s.Create(CreateInput{Summary: "회의", Start: start})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Update(ev.ID, CreateInput{Summary: "회의 (변경)", Start: start}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := s.Delete(ev.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// A missing-target mutation must NOT notify.
	_ = s.Delete("local:nope")
	if _, err := s.Update("local:nope", CreateInput{Summary: "x", Start: start}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update missing = %v, want ErrNotFound", err)
	}

	want := []string{ev.ID, ev.ID, ev.ID}
	if len(got) != len(want) {
		t.Fatalf("observer fired %d times (%v), want %d (create/update/delete only)", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fire[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
