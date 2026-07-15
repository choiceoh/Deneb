package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Context helpers — delegate to toolport/ (the canonical definitions).
// These wrappers preserve backward compatibility for callers within chat/.

// withDeliveryContext attaches a DeliveryContext to ctx.
func withDeliveryContext(ctx context.Context, d *DeliveryContext) context.Context {
	return toolport.WithDeliveryContext(ctx, d)
}

// withReplyFunc attaches a replyFunc to ctx.
func withReplyFunc(ctx context.Context, fn replyFunc) context.Context {
	return toolport.WithReplyFunc(ctx, fn)
}

// withAutoDelivery marks a run whose final reply text is delivered by the
// run-completion layer rather than the agent's in-loop message tool.
func withAutoDelivery(ctx context.Context) context.Context {
	return toolport.WithAutoDelivery(ctx)
}

// withSessionKey attaches the session key to ctx.
func withSessionKey(ctx context.Context, key string) context.Context {
	return toolport.WithSessionKey(ctx, key)
}

// withMediaSendFunc attaches a mediaSendFunc to ctx.
func withMediaSendFunc(ctx context.Context, fn mediaSendFunc) context.Context {
	return toolport.WithMediaSendFunc(ctx, fn)
}

// withMaxUploadBytes attaches the channel-specific file upload limit to ctx.
func withMaxUploadBytes(ctx context.Context, n int64) context.Context {
	return toolport.WithMaxUploadBytes(ctx, n)
}

// withTurnContext attaches a TurnContext to ctx for cross-tool result sharing.
func withTurnContext(ctx context.Context, tc *TurnContext) context.Context {
	return toolport.WithTurnContext(ctx, tc)
}

// turnContextFromContext extracts the TurnContext from ctx. Returns nil if not set.
func turnContextFromContext(ctx context.Context) *TurnContext {
	return toolport.TurnContextFromContext(ctx)
}

// withRunCache attaches a runCache to ctx for cross-turn result caching.
func withRunCache(ctx context.Context, rc *runCache) context.Context {
	return toolport.WithRunCache(ctx, rc)
}

// runCacheFromContext extracts the runCache from ctx. Returns nil if not set.
func runCacheFromContext(ctx context.Context) *runCache {
	return toolport.RunCacheFromContext(ctx)
}

// withFileCache attaches a FileCache to ctx for cross-turn file read dedup.
func withFileCache(ctx context.Context, fc *agent.FileCache) context.Context {
	return toolport.WithFileCache(ctx, fc)
}

// WithToolPreset attaches a tool preset to ctx for execution-time enforcement.
func WithToolPreset(ctx context.Context, preset string) context.Context {
	return toolport.WithToolPreset(ctx, preset)
}

// spawnFlag is a re-export of toolport.SpawnFlag.
type spawnFlag = toolport.SpawnFlag

// newSpawnFlag creates a new (unset) spawnFlag.
func newSpawnFlag() *spawnFlag {
	return toolport.NewSpawnFlag()
}

// withSpawnFlag attaches a spawnFlag to ctx.
func withSpawnFlag(ctx context.Context, f *spawnFlag) context.Context {
	return toolport.WithSpawnFlag(ctx, f)
}

// deferredActivation is a re-export of toolport.DeferredActivation.
type deferredActivation = toolport.DeferredActivation

// newDeferredActivation creates a new (empty) deferredActivation tracker.
func newDeferredActivation() *deferredActivation {
	return toolport.NewDeferredActivation()
}

// withDeferredActivation attaches a deferredActivation to ctx.
func withDeferredActivation(ctx context.Context, da *deferredActivation) context.Context {
	return toolport.WithDeferredActivation(ctx, da)
}
