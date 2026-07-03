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

// dirtyOnceRunner models the normal turn: `git status` reports the turn's
// edits once, then a clean tree after the commit (so the verify race guards
// see the tree still matching the checkpoint).
func dirtyOnceRunner(extra map[string][]byte) *fakeRunner {
	out := map[string][]byte{"rev-parse": []byte("abc123\n")} // HeadSHA
	for k, v := range extra {
		out[k] = v
	}
	return &fakeRunner{
		out: out,
		seq: map[string][][]byte{"status": {[]byte("M main.go\n")}}, // dirty → (drained = clean)
	}
}

func TestAfterTurn_DirtyCommitsAndPasses(t *testing.T) {
	fake := dirtyOnceRunner(nil)
	m, store := newTurnFixture(t, "fix-login", fake)

	AfterTurn(context.Background(), m, store, "fix-login", "로그인 폼 추가", nil, nil)

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

	AfterTurn(context.Background(), m, store, "readonly", "코드 설명해줘", nil, nil)

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

func TestAfterTurn_SummarizeLabelsCheckpoint(t *testing.T) {
	t.Run("summarize result becomes the label", func(t *testing.T) {
		fake := dirtyOnceRunner(nil)
		m, store := newTurnFixture(t, "labeled", fake)

		AfterTurn(context.Background(), m, store, "labeled", "로그인 고쳐줘",
			func(context.Context) string { return " 로그인 폼 검증 추가 " }, nil)

		got, _ := store.Get("labeled")
		if len(got.Checkpoints) != 1 || got.Checkpoints[0].Summary != "로그인 폼 검증 추가" {
			t.Errorf("checkpoints = %+v, want the trimmed summarize label", got.Checkpoints)
		}
	})

	t.Run("empty summarize keeps the fallback (fail-open)", func(t *testing.T) {
		fake := dirtyOnceRunner(nil)
		m, store := newTurnFixture(t, "fallback", fake)

		AfterTurn(context.Background(), m, store, "fallback", "로그인 고쳐줘",
			func(context.Context) string { return "  " }, nil)

		got, _ := store.Get("fallback")
		if len(got.Checkpoints) != 1 || got.Checkpoints[0].Summary != "로그인 고쳐줘" {
			t.Errorf("checkpoints = %+v, want the fallback summary", got.Checkpoints)
		}
	})

	t.Run("clean tree never invokes summarize", func(t *testing.T) {
		fake := &fakeRunner{} // clean
		m, store := newTurnFixture(t, "clean", fake)

		called := false
		AfterTurn(context.Background(), m, store, "clean", "설명해줘",
			func(context.Context) string { called = true; return "x" }, nil)

		if called {
			t.Error("summarize must not run for a read-only turn (dirty check first)")
		}
	})
}

func TestAfterTurn_VerifyFailMarksFailedButKeepsCheckpoint(t *testing.T) {
	fake := dirtyOnceRunner(map[string][]byte{"rev-parse": []byte("def456\n")})
	fake.fail = map[string]bool{"test": true} // go test fails
	m, store := newTurnFixture(t, "bug", fake)

	AfterTurn(context.Background(), m, store, "bug", "버그 수정 시도", nil, nil)

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

// TestAfterTurn_NextTurnEditsSkipVerify pins the detached-verify race guard:
// AfterTurn runs after the turn returns, so the user may already be editing
// via the NEXT turn — those in-progress edits must not be graded under THIS
// turn's checkpoint (a transient rail mislabel). Dirty again at verify start
// → checkpoint stays, verify is skipped, prior status stands.
func TestAfterTurn_NextTurnEditsSkipVerify(t *testing.T) {
	fake := &fakeRunner{
		out: map[string][]byte{"rev-parse": []byte("abc123\n")},
		seq: map[string][][]byte{"status": {
			[]byte("M main.go\n"),  // turn's own edits → checkpoint
			[]byte("M other.go\n"), // next turn already editing → skip verify
		}},
	}
	m, store := newTurnFixture(t, "racing", fake)

	AfterTurn(context.Background(), m, store, "racing", "로그인 수정", nil, nil)

	got, _ := store.Get("racing")
	if got.Status != StatusWorking {
		t.Errorf("status = %q, want unchanged %q (dirty tree must not be graded)", got.Status, StatusWorking)
	}
	if len(got.Checkpoints) != 1 {
		t.Errorf("checkpoints = %+v, want one (the commit itself must not be skipped)", got.Checkpoints)
	}
	for _, c := range fake.joined() {
		if strings.Contains(c, "go build") || strings.Contains(c, "go test") {
			t.Errorf("verify must not run on a dirty tree; ran: %v", fake.joined())
			break
		}
	}
}

// TestAfterTurn_EditsDuringVerifyKeepPriorStatus covers the other side of the
// race: the tree was clean when verify started but changed while the build
// ran — the result no longer describes the checkpoint, so the rail keeps its
// prior status (verify itself did run).
func TestAfterTurn_EditsDuringVerifyKeepPriorStatus(t *testing.T) {
	fake := &fakeRunner{
		out: map[string][]byte{"rev-parse": []byte("abc123\n")},
		seq: map[string][][]byte{"status": {
			[]byte("M main.go\n"),  // turn's own edits → checkpoint
			[]byte(""),             // clean at verify start → verify runs
			[]byte("M other.go\n"), // dirty after verify → don't flip status
		}},
	}
	m, store := newTurnFixture(t, "midrace", fake)

	AfterTurn(context.Background(), m, store, "midrace", "로그인 수정", nil, nil)

	got, _ := store.Get("midrace")
	if got.Status != StatusWorking {
		t.Errorf("status = %q, want unchanged %q (result no longer describes the checkpoint)", got.Status, StatusWorking)
	}
	ranVerify := false
	for _, c := range fake.joined() {
		if strings.Contains(c, "go build") {
			ranVerify = true
			break
		}
	}
	if !ranVerify {
		t.Errorf("verify should have run (tree was clean at start); ran: %v", fake.joined())
	}
}
