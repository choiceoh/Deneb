package code

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records git invocations and lets a test force specific subcommands
// to fail, so worktree orchestration is verified without shelling out to git.
type fakeRunner struct {
	calls []fakeCall
	fail  map[string]bool   // keyed by first arg (the git subcommand)
	out   map[string][]byte // canned stdout keyed by first arg
}

type fakeCall struct {
	dir  string
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakeCall{dir: dir, name: name, args: append([]string(nil), args...)})
	key := ""
	if len(args) > 0 {
		key = args[0]
	}
	if f.fail[key] {
		return nil, fmt.Errorf("forced fail: %s", key)
	}
	return f.out[key], nil
}

// joined returns each recorded call as "name arg arg …" for order assertions.
func (f *fakeRunner) joined() []string {
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.name + " " + strings.Join(c.args, " ")
	}
	return out
}

func wantSeq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("command count = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStartTask_FreshClone(t *testing.T) {
	// rev-parse fails → base missing → clone, then worktree add.
	fake := &fakeRunner{fail: map[string]bool{"rev-parse": true}}
	m := &Manager{Root: t.TempDir(), DefaultBranch: "main", Runner: fake}

	task, err := m.StartTask(context.Background(), Repo{Owner: "acme", Name: "app"}, "fix-login")
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if task.Branch != "deneb/fix-login" {
		t.Errorf("branch = %q, want deneb/fix-login", task.Branch)
	}
	if wantSuffix := filepath.Join("wt", "fix-login"); !strings.HasSuffix(task.Dir, wantSuffix) {
		t.Errorf("dir = %q, want it to end at %q", task.Dir, wantSuffix)
	}
	wantSeq(t, fake.joined(), []string{
		"git rev-parse --git-dir",
		"git clone https://github.com/acme/app.git " + m.basePath(task.Repo),
		"git worktree add -b deneb/fix-login " + task.Dir + " origin/main",
	})
}

func TestStartTask_ExistingFetch(t *testing.T) {
	// rev-parse succeeds → base present → fetch, then worktree add (no clone).
	fake := &fakeRunner{}
	m := &Manager{Root: t.TempDir(), DefaultBranch: "main", Runner: fake}

	if _, err := m.StartTask(context.Background(), Repo{Owner: "acme", Name: "app"}, "task1"); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	got := fake.joined()
	wantSeq(t, got, []string{
		"git rev-parse --git-dir",
		"git fetch origin",
		"git worktree add -b deneb/task1 " + m.worktreePath(Repo{"acme", "app"}, "task1") + " origin/main",
	})
}

func TestCommitPushDiscard(t *testing.T) {
	fake := &fakeRunner{}
	m := &Manager{Root: t.TempDir(), Runner: fake}
	task := Task{ID: "t", Repo: Repo{"acme", "app"}, Branch: "deneb/t", Dir: "/wt/t"}

	if err := m.Commit(context.Background(), task, "add status field"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := m.Push(context.Background(), task); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := m.Discard(context.Background(), task); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	wantSeq(t, fake.joined(), []string{
		"git add -A",
		"git commit -m add status field",
		"git push -u origin deneb/t",
		"git worktree remove --force /wt/t",
		"git branch -D deneb/t",
	})
}

func TestCommit_DefaultMessage(t *testing.T) {
	fake := &fakeRunner{}
	m := &Manager{Root: t.TempDir(), Runner: fake}
	if err := m.Commit(context.Background(), Task{Dir: "/wt/t"}, "   "); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got := fake.joined()[1]; got != "git commit -m deneb: update" {
		t.Errorf("default commit = %q, want fallback message", got)
	}
}

func TestStartTask_RejectsBadInput(t *testing.T) {
	m := &Manager{Root: t.TempDir(), Runner: &fakeRunner{}}
	cases := []struct {
		repo Repo
		id   string
	}{
		{Repo{"", "app"}, "ok"},         // empty owner
		{Repo{"acme", ".."}, "ok"},      // traversal in name
		{Repo{"a/b", "app"}, "ok"},      // separator in owner
		{Repo{"acme", "app"}, "Bad_ID"}, // uppercase + underscore
		{Repo{"acme", "app"}, "a/b"},    // separator in id
		{Repo{"acme", "app"}, ""},       // empty id
	}
	for _, c := range cases {
		if _, err := m.StartTask(context.Background(), c.repo, c.id); err == nil {
			t.Errorf("StartTask(%+v, %q) = nil error, want rejection", c.repo, c.id)
		}
	}
}

func TestNewTaskID_IsSlugSafe(t *testing.T) {
	// The auto-generated id (used when the user leaves 작업 ID blank) must pass the
	// same validation a hand-typed id does, or StartTask would reject it.
	id := NewTaskID()
	if !strings.HasPrefix(id, "task-") {
		t.Errorf("NewTaskID = %q, want a task- prefix", id)
	}
	if err := validateTaskID(id); err != nil {
		t.Errorf("NewTaskID %q failed validateTaskID: %v", id, err)
	}
	// And it must actually drive a StartTask end-to-end (branch + worktree named off it).
	m := &Manager{Root: t.TempDir(), DefaultBranch: "main", Runner: &fakeRunner{}}
	task, err := m.StartTask(context.Background(), Repo{Owner: "acme", Name: "app"}, id)
	if err != nil {
		t.Fatalf("StartTask with auto id %q: %v", id, err)
	}
	if task.Branch != "deneb/"+id {
		t.Errorf("branch = %q, want deneb/%s", task.Branch, id)
	}
}

func TestNewTaskTitle_HumanLabel(t *testing.T) {
	// Display-only fallback when the user names nothing — should read as a label,
	// not a slug.
	if got := NewTaskTitle(); !strings.HasPrefix(got, "새 작업 ") {
		t.Errorf("NewTaskTitle = %q, want a 새 작업 prefix", got)
	}
}

func TestRepoCloneURL(t *testing.T) {
	if got := (Repo{Owner: "acme", Name: "app"}).CloneURL(); got != "https://github.com/acme/app.git" {
		t.Errorf("CloneURL = %q", got)
	}
}

func TestPRURL(t *testing.T) {
	fake := &fakeRunner{out: map[string][]byte{"pr": []byte("https://github.com/acme/app/pull/7\n")}}
	m := &Manager{Runner: fake}
	url, err := m.PRURL(context.Background(), Task{Repo: Repo{"acme", "app"}, Branch: "deneb/fix"})
	if err != nil {
		t.Fatalf("PRURL: %v", err)
	}
	if url != "https://github.com/acme/app/pull/7" {
		t.Errorf("url = %q, want the trimmed PR url", url)
	}
	// Targets the repo by -R owner/repo + the task branch, across all PR states.
	cmd := fake.joined()[0]
	for _, want := range []string{"gh pr list", "-R acme/app", "--head deneb/fix", "--state all"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("PRURL cmd %q missing %q", cmd, want)
		}
	}
}

