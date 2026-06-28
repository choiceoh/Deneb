package code

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return func() time.Time { return t }
}

// newTestStore returns a store rooted in a temp dir with a deterministic clock.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	st.now = fixedClock()
	return st
}

func sampleSession() *Session {
	return NewSession(
		Task{ID: "fix-login", Repo: Repo{Owner: "acme", Name: "app"}, Branch: "deneb/fix-login", Dir: "/wt/fix-login"},
		"로그인 수정", "chat:code-fix-login",
	)
}

func TestStore_AddGetList(t *testing.T) {
	st := newTestStore(t)
	if err := st.Add(sampleSession()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := st.Add(sampleSession()); err == nil {
		t.Errorf("duplicate Add should fail")
	}

	got, ok := st.Get("fix-login")
	if !ok {
		t.Fatal("Get: session not found")
	}
	if got.Title != "로그인 수정" || got.Status != StatusWorking {
		t.Errorf("got %+v, want title=로그인 수정 status=working", got)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("timestamps not stamped: %+v", got)
	}
	if list := st.List(); len(list) != 1 || list[0].ID != "fix-login" {
		t.Errorf("List = %+v, want one fix-login", list)
	}
}

func TestStore_PersistAcrossReload(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	st.now = fixedClock()
	if err := st.Add(sampleSession()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A fresh store over the same root must see the persisted session.
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.Get("fix-login")
	if !ok || got.Branch != "deneb/fix-login" || got.Repo.Owner != "acme" {
		t.Errorf("reload lost data: %+v ok=%v", got, ok)
	}
	if path := filepath.Join(root, "sessions.json"); reopened.path != path {
		t.Errorf("store path = %q, want %q", reopened.path, path)
	}
}

func TestStore_StatusTitleCheckpoint(t *testing.T) {
	st := newTestStore(t)
	if err := st.Add(sampleSession()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := st.SetStatus("fix-login", StatusPassed); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if err := st.SetTitle("fix-login", "로그인 버튼 정렬"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	if err := st.AddCheckpoint("fix-login", Checkpoint{SHA: "abc123", Summary: "버튼 정렬 고침", At: "2026-01-02T03:04:05Z"}); err != nil {
		t.Fatalf("AddCheckpoint: %v", err)
	}

	got, _ := st.Get("fix-login")
	if got.Status != StatusPassed || got.Title != "로그인 버튼 정렬" {
		t.Errorf("got status=%q title=%q", got.Status, got.Title)
	}
	if len(got.Checkpoints) != 1 || got.Checkpoints[0].SHA != "abc123" {
		t.Errorf("checkpoints = %+v", got.Checkpoints)
	}

	if err := st.SetStatus("missing-id", StatusPassed); err == nil {
		t.Errorf("SetStatus on unknown id should fail")
	}
}

func TestStore_Reconcile(t *testing.T) {
	st := newTestStore(t)
	if err := st.Add(sampleSession()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Worktree dir reported gone → status flips to missing.
	if err := st.Reconcile(func(string) bool { return false }); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got, _ := st.Get("fix-login"); got.Status != StatusMissing {
		t.Errorf("status = %q, want missing", got.Status)
	}
	// Present worktree → status untouched.
	st2 := newTestStore(t)
	_ = st2.Add(sampleSession())
	_ = st2.Reconcile(func(string) bool { return true })
	if got, _ := st2.Get("fix-login"); got.Status != StatusWorking {
		t.Errorf("status = %q, want working (present)", got.Status)
	}
}

func TestStore_Delete(t *testing.T) {
	st := newTestStore(t)
	_ = st.Add(sampleSession())
	if err := st.Delete("fix-login"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := st.Get("fix-login"); ok {
		t.Errorf("session still present after Delete")
	}
	if err := st.Delete("fix-login"); err == nil {
		t.Errorf("Delete of unknown id should fail")
	}
}

func TestStore_PopCheckpoint(t *testing.T) {
	st := newTestStore(t)
	if err := st.Add(sampleSession()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_ = st.AddCheckpoint("fix-login", Checkpoint{SHA: "a"})
	_ = st.AddCheckpoint("fix-login", Checkpoint{SHA: "b"})

	if err := st.PopCheckpoint("fix-login"); err != nil {
		t.Fatalf("PopCheckpoint: %v", err)
	}
	got, _ := st.Get("fix-login")
	if len(got.Checkpoints) != 1 || got.Checkpoints[0].SHA != "a" {
		t.Errorf("after pop, checkpoints = %+v", got.Checkpoints)
	}

	// Pop down to empty, then once more — a no-op, not an error (undo of
	// uncommitted-only edits has no checkpoint to drop).
	_ = st.PopCheckpoint("fix-login")
	if err := st.PopCheckpoint("fix-login"); err != nil {
		t.Errorf("pop on empty should be a no-op, got %v", err)
	}
}

func TestStore_ToleratesCorruptFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A corrupt file must not disable coding mode — start empty, preserve the bad file.
	st, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore should tolerate corrupt file, got %v", err)
	}
	if len(st.List()) != 0 {
		t.Errorf("corrupt store should start empty, got %d", len(st.List()))
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Errorf("corrupt file should be preserved as .corrupt: %v", err)
	}
}
