// Package checkpoint provides the checkpoint.{list,restore,diff} RPC methods
// that back the user-facing /rollback slash command.
//
// Design notes:
//
//   - Checkpoints are persisted per-session under <root>/<sessionID>/... by
//     pkg/checkpoint. A live pkg/checkpoint.Manager is normally scoped to a
//     single agent run, but for List/Restore/Diff we only read the persisted
//     JSONL index + blobs. So each RPC call builds a throw-away Manager
//     using the same root + sessionID and defers to it.
//   - Tombstone snapshots carry an empty BlobPath on disk and a "tombstone"
//     flag on the wire; callers that want to render them should check
//     Tombstone and substitute a placeholder ("(삭제된 상태)").
//   - Root must be configured on the hub (via SetCheckpointRoot). When empty
//     the handlers reply with UNAVAILABLE so callers can render a user-
//     friendly Korean notice.
//
// Handler packages must never import the hub type directly — the Deps struct
// is the only contract. See .claude/rules/hub-wiring.md Rule 3.
package checkpoint

import (
	"context"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps carries the dependencies for the checkpoint RPC domain.
// Root is the directory under which per-session snapshot stores live. When
// empty, Methods() still registers the three methods but each replies with
// UNAVAILABLE — keeping the names discoverable via tools.catalog rather
// than silently falling through to METHOD_NOT_FOUND.
// Logger is optional; when nil, slog.Default() is used.
type Deps struct {
	Root   string
	Logger *slog.Logger
}

// ListParams is the RPC argument shape for checkpoint.list.
type ListParams struct {
	SessionKey string `json:"sessionKey"`
	Path       string `json:"path,omitempty"`  // optional: filter to a single absolute path
	Limit      int    `json:"limit,omitempty"` // optional: cap result count (0 = no limit)
}

// SnapshotWire is a JSON-serialisable projection of checkpoint.Snapshot.
// We re-shape it so callers get stable field names independent of the
// storage package and so tombstones can be rendered without the caller
// looking up an empty BlobPath.
type SnapshotWire struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Seq       int    `json:"seq"`
	TakenAt   string `json:"takenAt"` // RFC3339, UTC — renderer converts to KST
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Reason    string `json:"reason"`
	Tombstone bool   `json:"tombstone,omitempty"`
}

// ListResult is the RPC response shape for checkpoint.list.
type ListResult struct {
	SessionKey string          `json:"sessionKey"`
	Total      int             `json:"total"`
	Snapshots  []*SnapshotWire `json:"snapshots"`
}

// RestoreParams is the RPC argument shape for checkpoint.restore.
type RestoreParams struct {
	SessionKey string `json:"sessionKey"`
	ID         string `json:"id"`
}

// RestoreResult is the RPC response shape for checkpoint.restore.
type RestoreResult struct {
	SessionKey string        `json:"sessionKey"`
	Restored   *SnapshotWire `json:"restored"`
}

// DiffParams is the RPC argument shape for checkpoint.diff.
type DiffParams struct {
	SessionKey string `json:"sessionKey"`
	ID         string `json:"id"`
}

// DiffResult is the RPC response shape for checkpoint.diff.
type DiffResult struct {
	SessionKey string `json:"sessionKey"`
	ID         string `json:"id"`
	Path       string `json:"path"`
	Diff       string `json:"diff"` // unified diff, never truncated here (caller truncates for display)
}

// Methods returns the checkpoint domain handler map. Always returns a
// non-nil map so the three methods stay registered even when Root is empty.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return map[string]rpcutil.HandlerFunc{
		"checkpoint.list":    listHandler(deps),
		"checkpoint.restore": restoreHandler(deps),
		"checkpoint.diff":    diffHandler(deps),
	}
}

// ── Handlers ───────────────────────────────────────────────────────────────

func listHandler(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Root == "" {
			return rpcerr.Unavailable("checkpoints are disabled").Response(req.ID)
		}
		p, errResp := rpcutil.DecodeParams[ListParams](req)
		if errResp != nil {
			return errResp
		}
		sessionKey, resp := requireSessionKey(req.ID, p.SessionKey)
		if resp != nil {
			return resp
		}

		mgr := checkpoint.New(deps.Root, sessionKey)
		snaps, err := mgr.List(p.Path, p.Limit)
		if err != nil {
			deps.Logger.Error("checkpoint.list failed",
				"session", sessionKey, "path", p.Path, "error", err)
			return rpcerr.WrapDependencyFailed("checkpoint list failed", err).
				WithSession(sessionKey).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, &ListResult{
			SessionKey: sessionKey,
			Total:      len(snaps),
			Snapshots:  toWire(snaps),
		})
	}
}