func TestPRURL_NoPR(t *testing.T) {
	// gh's --jq yields empty output when no PR matches the branch → "" (no link).
	fake := &fakeRunner{out: map[string][]byte{"pr": []byte("\n")}}
	m := &Manager{Runner: fake}
	url, err := m.PRURL(context.Background(), Task{Repo: Repo{"acme", "app"}, Branch: "deneb/x"})
	if err != nil || url != "" {
		t.Errorf("PRURL no-pr = (%q, %v), want empty string and no error", url, err)
	}
}

func TestUndo_DirtyDiscardsToCheckpoint(t *testing.T) {
	// status --porcelain returns changes → dirty → reset HEAD + clean untracked.
	fake := &fakeRunner{out: map[string][]byte{"status": []byte(" M main.go\n")}}
	m := &Manager{Runner: fake}
	popped, err := m.Undo(context.Background(), Task{Dir: "/wt/t", Branch: "deneb/t"})
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if popped {
		t.Error("dirty undo discards uncommitted edits — no checkpoint commit dropped")
	}
	wantSeq(t, fake.joined(), []string{
		"git status --porcelain",
		"git reset --hard HEAD",
		"git clean -fd",
	})
}

func TestUndo_CleanRevertsLastCheckpoint(t *testing.T) {
	// status empty → clean tree; rev-list shows checkpoints ahead of base → reset.
	fake := &fakeRunner{out: map[string][]byte{"rev-list": []byte("2\n")}}
	m := &Manager{DefaultBranch: "main", Runner: fake}
	popped, err := m.Undo(context.Background(), Task{Dir: "/wt/t"})
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if !popped {
		t.Error("clean undo drops a checkpoint commit — popped should be true")
	}
	wantSeq(t, fake.joined(), []string{
		"git status --porcelain",
		"git rev-list --count origin/main..HEAD",
		"git reset --hard HEAD~1",
	})
}

func TestUndo_NothingToRevert(t *testing.T) {
	// clean tree + no checkpoints ahead of base → refuse (never reset past the fork).
	fake := &fakeRunner{out: map[string][]byte{"rev-list": []byte("0\n")}}
	m := &Manager{DefaultBranch: "main", Runner: fake}
	if _, err := m.Undo(context.Background(), Task{Dir: "/wt/t"}); err == nil {
		t.Error("undo with no checkpoints should error, not reset past the fork")
	}
	for _, c := range fake.joined() {
		if c == "git reset --hard HEAD~1" {
			t.Error("reset must not run when there is nothing to undo")
		}
	}
}

func TestHeadSHA(t *testing.T) {
	fake := &fakeRunner{out: map[string][]byte{"rev-parse": []byte("abc123\n")}}
	m := &Manager{Runner: fake}
	sha, err := m.HeadSHA(context.Background(), Task{Dir: "/wt/t"})
	if err != nil || sha != "abc123" {
		t.Errorf("HeadSHA = %q, err=%v", sha, err)
	}
}
