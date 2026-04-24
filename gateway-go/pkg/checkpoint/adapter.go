package checkpoint

import "context"

// ToolAdapter wraps a *Manager so it satisfies the narrow Checkpointer
// interface defined in internal/pipeline/chat/toolctx/ (which doesn't want
// to pull the richer *Snapshot return type into its public surface).
//
// Construct with NewToolAdapter and attach via toolctx.WithCheckpointer.
type ToolAdapter struct{ M *Manager }

// NewToolAdapter returns a Checkpointer adapter for m. Returns nil if m is
// nil so callers can funnel through toolctx.WithCheckpointer without extra
// guards.
func NewToolAdapter(m *Manager) *ToolAdapter {
	if m == nil {
		return nil
	}
	return &ToolAdapter{M: m}
}

// Snapshot forwards to Manager.Snapshot and discards the returned record.
func (a *ToolAdapter) Snapshot(ctx context.Context, path, reason string) error {
	if a == nil || a.M == nil {
		return nil
	}
	_, err := a.M.Snapshot(ctx, path, reason)
	return err
}
