package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Context helpers — delegate to toolport/ (the canonical definitions).
// These wrappers preserve backward compatibility for callers within chat/.

// WithDeliveryContext attaches a DeliveryContext to ctx.
func WithDeliveryContext(ctx context.Context, d *DeliveryContext) context.Context {
	return toolport.WithDeliveryContext(ctx, d)
}

// WithReplyFunc attaches a ReplyFunc to ctx.
func WithReplyFunc(ctx context.Context, fn ReplyFunc) context.Context {
	return toolport.WithReplyFunc(ctx, fn)
}

// WithAutoDelivery marks a run whose final reply text is delivered by the
// run-completion layer rather than the agent's in-loop message tool.
func WithAutoDelivery(ctx context.Context) context.Context {
	return toolport.WithAutoDelivery(ctx)
}

// WithSessionKey attaches the session key to ctx.
func WithSessionKey(ctx context.Context, key string) context.Context {
	return toolport.WithSessionKey(ctx, key)
}

// WithMediaSendFunc attaches a MediaSendFunc to ctx.
func WithMediaSendFunc(ctx context.Context, fn MediaSendFunc) context.Context {
	return toolport.WithMediaSendFunc(ctx, fn)
}

// WithMaxUploadBytes attaches the channel-specific file upload limit to ctx.
func WithMaxUploadBytes(ctx context.Context, n int64) context.Context {
	return toolport.WithMaxUploadBytes(ctx, n)
}

// WithTurnContext attaches a TurnContext to ctx for cross-tool result sharing.
func WithTurnContext(ctx context.Context, tc *TurnContext) context.Context {
	return toolport.WithTurnContext(ctx, tc)
}

// TurnContextFromContext extracts the TurnContext from ctx. Returns nil if not set.
func TurnContextFromContext(ctx context.Context) *TurnContext {
	return toolport.TurnContextFromContext(ctx)
}

// WithRunCache attaches a RunCache to ctx for cross-turn result caching.
func WithRunCache(ctx context.Context, rc *RunCache) context.Context {
	return toolport.WithRunCache(ctx, rc)
}

// RunCacheFromContext extracts the RunCache from ctx. Returns nil if not set.
func RunCacheFromContext(ctx context.Context) *RunCache {
	return toolport.RunCacheFromContext(ctx)
}

// WithBlackboard attaches a run-scoped typed blackboard to ctx.
func WithBlackboard(ctx context.Context, board *toolport.Blackboard) context.Context {
	return toolport.WithBlackboard(ctx, board)
}

// BlackboardFromContext extracts the Blackboard from ctx. Returns nil if not set.
func BlackboardFromContext(ctx context.Context) *toolport.Blackboard {
	return toolport.BlackboardFromContext(ctx)
}

// NewBlackboard creates an empty typed blackboard.
func NewBlackboard() *toolport.Blackboard {
	return toolport.NewBlackboard()
}

// WithFileCache attaches a FileCache to ctx for cross-turn file read dedup.
func WithFileCache(ctx context.Context, fc *agent.FileCache) context.Context {
	return toolport.WithFileCache(ctx, fc)
}

// WithToolPreset attaches a tool preset to ctx for execution-time enforcement.
func WithToolPreset(ctx context.Context, preset string) context.Context {
	return toolport.WithToolPreset(ctx, preset)
}

// SpawnFlag is a re-export of toolport.SpawnFlag.
type SpawnFlag = toolport.SpawnFlag

// NewSpawnFlag creates a new (unset) SpawnFlag.
func NewSpawnFlag() *SpawnFlag {
	return toolport.NewSpawnFlag()
}

// WithSpawnFlag attaches a SpawnFlag to ctx.
func WithSpawnFlag(ctx context.Context, f *SpawnFlag) context.Context {
	return toolport.WithSpawnFlag(ctx, f)
}

// DeferredActivation is a re-export of toolport.DeferredActivation.
type DeferredActivation = toolport.DeferredActivation

// NewDeferredActivation creates a new (empty) DeferredActivation tracker.
func NewDeferredActivation() *DeferredActivation {
	return toolport.NewDeferredActivation()
}

// WithDeferredActivation attaches a DeferredActivation to ctx.
func WithDeferredActivation(ctx context.Context, da *DeferredActivation) context.Context {
	return toolport.WithDeferredActivation(ctx, da)
}
