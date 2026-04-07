package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Context helpers — delegate to toolctx/ (the canonical definitions).
// These wrappers preserve backward compatibility for callers within chat/.

// WithDeliveryContext attaches a DeliveryContext to ctx.
func WithDeliveryContext(ctx context.Context, d *DeliveryContext) context.Context {
	return toolctx.WithDeliveryContext(ctx, d)
}

// DeliveryFromContext extracts the DeliveryContext from ctx. Returns nil if not set.
func DeliveryFromContext(ctx context.Context) *DeliveryContext {
	return toolctx.DeliveryFromContext(ctx)
}

// WithReplyFunc attaches a ReplyFunc to ctx.
func WithReplyFunc(ctx context.Context, fn ReplyFunc) context.Context {
	return toolctx.WithReplyFunc(ctx, fn)
}

// ReplyFuncFromContext extracts the ReplyFunc from ctx. Returns nil if not set.
func ReplyFuncFromContext(ctx context.Context) ReplyFunc {
	return toolctx.ReplyFuncFromContext(ctx)
}

// WithSessionKey attaches the session key to ctx.
func WithSessionKey(ctx context.Context, key string) context.Context {
	return toolctx.WithSessionKey(ctx, key)
}

// SessionKeyFromContext extracts the session key from ctx. Returns "" if not set.
func SessionKeyFromContext(ctx context.Context) string {
	return toolctx.SessionKeyFromContext(ctx)
}

// WithMediaSendFunc attaches a MediaSendFunc to ctx.
func WithMediaSendFunc(ctx context.Context, fn MediaSendFunc) context.Context {
	return toolctx.WithMediaSendFunc(ctx, fn)
}

// MediaSendFuncFromContext extracts the MediaSendFunc from ctx. Returns nil if not set.
func MediaSendFuncFromContext(ctx context.Context) MediaSendFunc {
	return toolctx.MediaSendFuncFromContext(ctx)
}

// WithMaxUploadBytes attaches the channel-specific file upload limit to ctx.
func WithMaxUploadBytes(ctx context.Context, n int64) context.Context {
	return toolctx.WithMaxUploadBytes(ctx, n)
}

// MaxUploadBytesFromContext returns the channel-specific upload limit.
// Returns 0 if not set; callers should apply a safe default.
func MaxUploadBytesFromContext(ctx context.Context) int64 {
	return toolctx.MaxUploadBytesFromContext(ctx)
}

// WithTurnContext attaches a TurnContext to ctx for cross-tool result sharing.
func WithTurnContext(ctx context.Context, tc *TurnContext) context.Context {
	return toolctx.WithTurnContext(ctx, tc)
}

// TurnContextFromContext extracts the TurnContext from ctx. Returns nil if not set.
func TurnContextFromContext(ctx context.Context) *TurnContext {
	return toolctx.TurnContextFromContext(ctx)
}

// WithRunCache attaches a RunCache to ctx for cross-turn result caching.
func WithRunCache(ctx context.Context, rc *RunCache) context.Context {
	return toolctx.WithRunCache(ctx, rc)
}

// RunCacheFromContext extracts the RunCache from ctx. Returns nil if not set.
func RunCacheFromContext(ctx context.Context) *RunCache {
	return toolctx.RunCacheFromContext(ctx)
}

// WithFileCache attaches a FileCache to ctx for cross-turn file read dedup.
func WithFileCache(ctx context.Context, fc *agent.FileCache) context.Context {
	return toolctx.WithFileCache(ctx, fc)
}

// WithToolPreset attaches a tool preset to ctx for execution-time enforcement.
func WithToolPreset(ctx context.Context, preset string) context.Context {
	return toolctx.WithToolPreset(ctx, preset)
}

// ContinuationSignal is a re-export of toolctx.ContinuationSignal.
type ContinuationSignal = toolctx.ContinuationSignal

// NewContinuationSignal creates a new (unset) ContinuationSignal.
func NewContinuationSignal() *ContinuationSignal {
	return toolctx.NewContinuationSignal()
}

// WithContinuationSignal attaches a ContinuationSignal to ctx.
func WithContinuationSignal(ctx context.Context, sig *ContinuationSignal) context.Context {
	return toolctx.WithContinuationSignal(ctx, sig)
}

// SpawnFlag is a re-export of toolctx.SpawnFlag.
type SpawnFlag = toolctx.SpawnFlag

// NewSpawnFlag creates a new (unset) SpawnFlag.
func NewSpawnFlag() *SpawnFlag {
	return toolctx.NewSpawnFlag()
}

// WithSpawnFlag attaches a SpawnFlag to ctx.
func WithSpawnFlag(ctx context.Context, f *SpawnFlag) context.Context {
	return toolctx.WithSpawnFlag(ctx, f)
}

// DeferredActivation is a re-export of toolctx.DeferredActivation.
type DeferredActivation = toolctx.DeferredActivation

// NewDeferredActivation creates a new (empty) DeferredActivation tracker.
func NewDeferredActivation() *DeferredActivation {
	return toolctx.NewDeferredActivation()
}

// WithDeferredActivation attaches a DeferredActivation to ctx.
func WithDeferredActivation(ctx context.Context, da *DeferredActivation) context.Context {
	return toolctx.WithDeferredActivation(ctx, da)
}
