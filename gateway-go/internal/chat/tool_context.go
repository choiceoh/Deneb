package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Context helpers — delegate to toolctx/ (the canonical definitions).
// These wrappers preserve backward compatibility for callers within chat/.

func WithDeliveryContext(ctx context.Context, d *DeliveryContext) context.Context {
	return toolctx.WithDeliveryContext(ctx, d)
}

func DeliveryFromContext(ctx context.Context) *DeliveryContext {
	return toolctx.DeliveryFromContext(ctx)
}

func WithReplyFunc(ctx context.Context, fn ReplyFunc) context.Context {
	return toolctx.WithReplyFunc(ctx, fn)
}

func ReplyFuncFromContext(ctx context.Context) ReplyFunc {
	return toolctx.ReplyFuncFromContext(ctx)
}

func WithSessionKey(ctx context.Context, key string) context.Context {
	return toolctx.WithSessionKey(ctx, key)
}

func SessionKeyFromContext(ctx context.Context) string {
	return toolctx.SessionKeyFromContext(ctx)
}

func WithMediaSendFunc(ctx context.Context, fn MediaSendFunc) context.Context {
	return toolctx.WithMediaSendFunc(ctx, fn)
}

func MediaSendFuncFromContext(ctx context.Context) MediaSendFunc {
	return toolctx.MediaSendFuncFromContext(ctx)
}

func WithMaxUploadBytes(ctx context.Context, n int64) context.Context {
	return toolctx.WithMaxUploadBytes(ctx, n)
}

func MaxUploadBytesFromContext(ctx context.Context) int64 {
	return toolctx.MaxUploadBytesFromContext(ctx)
}

func WithTurnContext(ctx context.Context, tc *TurnContext) context.Context {
	return toolctx.WithTurnContext(ctx, tc)
}

func TurnContextFromContext(ctx context.Context) *TurnContext {
	return toolctx.TurnContextFromContext(ctx)
}

func WithRunCache(ctx context.Context, rc *RunCache) context.Context {
	return toolctx.WithRunCache(ctx, rc)
}

func RunCacheFromContext(ctx context.Context) *RunCache {
	return toolctx.RunCacheFromContext(ctx)
}
