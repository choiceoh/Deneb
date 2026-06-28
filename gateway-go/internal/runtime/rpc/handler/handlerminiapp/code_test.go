package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/code"
)

type fakeWorktrees struct{}

func (fakeWorktrees) StartTask(_ context.Context, r code.Repo, id string) (code.Task, error) {
	return code.Task{ID: id, Repo: r, Branch: "deneb/" + id, Dir: "/wt/" + id}, nil
}

func (fakeWorktrees) ListRepos(context.Context) ([]code.Repo, error) {
	return []code.Repo{{Owner: "acme", Name: "app"}}, nil
}
func (fakeWorktrees) Discard(context.Context, code.Task) error { return nil }
func (fakeWorktrees) Verify(context.Context, string) (code.VerifyResult, error) {
	return code.VerifyResult{Kind: code.KindGo, Passed: true}, nil
}
func (fakeWorktrees) Commit(context.Context, code.Task, string) error    { return nil }
func (fakeWorktrees) HeadSHA(context.Context, code.Task) (string, error) { return "sha", nil }
func (fakeWorktrees) Undo(context.Context, code.Task) (bool, error)      { return true, nil }
func (fakeWorktrees) Push(context.Context, code.Task) error              { return nil }

type fakeSessions struct{}

func (fakeSessions) Add(*code.Session) error                     { return nil }
func (fakeSessions) Get(string) (code.Session, bool)             { return code.Session{}, false }
func (fakeSessions) List() []code.Session                        { return nil }
func (fakeSessions) Reconcile(func(string) bool) error           { return nil }
func (fakeSessions) Delete(string) error                         { return nil }
func (fakeSessions) SetStatus(string, string) error              { return nil }
func (fakeSessions) AddCheckpoint(string, code.Checkpoint) error { return nil }
func (fakeSessions) PopCheckpoint(string) error                  { return nil }

func TestCodeMethods_NilDepsSkips(t *testing.T) {
	if CodeMethods(CodeDeps{}) != nil {
		t.Error("nil deps should yield no methods")
	}
	if CodeMethods(CodeDeps{Worktrees: fakeWorktrees{}}) != nil {
		t.Error("missing Sessions should yield no methods")
	}
}

func TestCodeMethods_Keys(t *testing.T) {
	m := CodeMethods(CodeDeps{Worktrees: fakeWorktrees{}, Sessions: fakeSessions{}})
	for _, k := range []string{
		"miniapp.code.sessions",
		"miniapp.code.repos",
		"miniapp.code.start",
		"miniapp.code.status",
		"miniapp.code.verify",
		"miniapp.code.checkpoint",
		"miniapp.code.undo",
		"miniapp.code.push",
		"miniapp.code.discard",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing method %q", k)
		}
	}
}

func TestTaskFromSession(t *testing.T) {
	s := code.Session{ID: "t", Repo: code.Repo{Owner: "a", Name: "b"}, Branch: "deneb/t", Dir: "/wt/t"}
	task := taskFromSession(s)
	if task.ID != "t" || task.Repo.Owner != "a" || task.Branch != "deneb/t" || task.Dir != "/wt/t" {
		t.Errorf("taskFromSession = %+v", task)
	}
}
