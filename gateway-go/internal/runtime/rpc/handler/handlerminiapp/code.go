// code.go — miniapp.code.* RPC handlers (coding-mode session surface).
//
//   sessions / repos         — list sessions (the rail) / the operator's GitHub repos
//   start / discard          — create / remove a worktree + session
//   status                   — one session's current state
//   verify                   — run build/test, flip the session status
//   checkpoint / undo / push — save a change / step back / push the branch
//
// The auto verify-on-turn, Korean summary, and auto push+PR orchestration land
// later (it needs the model). Worktrees and Sessions are interfaces so handlers
// test with fakes; the real wiring passes *code.Manager and *code.Store.

package handlerminiapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/code"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CodeWorktrees is the subset of *code.Manager the handlers use.
type CodeWorktrees interface {
	StartTask(ctx context.Context, r code.Repo, taskID string) (code.Task, error)
	ListRepos(ctx context.Context) ([]code.Repo, error)
	Verify(ctx context.Context, dir string) (code.VerifyResult, error)
	Commit(ctx context.Context, t code.Task, message string) error
	HeadSHA(ctx context.Context, t code.Task) (string, error)
	Undo(ctx context.Context, t code.Task) (bool, error)
	Push(ctx context.Context, t code.Task) error
	Discard(ctx context.Context, t code.Task) error
}

// CodeSessions is the subset of *code.Store the handlers use.
type CodeSessions interface {
	Add(sess *code.Session) error
	Get(id string) (code.Session, bool)
	List() []code.Session
	Reconcile(exists func(dir string) bool) error
	SetStatus(id, status string) error
	AddCheckpoint(id string, cp code.Checkpoint) error
	PopCheckpoint(id string) error
	Delete(id string) error
}

// CodeDeps wires the coding-mode handlers. Both nil → the domain is skipped.
type CodeDeps struct {
	Worktrees CodeWorktrees
	Sessions  CodeSessions
}

// codeOpTimeout bounds the git/exec a handler may run (clone, npm install, build)
// so a hung command can't pin the request past the write-timeout backstop.
const codeOpTimeout = 5 * time.Minute

// CodeMethods returns the miniapp.code.* handlers, or nil when unconfigured.
func CodeMethods(deps CodeDeps) map[string]rpcutil.HandlerFunc {
	if deps.Worktrees == nil || deps.Sessions == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.code.sessions":   codeSessions(deps),
		"miniapp.code.repos":      codeRepos(deps),
		"miniapp.code.start":      codeStart(deps),
		"miniapp.code.status":     codeStatus(deps),
		"miniapp.code.verify":     codeVerify(deps),
		"miniapp.code.checkpoint": codeCheckpoint(deps),
		"miniapp.code.undo":       codeUndo(deps),
		"miniapp.code.push":       codePush(deps),
		"miniapp.code.discard":    codeDiscard(deps),
	}
}

func codeSessions(deps CodeDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		// Mark sessions whose worktree vanished (manual cleanup, crash) as missing
		// so the rail reflects reality and stale rows don't fail every action.
		_ = deps.Sessions.Reconcile(nil)
		return rpcutil.RespondOK(req.ID, map[string]any{"sessions": deps.Sessions.List()})
	}
}

func codeRepos(deps CodeDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		repos, err := deps.Worktrees.ListRepos(ctx)
		if err != nil {
			// gh unauthenticated / unavailable → empty picker; the UI falls back
			// to manual owner/repo entry rather than erroring.
			return rpcutil.RespondOK(req.ID, map[string]any{"repos": []code.Repo{}})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"repos": repos})
	}
}

