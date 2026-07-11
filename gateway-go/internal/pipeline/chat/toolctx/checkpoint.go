package toolctx

import (
	"context"
	"log/slog"
)

// SnapshotBeforeWrite takes a best-effort pre-edit checkpoint when ctx carries
// a Checkpointer. Snapshot failures are logged but never block the write.
func SnapshotBeforeWrite(ctx context.Context, path, reason string) {
	cp := CheckpointerFromContext(ctx)
	if cp == nil {
		return
	}
	if err := cp.Snapshot(ctx, path, reason); err != nil {
		slog.Error("checkpoint snapshot failed; rollback unavailable for edit",
			"path", path, "reason", reason, "error", err)
	}
}
