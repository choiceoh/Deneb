package localcal

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func breakCalendarPersistence(t *testing.T, s *Store) func() {
	t.Helper()
	original := s.path
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "calendar.json")
	return func() { s.path = original }
}

func eventInput(summary string, hour int) CreateInput {
	return CreateInput{
		Summary: summary,
		Start:   time.Date(2026, 7, 11, hour, 0, 0, 0, time.UTC),
	}
}

func TestCreatePersistenceFailureRollsBackMemoryAndNotification(t *testing.T) {
	s := newTestStore(t)
	var notified []string
	s.SetChangeObserver(func(id string) { notified = append(notified, id) })
	restore := breakCalendarPersistence(t, s)

	if _, err := s.Create(eventInput("must fail", 9)); err == nil {
		t.Fatal("Create succeeded with an unusable persistence path")
	}
	if got := s.ListRange(time.Time{}, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("failed create leaked into memory: %+v", got)
	}
	if len(notified) != 0 {
		t.Fatalf("failed create notified observer: %#v", notified)
	}

	restore()
	if _, err := s.Create(eventInput("retry", 9)); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	if len(notified) != 1 {
		t.Fatalf("successful retry notifications = %#v", notified)
	}
}

func TestUpdatePersistenceFailureRestoresPreviousEvent(t *testing.T) {
	s := newTestStore(t)
	original, err := s.Create(CreateInput{
		Summary:     "original",
		Description: "kept",
		Location:    "Seoul",
		Start:       time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
		Source:      "mail:one",
		SourceLabel: "source label",
		Kind:        "meeting",
		Docs:        []string{"contract.pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var notified []string
	s.SetChangeObserver(func(id string) { notified = append(notified, id) })
	restore := breakCalendarPersistence(t, s)
	defer restore()

	if _, err := s.Update(original.ID, CreateInput{Summary: "changed", Start: original.Start.Add(time.Hour)}); err == nil {
		t.Fatal("Update succeeded with an unusable persistence path")
	}
	got := s.Get(original.ID)
	if got == nil || got.Summary != "original" || got.Description != "kept" || got.Location != "Seoul" {
		t.Fatalf("failed update corrupted event: %+v", got)
	}
	if got.Source != "mail:one" || !reflect.DeepEqual(got.Docs, []string{"contract.pdf"}) {
		t.Fatalf("failed update corrupted provenance: %+v", got)
	}
	if len(notified) != 0 {
		t.Fatalf("failed update notified observer: %#v", notified)
	}
}

func TestDeletePersistenceFailureRestoresOrderingAndEvent(t *testing.T) {
	s := newTestStore(t)
	first, _ := s.Create(eventInput("first", 8))
	second, _ := s.Create(eventInput("second", 9))
	third, _ := s.Create(eventInput("third", 10))
	var notified []string
	s.SetChangeObserver(func(id string) { notified = append(notified, id) })
	restore := breakCalendarPersistence(t, s)
	defer restore()

	if err := s.Delete(second.ID); err == nil {
		t.Fatal("Delete succeeded with an unusable persistence path")
	}
	if s.Get(second.ID) == nil {
		t.Fatal("failed delete removed event from memory")
	}
	got := s.ListRange(first.Start.Add(-time.Hour), third.End.Add(time.Hour))
	if len(got) != 3 || got[0].ID != first.ID || got[1].ID != second.ID || got[2].ID != third.ID {
		t.Fatalf("rollback order = %+v", got)
	}
	if len(notified) != 0 {
		t.Fatalf("failed delete notified observer: %#v", notified)
	}
}

func TestListRangeInstantAndHalfOpenBoundaries(t *testing.T) {
	s := newTestStore(t)
	from := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	s.events = []storedEvent{
		{ID: "at-from", Summary: "at from", Start: from.Format(time.RFC3339)},
		{ID: "inside", Summary: "inside", Start: from.Add(30 * time.Minute).Format(time.RFC3339)},
		{ID: "at-to", Summary: "at to", Start: to.Format(time.RFC3339)},
		{ID: "ends-at-from", Summary: "ended", Start: from.Add(-time.Hour).Format(time.RFC3339), End: from.Format(time.RFC3339)},
		{ID: "overlap", Summary: "overlap", Start: from.Add(-time.Hour).Format(time.RFC3339), End: from.Add(time.Minute).Format(time.RFC3339)},
		{ID: "invalid", Summary: "bad", Start: "not-a-time"},
	}

	got := s.ListRange(from, to)
	ids := make([]string, 0, len(got))
	for _, ev := range got {
		ids = append(ids, ev.ID)
	}
	want := []string{"overlap", "at-from", "inside"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("range ids = %#v, want %#v", ids, want)
	}
}

func TestAllDayDefaultUsesNextLocalCalendarDayAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	start := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	rec := buildRecord("local:dst", CreateInput{Summary: "DST day", Start: start, AllDay: true})
	end, err := time.Parse(time.RFC3339, rec.End)
	if err != nil {
		t.Fatal(err)
	}
	if end.Hour() != 0 || end.Day() != 9 {
		t.Fatalf("all-day end = %v, want next local midnight", end)
	}
	if elapsed := end.Sub(start); elapsed != 23*time.Hour {
		t.Fatalf("spring-forward all-day duration = %v, want 23h", elapsed)
	}
}

func TestCreateAndGetDefensivelyCopyDocumentSlices(t *testing.T) {
	s := newTestStore(t)
	docs := []string{"quote.pdf", "contract.docx"}
	ev, err := s.Create(CreateInput{Summary: "meeting", Start: time.Now(), Docs: docs})
	if err != nil {
		t.Fatal(err)
	}
	docs[0] = "mutated-input.pdf"
	ev.Docs[1] = "mutated-return.docx"
	got := s.Get(ev.ID)
	if !reflect.DeepEqual(got.Docs, []string{"quote.pdf", "contract.docx"}) {
		t.Fatalf("stored docs aliased create input/result: %#v", got.Docs)
	}
	got.Docs[0] = "mutated-get.pdf"
	if again := s.Get(ev.ID); !reflect.DeepEqual(again.Docs, []string{"quote.pdf", "contract.docx"}) {
		t.Fatalf("Get returned aliased docs: %#v", again.Docs)
	}
}

func TestBuildRecordNormalizationAndDefaultEnds(t *testing.T) {
	start := time.Date(2026, 7, 11, 10, 0, 0, 123, time.FixedZone("custom", 9*3600))
	for _, tt := range []struct {
		name    string
		in      CreateInput
		wantEnd time.Time
	}{
		{name: "missing timed end", in: CreateInput{Summary: " x ", Start: start}, wantEnd: start.Add(time.Hour)},
		{name: "equal timed end", in: CreateInput{Summary: "x", Start: start, End: start}, wantEnd: start.Add(time.Hour)},
		{name: "backwards timed end", in: CreateInput{Summary: "x", Start: start, End: start.Add(-time.Hour)}, wantEnd: start.Add(time.Hour)},
		{name: "explicit future end", in: CreateInput{Summary: "x", Start: start, End: start.Add(2 * time.Hour)}, wantEnd: start.Add(2 * time.Hour)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := buildRecord("local:id", tt.in)
			if rec.Summary != "x" {
				t.Fatalf("summary = %q", rec.Summary)
			}
			end, err := time.Parse(time.RFC3339, rec.End)
			if err != nil || !end.Equal(tt.wantEnd.Truncate(time.Second)) {
				t.Fatalf("end = %v/%v, want %v", end, err, tt.wantEnd)
			}
		})
	}
}

func TestUpdatePreservesIndividualProvenanceFieldsAndCopiesDocs(t *testing.T) {
	s := newTestStore(t)
	ev, err := s.Create(CreateInput{
		Summary: "original", Start: time.Now(), Source: "mail:1", SourceLabel: "label", Kind: "meeting", Docs: []string{"a.pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	docs := []string{"b.pdf"}
	updated, err := s.Update(ev.ID, CreateInput{
		Summary: "updated", Start: ev.Start.Add(time.Hour), SourceLabel: "new label", Docs: docs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Source != "mail:1" || updated.SourceLabel != "new label" || updated.Kind != "meeting" {
		t.Fatalf("mixed provenance update = %+v", updated)
	}
	docs[0] = "mutated.pdf"
	updated.Docs[0] = "also-mutated.pdf"
	if got := s.Get(ev.ID); !reflect.DeepEqual(got.Docs, []string{"b.pdf"}) {
		t.Fatalf("updated docs aliased caller: %#v", got.Docs)
	}
}

func TestObserverCanReadStoreAndSwapItself(t *testing.T) {
	s := newTestStore(t)
	var mu sync.Mutex
	var observed []string
	s.SetChangeObserver(func(id string) {
		if s.Get(id) == nil {
			t.Errorf("observer could not read created event %q", id)
		}
		mu.Lock()
		observed = append(observed, id)
		mu.Unlock()
		s.SetChangeObserver(nil)
	})
	ev, err := s.Create(eventInput("readable", 11))
	if err != nil {
		t.Fatal(err)
	}
	if got := observed; !reflect.DeepEqual(got, []string{ev.ID}) {
		t.Fatalf("observed = %#v", got)
	}
	if _, err := s.Update(ev.ID, eventInput("no second callback", 12)); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 {
		t.Fatalf("observer fired after removal: %#v", observed)
	}
}

func TestConcurrentCreateProducesUniqueIDsAndDurableEvents(t *testing.T) {
	s := newTestStore(t)
	const count = 40
	var wg sync.WaitGroup
	ids := make(chan string, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev, err := s.Create(eventInput("concurrent", 1+i%20))
			if err != nil {
				errs <- err
				return
			}
			ids <- ev.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent create: %v", err)
	}
	unique := make(map[string]struct{}, count)
	for id := range ids {
		if !strings.HasPrefix(id, IDPrefix) {
			t.Fatalf("id = %q", id)
		}
		unique[id] = struct{}{}
	}
	if len(unique) != count {
		t.Fatalf("unique ids = %d, want %d", len(unique), count)
	}
	reloaded, err := New(s.path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.ListRange(time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
	if len(got) != count {
		t.Fatalf("durable events = %d, want %d", len(got), count)
	}
}

func TestNewRejectsEmptyAndCorruptStores(t *testing.T) {
	for _, path := range []string{"", " ", "\n\t"} {
		if _, err := New(path); err == nil || !strings.Contains(err.Error(), "empty path") {
			t.Errorf("New(%q) error = %v", path, err)
		}
	}
	path := filepath.Join(t.TempDir(), "calendar.json")
	if err := os.WriteFile(path, []byte(`{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path); err == nil {
		t.Fatal("corrupt calendar store was accepted")
	}
}

func TestMutationValidationLeavesStoreUntouched(t *testing.T) {
	s := newTestStore(t)
	ev, err := s.Create(eventInput("valid", 9))
	if err != nil {
		t.Fatal(err)
	}
	var notified []string
	s.SetChangeObserver(func(id string) { notified = append(notified, id) })
	invalid := []CreateInput{
		{Summary: " ", Start: time.Now()},
		{Summary: "missing start"},
	}
	for _, in := range invalid {
		if _, err := s.Create(in); err == nil {
			t.Errorf("invalid create accepted: %+v", in)
		}
		if _, err := s.Update(ev.ID, in); err == nil {
			t.Errorf("invalid update accepted: %+v", in)
		}
	}
	if got := s.Get(ev.ID); got == nil || got.Summary != "valid" {
		t.Fatalf("validation changed event: %+v", got)
	}
	if len(notified) != 0 {
		t.Fatalf("validation failure notified: %#v", notified)
	}
	if err := s.Delete("local:missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing delete = %v", err)
	}
}