func restoreHandler(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Root == "" {
			return rpcerr.Unavailable("checkpoints are disabled").Response(req.ID)
		}
		p, errResp := rpcutil.DecodeParams[RestoreParams](req)
		if errResp != nil {
			return errResp
		}
		sessionKey, resp := requireSessionKey(req.ID, p.SessionKey)
		if resp != nil {
			return resp
		}
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		mgr := checkpoint.New(deps.Root, sessionKey)
		snap, err := mgr.Restore(ctx, id)
		if err != nil {
			// Missing ID is a user error, not a service failure — surface it
			// as NOT_FOUND so the UI can render "그 체크포인트를 찾을 수 없어요".
			if isNotFound(err) {
				return rpcerr.NotFound("snapshot " + rpcutil.TruncateForError(id)).
					WithSession(sessionKey).Response(req.ID)
			}
			deps.Logger.Error("checkpoint.restore failed",
				"session", sessionKey, "id", id, "error", err)
			return rpcerr.WrapDependencyFailed("checkpoint restore failed", err).
				WithSession(sessionKey).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, &RestoreResult{
			SessionKey: sessionKey,
			Restored:   oneWire(snap),
		})
	}
}

func diffHandler(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Root == "" {
			return rpcerr.Unavailable("checkpoints are disabled").Response(req.ID)
		}
		p, errResp := rpcutil.DecodeParams[DiffParams](req)
		if errResp != nil {
			return errResp
		}
		sessionKey, resp := requireSessionKey(req.ID, p.SessionKey)
		if resp != nil {
			return resp
		}
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		mgr := checkpoint.New(deps.Root, sessionKey)

		// Look up the snapshot so we can echo back its path — Diff() itself
		// embeds the path in its header but callers benefit from having it
		// as a top-level field for rendering.
		all, err := mgr.List("", 0)
		if err != nil {
			deps.Logger.Error("checkpoint.diff list failed",
				"session", sessionKey, "id", id, "error", err)
			return rpcerr.WrapDependencyFailed("checkpoint list failed", err).
				WithSession(sessionKey).Response(req.ID)
		}
		var target *checkpoint.Snapshot
		for _, s := range all {
			if s.ID == id {
				target = s
				break
			}
		}
		if target == nil {
			return rpcerr.NotFound("snapshot " + rpcutil.TruncateForError(id)).
				WithSession(sessionKey).Response(req.ID)
		}

		diff, err := mgr.Diff(id)
		if err != nil {
			deps.Logger.Error("checkpoint.diff failed",
				"session", sessionKey, "id", id, "error", err)
			return rpcerr.WrapDependencyFailed("checkpoint diff failed", err).
				WithSession(sessionKey).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, &DiffResult{
			SessionKey: sessionKey,
			ID:         id,
			Path:       target.Path,
			Diff:       diff,
		})
	}
}

// ── Internals ──────────────────────────────────────────────────────────────

func requireSessionKey(reqID, raw string) (string, *protocol.ResponseFrame) {
	k := strings.TrimSpace(raw)
	if k == "" {
		return "", rpcerr.MissingParam("sessionKey").Response(reqID)
	}
	return k, nil
}

// isNotFound reports whether err is a "snapshot not found" error from
// checkpoint.Manager. The package exposes only a formatted string rather
// than a sentinel, so we match on the substring. Kept narrow so other
// failures (I/O, blob read) do not get silently reclassified.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "not found")
}

// toWire projects a []*checkpoint.Snapshot onto the wire format.
func toWire(in []*checkpoint.Snapshot) []*SnapshotWire {
	out := make([]*SnapshotWire, 0, len(in))
	for _, s := range in {
		out = append(out, oneWire(s))
	}
	return out
}

// oneWire projects a single snapshot. nil-safe.
func oneWire(s *checkpoint.Snapshot) *SnapshotWire {
	if s == nil {
		return nil
	}
	return &SnapshotWire{
		ID:        s.ID,
		Path:      s.Path,
		Seq:       s.Seq,
		TakenAt:   s.TakenAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Size:      s.Size,
		SHA256:    s.SHA256,
		Reason:    s.Reason,
		Tombstone: s.Tombstone,
	}
}
