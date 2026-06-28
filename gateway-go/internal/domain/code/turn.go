package code

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// AfterTurn is the coding turn-end orchestration: it snapshots the worktree edits
// as a checkpoint, then verifies build/tests and updates the session status so the
// rail reflects pass/fail. The chat turn-end hook calls this for coding sessions —
// the agent edits the worktree during the turn; this captures and grades the result.
//
// Best-effort: a read-only turn (no edits) checkpoints nothing and skips verify,
// leaving the prior status intact; individual step failures are logged and do not
// abort the rest. Callers serialize per task — AfterTurn itself does no locking.
func AfterTurn(ctx context.Context, m *Manager, store *Store, taskID, summary string, logger *slog.Logger) {
	if m == nil || store == nil || taskID == "" {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	sess, ok := store.Get(taskID)
	if !ok {
		return
	}
	task := Task{ID: sess.ID, Repo: sess.Repo, Branch: sess.Branch, Dir: sess.Dir}

	// Only do work when the turn actually changed files. A turn where the agent
	// just read or answered leaves a clean tree → no checkpoint, and the previous
	// verify status still stands (so we skip a wasteful rebuild).
	dirty, err := m.hasUncommitted(ctx, sess.Dir)
	if err != nil {
		logger.Warn("coding turn-end: worktree status check failed", "task", taskID, "error", err)
		return
	}
	if !dirty {
		return
	}

	// 1. Checkpoint the edits — even if verify later fails, the change is saved and
	//    undoable, and the rail shows what the turn did.
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = "변경 저장"
	}
	if err := m.Commit(ctx, task, summary); err != nil {
		logger.Warn("coding turn-end: commit failed", "task", taskID, "error", err)
		return
	}
	if sha, err := m.HeadSHA(ctx, task); err != nil {
		logger.Warn("coding turn-end: read commit SHA failed", "task", taskID, "error", err)
	} else if err := store.AddCheckpoint(taskID, Checkpoint{
		SHA:     sha,
		Summary: summary,
		At:      time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		logger.Warn("coding turn-end: checkpoint save failed", "task", taskID, "error", err)
	}

	// 2. Verify build/tests and flip the rail status. An unknown toolchain yields
	//    no pass/fail signal — leave the status as-is (mirrors miniapp.code.verify).
	res, err := m.Verify(ctx, sess.Dir)
	if err != nil {
		logger.Warn("coding turn-end: verify failed to run", "task", taskID, "error", err)
		return
	}
	if res.Kind == KindUnknown {
		return
	}
	status := StatusFailed
	if res.Passed {
		status = StatusPassed
	}
	if err := store.SetStatus(taskID, status); err != nil {
		logger.Warn("coding turn-end: status update failed", "task", taskID, "error", err)
	}
}
