package localtodo

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

func breakTodoPersistence(t *testing.T, s *Store) func() {
	t.Helper()
	original := s.path
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "todos.json")
	return func() { s.path = original }
}

func TestCreatePersistenceFailureRollsBackTodoMemory(t *testing.T) {
	s := newTestStore(t)
	restore := breakTodoPersistence(t, s)
	if _, err := s.Create(CreateInput{Title: "must fail"}); err == nil {
		t.Fatal("Create succeeded with an unusable persistence path")
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("failed create leaked into memory: %+v", got)
	}
	restore()
	if _, err := s.Create(CreateInput{Title: "retry"}); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	if got := s.List(); len(got) != 1 || got[0].Title != "retry" {
		t.Fatalf("retry result = %+v", got)
	}
}

func TestCreateIfAbsentPersistenceFailureRollsBackDedupState(t *testing.T) {
	s := newTestStore(t)
	in := CreateInput{Title: "automated", Source: " mail:one|automated "}
	restore := breakTodoPersistence(t, s)
	if _, created, err := s.CreateIfAbsent(in); err == nil || created {
		t.Fatalf("CreateIfAbsent = created=%v err=%v", created, err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("failed idempotent create leaked into memory: %+v", got)
	}
	restore()
	td, created, err := s.CreateIfAbsent(in)
	if err != nil || !created || td.Source != "mail:one|automated" {
		t.Fatalf("retry = %+v created=%v err=%v", td, created, err)
	}
	duplicate, created, err := s.CreateIfAbsent(CreateInput{Title: "different title", Source: "mail:one|automated"})
	if err != nil || created || duplicate.ID != td.ID {
		t.Fatalf("dedup = %+v created=%v err=%v", duplicate, created, err)
	}
}

func TestUpdatePersistenceFailureRestoresAllTodoFields(t *testing.T) {
	s := newTestStore(t)
	due := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	original, err := s.Create(CreateInput{Title: "original", Note: "keep", Due: due, DueAllDay: true, Source: "mail:one"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.SetDone(original.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	restore := breakTodoPersistence(t, s)
	defer restore()

	if _, err := s.Update(original.ID, CreateInput{Title: "changed", Note: "changed", Source: "mail:two"}); err == nil {
		t.Fatal("Update succeeded with an unusable persistence path")
	}
	got := s.Get(original.ID)
	if got == nil || got.Title != "original" || got.Note != "keep" || !got.Due.Equal(due) || !got.DueAllDay {
		t.Fatalf("failed update corrupted editable fields: %+v", got)
	}
	if !got.Done || !got.DoneAt.Equal(done.DoneAt) || got.Source != "mail:one" {
		t.Fatalf("failed update corrupted lifecycle/provenance: %+v", got)
	}
}

func TestSetDonePersistenceFailureRestoresCompletionState(t *testing.T) {
	s := newTestStore(t)
	td, _ := s.Create(CreateInput{Title: "task"})
	restore := breakTodoPersistence(t, s)
	if _, err := s.SetDone(td.ID, true); err == nil {
		t.Fatal("SetDone succeeded with an unusable persistence path")
	}
	got := s.Get(td.ID)
	if got == nil || got.Done || !got.DoneAt.IsZero() {
		t.Fatalf("failed SetDone corrupted state: %+v", got)
	}
	restore()
	done, err := s.SetDone(td.ID, true)
	if err != nil || !done.Done || done.DoneAt.IsZero() {
		t.Fatalf("retry SetDone = %+v/%v", done, err)
	}
}

func TestDeletePersistenceFailureRestoresTodoAndOrder(t *testing.T) {
	s := newTestStore(t)
	first, _ := s.Create(CreateInput{Title: "first", Due: time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)})
	second, _ := s.Create(CreateInput{Title: "second", Due: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)})
	third, _ := s.Create(CreateInput{Title: "third", Due: time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)})
	restore := breakTodoPersistence(t, s)
	defer restore()
	if err := s.Delete(second.ID); err == nil {
		t.Fatal("Delete succeeded with an unusable persistence path")
	}
	got := s.List()
	if len(got) != 3 || got[0].ID != first.ID || got[1].ID != second.ID || got[2].ID != third.ID {
		t.Fatalf("rollback order = %+v", got)
	}
}

func TestUpdatePreservesSourceUnlessExplicitlyOverridden(t *testing.T) {
	s := newTestStore(t)
	td, err := s.Create(CreateInput{Title: "from analysis", Source: "mail:one|action"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.Update(td.ID, CreateInput{Title: "edited by user", Note: "details"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Source != "mail:one|action" {
		t.Fatalf("ordinary update dropped dedup source: %+v", updated)
	}
	duplicate, created, err := s.CreateIfAbsent(CreateInput{Title: "re-analysis", Source: "mail:one|action"})
	if err != nil || created || duplicate.ID != td.ID {
		t.Fatalf("source preservation did not dedup: %+v created=%v err=%v", duplicate, created, err)
	}
	overridden, err := s.Update(td.ID, CreateInput{Title: "migrated", Source: "mail:two|action"})
	if err != nil || overridden.Source != "mail:two|action" {
		t.Fatalf("explicit source override = %+v/%v", overridden, err)
	}
}

func TestSetDoneIsIdempotentAndKeepsOriginalCompletionTime(t *testing.T) {
	s := newTestStore(t)
	td, _ := s.Create(CreateInput{Title: "task"})
	first, err := s.SetDone(td.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.DoneAt.IsZero() {
		t.Fatal("first completion did not set DoneAt")
	}
	// A repeated delivery of the same command must not rewrite the historical
	// completion timestamp or Updated time.
	time.Sleep(1100 * time.Millisecond)
	second, err := s.SetDone(td.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !second.DoneAt.Equal(first.DoneAt) || !second.Updated.Equal(first.Updated) {
		t.Fatalf("idempotent SetDone changed timestamps: first=%+v second=%+v", first, second)
	}
	undone, err := s.SetDone(td.ID, false)
	if err != nil || undone.Done || !undone.DoneAt.IsZero() {
		t.Fatalf("undo = %+v/%v", undone, err)
	}
	again, err := s.SetDone(td.ID, false)
	if err != nil || !again.Updated.Equal(undone.Updated) {
		t.Fatalf("idempotent undo = %+v/%v", again, err)
	}
}

func TestConcurrentCreateIfAbsentHasSingleWinner(t *testing.T) {
	s := newTestStore(t)
	const callers = 64
	type result struct {
		td      Todo
		created bool
		err     error
	}
	results := make(chan result, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			td, created, err := s.CreateIfAbsent(CreateInput{Title: "shared", Source: "mail:shared|action"})
			results <- result{td: td, created: created, err: err}
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	ids := make(map[string]struct{})
	for r := range results {
		if r.err != nil {
			t.Fatalf("CreateIfAbsent: %v", r.err)
		}
		if r.created {
			winners++
		}
		ids[r.td.ID] = struct{}{}
	}
	if winners != 1 || len(ids) != 1 {
		t.Fatalf("winners=%d ids=%#v", winners, ids)
	}
	if got := s.List(); len(got) != 1 || got[0].Source != "mail:shared|action" {
		t.Fatalf("stored todos = %+v", got)
	}
	reloaded, err := New(s.path)
	if err != nil || len(reloaded.List()) != 1 {
		t.Fatalf("durable todos = %+v/%v", reloaded, err)
	}
}

func TestConcurrentCreatesProduceUniqueTodoIDs(t *testing.T) {
	s := newTestStore(t)
	const count = 40
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			td, err := s.Create(CreateInput{Title: "task", Due: time.Date(2026, 7, 11, i%24, 0, 0, 0, time.UTC)})
			if err != nil {
				errs <- err
				return
			}
			ids <- td.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("Create: %v", err)
	}
	unique := make(map[string]struct{})
	for id := range ids {
		if !IsTodoID(id) {
			t.Fatalf("id = %q", id)
		}
		unique[id] = struct{}{}
	}
	if len(unique) != count || len(s.List()) != count {
		t.Fatalf("unique/stored = %d/%d, want %d", len(unique), len(s.List()), count)
	}
}

func TestListAndRangeBoundaryOrderingContract(t *testing.T) {
	s := newTestStore(t)
	from := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	atFrom, _ := s.Create(CreateInput{Title: "at-from", Due: from})
	inside, _ := s.Create(CreateInput{Title: "inside", Due: from.Add(30 * time.Minute)})
	_, _ = s.Create(CreateInput{Title: "at-to", Due: to})
	_, _ = s.Create(CreateInput{Title: "before", Due: from.Add(-time.Nanosecond)})
	undated, _ := s.Create(CreateInput{Title: "undated"})
	done, _ := s.Create(CreateInput{Title: "done-early", Due: from.Add(-time.Hour)})
	if _, err := s.SetDone(done.ID, true); err != nil {
		t.Fatal(err)
	}

	ranged := s.ListRange(from, to)
	if len(ranged) != 2 || ranged[0].ID != atFrom.ID || ranged[1].ID != inside.ID {
		t.Fatalf("range = %+v", ranged)
	}
	all := s.List()
	if all[len(all)-1].ID != done.ID {
		t.Fatalf("done todo not last: %+v", all)
	}
	undatedIndex := -1
	for i, td := range all {
		if td.ID == undated.ID {
			undatedIndex = i
		}
	}
	if undatedIndex < 0 || undatedIndex != len(all)-2 {
		t.Fatalf("undated incomplete position = %d in %+v", undatedIndex, all)
	}
}

func TestBuildRecordNormalizesAndRoundTripsFields(t *testing.T) {
	due := time.Date(2026, 7, 11, 17, 30, 0, 123, time.FixedZone("KST", 9*3600))
	rec := buildRecord("todo:id", CreateInput{
		Title:     "  review contract  ",
		Note:      "  exact terms  ",
		Due:       due,
		DueAllDay: true,
		Source:    "  mail:m1|review  ",
	})
	if rec.Title != "review contract" || rec.Note != "exact terms" || rec.Source != "mail:m1|review" {
		t.Fatalf("normalized record = %+v", rec)
	}
	td := rec.toTodo()
	if td.ID != "todo:id" || !td.Due.Equal(due.Truncate(time.Second)) || !td.DueAllDay || td.Done {
		t.Fatalf("round trip = %+v", td)
	}
	if td.Created.IsZero() || td.Updated.IsZero() {
		t.Fatalf("missing timestamps = %+v", td)
	}

	undated := buildRecord("todo:none", CreateInput{Title: "undated"}).toTodo()
	if !undated.Due.IsZero() {
		t.Fatalf("undated due = %v", undated.Due)
	}
}

func TestMalformedPersistedTimesDegradeToZero(t *testing.T) {
	st := storedTodo{
		ID:      "todo:bad",
		Title:   "bad times",
		Due:     "tomorrow",
		DoneAt:  "yesterday",
		Created: "never",
		Updated: "soon",
	}
	td := st.toTodo()
	if !td.Due.IsZero() || !td.DoneAt.IsZero() || !td.Created.IsZero() || !td.Updated.IsZero() {
		t.Fatalf("invalid times did not degrade to zero: %+v", td)
	}
}

func TestNewRejectsEmptyAndCorruptTodoStores(t *testing.T) {
	for _, path := range []string{"", " ", "\n\t"} {
		if _, err := New(path); err == nil || !strings.Contains(err.Error(), "empty path") {
			t.Errorf("New(%q) error = %v", path, err)
		}
	}
	path := filepath.Join(t.TempDir(), "todos.json")
	if err := os.WriteFile(path, []byte(`[{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path); err == nil {
		t.Fatal("corrupt todo store was accepted")
	}
}

func TestValidationFailuresLeaveTodoUntouched(t *testing.T) {
	s := newTestStore(t)
	td, err := s.Create(CreateInput{Title: "valid", Source: "mail:one"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := []CreateInput{{}, {Title: " "}, {Title: "\n\t"}}
	for _, in := range invalid {
		if _, err := s.Create(in); err == nil {
			t.Errorf("invalid create accepted: %+v", in)
		}
		if _, _, err := s.CreateIfAbsent(in); err == nil {
			t.Errorf("invalid idempotent create accepted: %+v", in)
		}
		if _, err := s.Update(td.ID, in); err == nil {
			t.Errorf("invalid update accepted: %+v", in)
		}
	}
	if got := s.Get(td.ID); got == nil || got.Title != "valid" || got.Source != "mail:one" {
		t.Fatalf("validation changed todo: %+v", got)
	}
	if _, err := s.Update("todo:missing", CreateInput{Title: "valid"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update = %v", err)
	}
	if _, err := s.SetDone("todo:missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing completion = %v", err)
	}
	if err := s.Delete("todo:missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing delete = %v", err)
	}
}

func TestSortTodosStableTieBreaks(t *testing.T) {
	created := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	due := created.Add(24 * time.Hour)
	in := []Todo{
		{ID: "undated-new", Created: created.Add(time.Hour)},
		{ID: "due-new", Due: due, Created: created.Add(time.Hour)},
		{ID: "done", Due: due.Add(-time.Hour), Done: true, Created: created.Add(-time.Hour)},
		{ID: "due-old", Due: due, Created: created},
		{ID: "undated-old", Created: created},
	}
	sortTodos(in)
	got := make([]string, 0, len(in))
	for _, td := range in {
		got = append(got, td.ID)
	}
	want := []string{"due-old", "due-new", "undated-old", "undated-new", "done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sort = %#v, want %#v", got, want)
	}
}
