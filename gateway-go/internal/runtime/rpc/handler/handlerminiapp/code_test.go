package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/code"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeWorktrees struct {
	status    code.WorktreeStatus
	statusErr error
}

func (fakeWorktrees) StartTask(_ context.Context, r code.Repo, id string) (code.Task, error) {
	return code.Task{ID: id, Repo: r, Branch: "deneb/" + id, Dir: "/wt/" + id}, nil
}

func (fakeWorktrees) ListRepos(context.Context) ([]code.Repo, error) {
	return []code.Repo{{Owner: "acme", Name: "app"}}, nil
}
func (f fakeWorktrees) WorktreeStatus(context.Context, code.Task) (code.WorktreeStatus, error) {
	return f.status, f.statusErr
}
func (fakeWorktrees) Discard(context.Context, code.Task) error { return nil }
func (fakeWorktrees) Verify(context.Context, string) (code.VerifyResult, error) {
	return code.VerifyResult{Kind: code.KindGo, Passed: true}, nil
}
func (fakeWorktrees) Commit(context.Context, code.Task, string) error    { return nil }
func (fakeWorktrees) HeadSHA(context.Context, code.Task) (string, error) { return "sha", nil }
func (fakeWorktrees) Undo(context.Context, code.Task) (bool, error)      { return true, nil }
func (fakeWorktrees) Push(context.Context, code.Task) error              { return nil }
func (fakeWorktrees) PRURL(context.Context, code.Task) (string, error) {
	return "https://github.com/acme/app/pull/1", nil
}

type fakeSessions struct{}

func (fakeSessions) Add(*code.Session) error                     { return nil }
func (fakeSessions) Get(string) (code.Session, bool)             { return code.Session{}, false }
func (fakeSessions) List() []code.Session                        { return nil }
func (fakeSessions) Reconcile(func(string) bool) error           { return nil }
func (fakeSessions) Delete(string) error                         { return nil }
func (fakeSessions) SetStatus(string, string) error              { return nil }
func (fakeSessions) AddCheckpoint(string, code.Checkpoint) error { return nil }
func (fakeSessions) PopCheckpoint(string) error                  { return nil }

type fakeSessionsWith struct {
	fakeSessions
	sess code.Session
	list []code.Session
}

func (f fakeSessionsWith) Get(id string) (code.Session, bool) {
	if f.sess.ID == id {
		return f.sess, true
	}
	return code.Session{}, false
}

func (f fakeSessionsWith) List() []code.Session {
	if f.list != nil {
		return f.list
	}
	return []code.Session{f.sess}
}

func codeReq(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("test-1", method, params)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

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
		"miniapp.code.pr",
		"miniapp.code.verify",
		"miniapp.code.checkpoint",
		"miniapp.code.undo",
		"miniapp.code.push",
		"miniapp.code.discard",
		"miniapp.code.close",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing method %q", k)
		}
	}
}

func TestActiveSessions_DropsClosed(t *testing.T) {
	in := []code.Session{
		{ID: "a", Status: code.StatusWorking},
		{ID: "b", Status: code.StatusClosed},
		{ID: "c", Status: code.StatusPassed},
		{ID: "d", Status: code.StatusClosed},
	}
	got := activeSessions(in)
	if len(got) != 2 {
		t.Fatalf("want 2 active sessions, got %d: %+v", len(got), got)
	}
	for _, s := range got {
		if s.Status == code.StatusClosed {
			t.Errorf("closed session %q leaked into the active rail list", s.ID)
		}
	}
}

func TestCodeStatus_IncludesWorktreeDirtyState(t *testing.T) {
	sess := code.Session{
		ID:     "t",
		Repo:   code.Repo{Owner: "acme", Name: "app"},
		Status: code.StatusWorking,
		Branch: "deneb/t",
		Dir:    "/wt/t",
	}
	h := codeStatus(CodeDeps{
		Worktrees: fakeWorktrees{status: code.WorktreeStatus{Dirty: true, ChangedFiles: 2}},
		Sessions:  fakeSessionsWith{sess: sess},
	})

	resp := h(
		clientauth.WithContext(context.Background(), sampleIdentity()),
		codeReq(t, "miniapp.code.status", map[string]string{"id": "t"}),
	)
	got := decodePayload(t, resp)
	session, ok := got["session"].(map[string]any)
	if !ok {
		t.Fatalf("session payload missing or wrong type: %#v", got["session"])
	}
	if session["dirty"] != true {
		t.Errorf("dirty = %v, want true", session["dirty"])
	}
	if session["changedFiles"] != float64(2) {
		t.Errorf("changedFiles = %v, want 2", session["changedFiles"])
	}
}

func TestCodeSessions_OmitsDirtyStateWhenWorktreeMissing(t *testing.T) {
	sess := code.Session{
		ID:     "t",
		Repo:   code.Repo{Owner: "acme", Name: "app"},
		Status: code.StatusMissing,
		Branch: "deneb/t",
		Dir:    "/wt/t",
	}
	h := codeSessions(CodeDeps{
		Worktrees: fakeWorktrees{status: code.WorktreeStatus{Dirty: true, ChangedFiles: 2}},
		Sessions:  fakeSessionsWith{sess: sess},
	})

	resp := h(clientauth.WithContext(context.Background(), sampleIdentity()), newReq(t, "miniapp.code.sessions"))
	got := decodePayload(t, resp)
	sessions, ok := got["sessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("sessions payload = %#v, want one session", got["sessions"])
	}
	session, ok := sessions[0].(map[string]any)
	if !ok {
		t.Fatalf("session row wrong type: %#v", sessions[0])
	}
	if _, ok := session["dirty"]; ok {
		t.Errorf("dirty key present for missing worktree: %#v", session)
	}
	if _, ok := session["changedFiles"]; ok {
		t.Errorf("changedFiles key present for missing worktree: %#v", session)
	}
}

func TestTaskFromSession(t *testing.T) {
	s := code.Session{ID: "t", Repo: code.Repo{Owner: "a", Name: "b"}, Branch: "deneb/t", Dir: "/wt/t"}
	task := taskFromSession(s)
	if task.ID != "t" || task.Repo.Owner != "a" || task.Branch != "deneb/t" || task.Dir != "/wt/t" {
		t.Errorf("taskFromSession = %+v", task)
	}
}
