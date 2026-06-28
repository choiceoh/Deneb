package code

import (
	"context"
	"strings"
	"testing"
)

// newTurnFixture builds a Go-kind worktree dir, a fake git runner, and a store
// holding one working session pointing at that dir.
func newTurnFixture(t *testing.T, taskID string, fake *fakeRunner) (*Manager, *Store) {
	t.Helper()
	dir := t.TempDir()
	writeMarker(t, dir, "go.mod") // detectKind → KindGo so verify runs go build/test
	m := &Manager{Root: t.TempDir(), DefaultBranch: "main", Runner: fake}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sess := NewSession(
		Task{ID: taskID, Repo: Repo{Owner: "acme", Name: "app"}, Branch: "deneb/" + taskID, Dir: dir},
		"제목", "code:"+taskID,
	)
	if err := store.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return m, store
}

func TestAfterTurn_DirtyCommitsAndPasses(t *testing.T) {
	fake := &fakeRunner{out: map[string][]byte{
		"status":    []byte("M main.go\n"), // dirty worktree
		"rev-parse": []byte("abc123\n"),    // HeadSHA
	}}
	m, store := newTurnFixture(t, "fix-login", fake)

	AfterTurn(context.Background(), m, store, "fix-login", "로그인 폼 추가", nil)

	got, _ := store.Get("fix-login")
	if got.Status != StatusPassed {
		t.Errorf("status = %q, want %q", got.Status, StatusPassed)
	}
	if len(got.Checkpoints) != 1 {
		t.Fatalf("checkpoints = %+v, want exactly one", got.Checkpoints)
	}
	if got.Checkpoints[0].SHA != "abc123" {
		t.Errorf("checkpoint SHA = %q, want abc123", got.Checkpoints[0].SHA)
	}
	if got.Checkpoints[0].Summary != "로그인 폼 추가" {
		t.Errorf("checkpoint summary = %q, want the turn message", got.Checkpoints[0].Summary)
	}
}

func TestAfterTurn_CleanTreeSkips(t *testing.T) {
	fake := &fakeRunner{} // status returns nil → clean tree
	m, store := newTurnFixture(t, "readonly", fake)

	AfterTurn(context.Background(), m, store, "readonly", "코드 설명해줘", nil)

	got, _ := store.Get("readonly")
	if got.Status != StatusWorking {
		t.Errorf("status = %q, want unchanged %q", got.Status, StatusWorking)
	}
	if len(got.Checkpoints) != 0 {
		t.Errorf("checkpoints = %+v, want none on a clean tree", got.Checkpoints)
	}
	for _, c := range fake.joined() {
		if strings.Contains(c, "commit") || strings.Contains(c, "build") {
			t.Errorf("clean tree must not commit or verify; ran: %v", fake.joined())
			break
		}
	}
}

func TestAfterTurn_VerifyFailMarksFailedButKeepsCheckpoint(t *testing.T) {
	fake := &fakeRunner{
		out:  map[string][]byte{"status": []byte("M main.go\n"), "rev-parse": []byte("def456\n")},
		fail: map[string]bool{"test": true}, // go test fails
	}
	m, store := newTurnFixture(t, "bug", fake)

	AfterTurn(context.Background(), m, store, "bug", "버그 수정 시도", nil)

	got, _ := store.Get("bug")
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want %q", got.Status, StatusFailed)
	}
	// The checkpoint is committed before verify, so a failing build is still saved
	// (and thus undoable) — the user sees a failed-but-recoverable step.
	if len(got.Checkpoints) != 1 {
		t.Errorf("checkpoints = %+v, want one (commit precedes verify)", got.Checkpoints)
	}
}
