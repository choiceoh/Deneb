package toolctx

import (
	"context"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
)

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
	ctxKeyFileCache
	ctxKeyToolPreset
	ctxKeyContinuationSignal
)

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

// WithFileCache attaches a FileCache to the context for cross-turn file read dedup.
func WithFileCache(ctx context.Context, fc *agent.FileCache) context.Context {
	return context.WithValue(ctx, ctxKeyFileCache, fc)
}

// FileCacheFromContext extracts the FileCache from a context.
func FileCacheFromContext(ctx context.Context) *agent.FileCache {
	fc, _ := ctx.Value(ctxKeyFileCache).(*agent.FileCache)
	return fc
}

// WithToolPreset attaches a tool preset string to the context.
// Used by Execute() to enforce tool restrictions at execution time.
func WithToolPreset(ctx context.Context, preset string) context.Context {
	if preset == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyToolPreset, preset)
}

// ToolPresetFromContext extracts the tool preset from a context.
func ToolPresetFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ctxKeyToolPreset).(string)
	return s
}

// --- ContinuationSignal ---

// ContinuationSignal is a shared flag set by the continue_run tool to signal
// that the LLM wants a new agent run to start after the current one completes.
// Thread-safe; the tool sets it from a tool goroutine, the run orchestrator
// reads it after the agent loop returns.
type ContinuationSignal struct {
	mu        sync.Mutex
	requested bool
	reason    string
}

// NewContinuationSignal creates a new (unset) ContinuationSignal.
func NewContinuationSignal() *ContinuationSignal { return &ContinuationSignal{} }

// Request marks the signal as requested with the given reason.
func (s *ContinuationSignal) Request(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requested = true
	s.reason = reason
}

// Requested reports whether continue_run was called.
func (s *ContinuationSignal) Requested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requested
}

// Reason returns the continuation reason (empty if not requested).
func (s *ContinuationSignal) Reason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

// WithContinuationSignal attaches a ContinuationSignal to the context.
func WithContinuationSignal(ctx context.Context, sig *ContinuationSignal) context.Context {
	return context.WithValue(ctx, ctxKeyContinuationSignal, sig)
}

// ContinuationSignalFromContext extracts the ContinuationSignal from a context.
func ContinuationSignalFromContext(ctx context.Context) *ContinuationSignal {
	s, _ := ctx.Value(ctxKeyContinuationSignal).(*ContinuationSignal)
	return s
}
