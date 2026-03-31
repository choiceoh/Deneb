package toolctx

import "context"

// contextKey is an unexported type for context value keys in this package.
type contextKey int

const (
	ctxKeyDelivery contextKey = iota
	ctxKeyReplyFunc
	ctxKeySessionKey
	ctxKeyMediaSendFunc
	ctxKeyTurnContext
	ctxKeyMaxUploadBytes
	ctxKeyRunCache
	ctxKeyCodingUpgrade
)

// CodingUpgradeSignal is called by the enable_coding_tools tool to request
// a mid-run switch to the full coding tool set.
type CodingUpgradeSignal func()

// WithCodingUpgradeSignal attaches a coding-upgrade signal function to the context.
func WithCodingUpgradeSignal(ctx context.Context, fn CodingUpgradeSignal) context.Context {
	return context.WithValue(ctx, ctxKeyCodingUpgrade, fn)
}

// CodingUpgradeSignalFromContext extracts the coding-upgrade signal function.
func CodingUpgradeSignalFromContext(ctx context.Context) CodingUpgradeSignal {
	fn, _ := ctx.Value(ctxKeyCodingUpgrade).(CodingUpgradeSignal)
	return fn
}

// WithDeliveryContext attaches a DeliveryContext to the context.
func WithDeliveryContext(ctx context.Context, d *DeliveryContext) context.Context {
	return context.WithValue(ctx, ctxKeyDelivery, d)
}

// DeliveryFromContext extracts the DeliveryContext from a context.
func DeliveryFromContext(ctx context.Context) *DeliveryContext {
	d, _ := ctx.Value(ctxKeyDelivery).(*DeliveryContext)
	return d
}

// WithReplyFunc attaches a ReplyFunc to the context.
func WithReplyFunc(ctx context.Context, fn ReplyFunc) context.Context {
	return context.WithValue(ctx, ctxKeyReplyFunc, fn)
}

// ReplyFuncFromContext extracts the ReplyFunc from a context.
func ReplyFuncFromContext(ctx context.Context) ReplyFunc {
	fn, _ := ctx.Value(ctxKeyReplyFunc).(ReplyFunc)
	return fn
}

// WithSessionKey attaches the session key to the context.
func WithSessionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxKeySessionKey, key)
}

// SessionKeyFromContext extracts the session key from a context.
func SessionKeyFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ctxKeySessionKey).(string)
	return s
}

// WithMediaSendFunc attaches a MediaSendFunc to the context.
func WithMediaSendFunc(ctx context.Context, fn MediaSendFunc) context.Context {
	return context.WithValue(ctx, ctxKeyMediaSendFunc, fn)
}

// MediaSendFuncFromContext extracts the MediaSendFunc from a context.
func MediaSendFuncFromContext(ctx context.Context) MediaSendFunc {
	fn, _ := ctx.Value(ctxKeyMediaSendFunc).(MediaSendFunc)
	return fn
}

// WithMaxUploadBytes attaches the channel-specific file upload limit to the context.
func WithMaxUploadBytes(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, ctxKeyMaxUploadBytes, n)
}

// MaxUploadBytesFromContext returns the channel-specific upload limit.
// Returns 0 if not set (caller should apply a safe default).
func MaxUploadBytesFromContext(ctx context.Context) int64 {
	n, _ := ctx.Value(ctxKeyMaxUploadBytes).(int64)
	return n
}

// WithTurnContext attaches a TurnContext to the context for cross-tool result sharing.
func WithTurnContext(ctx context.Context, tc *TurnContext) context.Context {
	return context.WithValue(ctx, ctxKeyTurnContext, tc)
}

// TurnContextFromContext extracts the TurnContext from a context.
func TurnContextFromContext(ctx context.Context) *TurnContext {
	tc, _ := ctx.Value(ctxKeyTurnContext).(*TurnContext)
	return tc
}

// WithRunCache attaches a RunCache to the context for cross-turn result caching.
func WithRunCache(ctx context.Context, rc *RunCache) context.Context {
	return context.WithValue(ctx, ctxKeyRunCache, rc)
}

// RunCacheFromContext extracts the RunCache from a context.
func RunCacheFromContext(ctx context.Context) *RunCache {
	rc, _ := ctx.Value(ctxKeyRunCache).(*RunCache)
	return rc
}
