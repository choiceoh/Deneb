package localtodo

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "todos.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestCreateGetDelete(t *testing.T) {
	s := newTestStore(t)
	td, err := s.Create(CreateInput{Title: "보고서 작성", Note: "Q2 매출"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !IsTodoID(td.ID) {
		t.Errorf("id %q lacks todo prefix", td.ID)
	}
	if td.Done {
		t.Error("new todo should be incomplete")
	}
	if got := s.Get(td.ID); got == nil || got.Title != "보고서 작성" {
		t.Fatalf("Get = %+v", got)
	}
	if err := s.Delete(td.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Get(td.ID) != nil {
		t.Error("todo present after delete")
	}
}

func TestCreateValidates(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(CreateInput{Title: "  "}); err == nil {
		t.Error("expected error for blank title")
	}
}

func TestCreateWithoutDueIsUndated(t *testing.T) {
	s := newTestStore(t)
	td, err := s.Create(CreateInput{Title: "장보기"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !td.Due.IsZero() {
		t.Errorf("undated todo has Due = %v", td.Due)
	}
}

func TestSetDoneStampsAndClears(t *testing.T) {
	s := newTestStore(t)
	td, err := s.Create(CreateInput{Title: "전화하기"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	done, err := s.SetDone(td.ID, true)
	if err != nil {
		t.Fatalf("SetDone true: %v", err)
	}
	if !done.Done || done.DoneAt.IsZero() {
		t.Errorf("mark done = %+v, want Done with DoneAt", done)
	}
	undone, err := s.SetDone(td.ID, false)
	if err != nil {
		t.Fatalf("SetDone false: %v", err)
	}
	if undone.Done || !undone.DoneAt.IsZero() {
		t.Errorf("un-mark = %+v, want incomplete with cleared DoneAt", undone)
	}
}

func TestListOrdersIncompleteThenDueThenCreated(t *testing.T) {
	s := newTestStore(t)
	// Undated, incomplete.
	undated, _ := s.Create(CreateInput{Title: "undated"})
	// Dated later.
	late, _ := s.Create(CreateInput{Title: "late", Due: time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)})
	// Dated earlier — should sort first among incomplete.
	early, _ := s.Create(CreateInput{Title: "early", Due: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)})
	// Mark the earliest done — should fall to the bottom.
	if _, err := s.SetDone(early.ID, true); err != nil {
		t.Fatalf("SetDone: %v", err)
	}

	got := s.List()
	if len(got) != 3 {
		t.Fatalf("List len = %d, want 3", len(got))
	}
	// Incomplete dated (late) first, then incomplete undated, then done (early).
	if got[0].ID != late.ID {
		t.Errorf("got[0] = %q, want late", got[0].Title)
	}
	if got[1].ID != undated.ID {
		t.Errorf("got[1] = %q, want undated", got[1].Title)
	}
	if got[2].ID != early.ID || !got[2].Done {
		t.Errorf("got[2] = %+v, want done early at bottom", got[2])
	}
}

func TestListRangeOnlyDatedInWindow(t *testing.T) {
	s := newTestStore(t)
	mk := func(title string, day int) {
		if _, err := s.Create(CreateInput{Title: title, Due: time.Date(2026, 6, day, 9, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatalf("seed %s: %v", title, err)
		}
	}
	mk("in1", 5)
	mk("in2", 10)
	mk("out", 20)
	if _, err := s.Create(CreateInput{Title: "undated"}); err != nil {
		t.Fatalf("seed undated: %v", err)
	}

	got := s.ListRange(
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	)
	if len(got) != 2 {
		t.Fatalf("range returned %d, want 2 (in1,in2)", len(got))
	}
	if got[0].Title != "in1" || got[1].Title != "in2" {
		t.Errorf("not sorted by due: %q, %q", got[0].Title, got[1].Title)
	}
}

func TestUpdatePreservesDoneAndCreated(t *testing.T) {
	s := newTestStore(t)
	td, err := s.Create(CreateInput{Title: "원래"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.SetDone(td.ID, true); err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	updated, err := s.Update(td.ID, CreateInput{Title: "수정", Note: "메모", Due: time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "수정" || updated.Note != "메모" {
		t.Errorf("update = %+v", updated)
	}
	if !updated.Done || updated.DoneAt.IsZero() {
		t.Errorf("update dropped done state: %+v", updated)
	}
}

func TestUpdateSetDoneDeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Update("todo:nope", CreateInput{Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing = %v, want ErrNotFound", err)
	}
	if _, err := s.SetDone("todo:nope", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetDone missing = %v, want ErrNotFound", err)
	}
	if err := s.Delete("todo:nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing = %v, want ErrNotFound", err)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todos.json")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	td, err := s1.Create(CreateInput{Title: "지속", Due: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s1.SetDone(td.ID, true); err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	s2, err := New(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := s2.List()
	if len(got) != 1 || got[0].Title != "지속" || !got[0].Done {
		t.Fatalf("reloaded = %+v", got)
	}
}

func TestIsTodoID(t *testing.T) {
	if !IsTodoID("todo:123") {
		t.Error("todo: prefix should be a todo id")
	}
	if IsTodoID("local:abc") {
		t.Error("calendar id should not be a todo id")
	}
}