func codeStart(deps CodeDeps) rpcutil.HandlerFunc {
	type params struct {
		Owner  string `json:"owner"`
		Name   string `json:"name"`
		TaskID string `json:"taskId"`
		Title  string `json:"title,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		repo := code.Repo{Owner: strings.TrimSpace(p.Owner), Name: strings.TrimSpace(p.Name)}
		taskID := strings.TrimSpace(p.TaskID)
		if repo.Owner == "" || repo.Name == "" || taskID == "" {
			return rpcerr.InvalidParams(fmt.Errorf("owner, name, and taskId are required")).Response(req.ID)
		}

		ctx, cancel := context.WithTimeout(ctx, codeOpTimeout)
		defer cancel()
		task, err := deps.Worktrees.StartTask(ctx, repo, taskID)
		if err != nil {
			return rpcerr.WrapUnavailable("worktree start failed", err).Response(req.ID)
		}
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = task.ID
		}
		sess := code.NewSession(task, title, "")
		if err := deps.Sessions.Add(sess); err != nil {
			// The worktree exists but we couldn't record the session — leaving it
			// would strand an un-discardable worktree, so roll it back on a fresh
			// short deadline (the start ctx may be exhausted by a slow clone).
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cleanupCancel()
			_ = deps.Worktrees.Discard(cleanupCtx, task)
			return rpcerr.WrapUnavailable("session save failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"session": sess})
	}
}

func codeStatus(deps CodeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		sess, ok := deps.Sessions.Get(strings.TrimSpace(p.ID))
		if !ok {
			return rpcerr.InvalidParams(fmt.Errorf("session %q not found", strings.TrimSpace(p.ID))).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"session": sess})
	}
}

func codeDiscard(deps CodeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		id := strings.TrimSpace(p.ID)
		sess, ok := deps.Sessions.Get(id)
		if !ok {
			return rpcerr.InvalidParams(fmt.Errorf("session %q not found", id)).Response(req.ID)
		}
		if err := deps.Worktrees.Discard(ctx, taskFromSession(sess)); err != nil {
			return rpcerr.WrapUnavailable("worktree discard failed", err).Response(req.ID)
		}
		if err := deps.Sessions.Delete(id); err != nil {
			return rpcerr.WrapUnavailable("session delete failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true})
	}
}

func codeVerify(deps CodeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		sess, ok := deps.Sessions.Get(strings.TrimSpace(p.ID))
		if !ok {
			return rpcerr.InvalidParams(fmt.Errorf("session %q not found", strings.TrimSpace(p.ID))).Response(req.ID)
		}
		ctx, cancel := context.WithTimeout(ctx, codeOpTimeout)
		defer cancel()
		res, err := deps.Worktrees.Verify(ctx, sess.Dir)
		if err != nil {
			return rpcerr.WrapUnavailable("verify failed", err).Response(req.ID)
		}
		// Only a recognized toolchain yields a real pass/fail. An unknown project
		// (no marker, or a subdir layout) must not read as 실패 to the user.
		if res.Kind != code.KindUnknown {
			status := code.StatusFailed
			if res.Passed {
				status = code.StatusPassed
			}
			if err := deps.Sessions.SetStatus(sess.ID, status); err != nil {
				return rpcerr.WrapUnavailable("status update failed", err).Response(req.ID)
			}
		}
		updated, _ := deps.Sessions.Get(sess.ID)
		return rpcutil.RespondOK(req.ID, map[string]any{"session": updated, "result": res})
	}
}

func codeCheckpoint(deps CodeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID      string `json:"id"`
		Summary string `json:"summary,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		sess, ok := deps.Sessions.Get(strings.TrimSpace(p.ID))
		if !ok {
			return rpcerr.InvalidParams(fmt.Errorf("session %q not found", strings.TrimSpace(p.ID))).Response(req.ID)
		}
		task := taskFromSession(sess)
		summary := strings.TrimSpace(p.Summary)
		if summary == "" {
			summary = "변경 저장"
		}
		if err := deps.Worktrees.Commit(ctx, task, summary); err != nil {
			return rpcerr.WrapUnavailable("commit failed", err).Response(req.ID)
		}
		sha, err := deps.Worktrees.HeadSHA(ctx, task)
		if err != nil {
			return rpcerr.WrapUnavailable("read commit failed", err).Response(req.ID)
		}
		if err := deps.Sessions.AddCheckpoint(sess.ID, code.Checkpoint{SHA: sha, Summary: summary, At: nowRFC3339()}); err != nil {
			return rpcerr.WrapUnavailable("checkpoint save failed", err).Response(req.ID)
		}
		updated, _ := deps.Sessions.Get(sess.ID)
		return rpcutil.RespondOK(req.ID, map[string]any{"session": updated})
	}
}

func codeUndo(deps CodeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		sess, ok := deps.Sessions.Get(strings.TrimSpace(p.ID))
		if !ok {
			return rpcerr.InvalidParams(fmt.Errorf("session %q not found", strings.TrimSpace(p.ID))).Response(req.ID)
		}
		popped, err := deps.Worktrees.Undo(ctx, taskFromSession(sess))
		if err != nil {
			return rpcerr.WrapUnavailable("undo failed", err).Response(req.ID)
		}
		// Only drop a checkpoint record when a checkpoint *commit* was dropped
		// (clean undo). A dirty undo discarded uncommitted edits only — the commit
		// and its record both stay.
		if popped {
			_ = deps.Sessions.PopCheckpoint(sess.ID)
		}
		updated, _ := deps.Sessions.Get(sess.ID)
		return rpcutil.RespondOK(req.ID, map[string]any{"session": updated})
	}
}

func codePush(deps CodeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		sess, ok := deps.Sessions.Get(strings.TrimSpace(p.ID))
		if !ok {
			return rpcerr.InvalidParams(fmt.Errorf("session %q not found", strings.TrimSpace(p.ID))).Response(req.ID)
		}
		ctx, cancel := context.WithTimeout(ctx, codeOpTimeout)
		defer cancel()
		if err := deps.Worktrees.Push(ctx, taskFromSession(sess)); err != nil {
			return rpcerr.WrapUnavailable("push failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true})
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// taskFromSession rebuilds the worktree handle a session points at.
func taskFromSession(s code.Session) code.Task {
	return code.Task{ID: s.ID, Repo: s.Repo, Branch: s.Branch, Dir: s.Dir}
}
