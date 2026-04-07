package toolctx

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
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
	ctxKeyDeferredActivation
	ctxKeySpawnFlag
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

// --- SpawnFlag ---

// SpawnFlag is an atomic flag set by sessions_spawn when a sub-agent is created.
// The executor reads it to suppress turn-budget warnings that induce continue_run
// after spawning, and the continuation logic uses it to change the continuation
// message from "continue your work" to "synthesize subagent results".
type SpawnFlag struct {
	val atomic.Bool
}

// NewSpawnFlag creates a new (unset) SpawnFlag.
func NewSpawnFlag() *SpawnFlag { return &SpawnFlag{} }

// Set marks the flag as active (a sub-agent was spawned in this run).
func (f *SpawnFlag) Set() { f.val.Store(true) }

// IsSet reports whether sessions_spawn was called during this run.
func (f *SpawnFlag) IsSet() bool { return f.val.Load() }

// WithSpawnFlag attaches a SpawnFlag to the context.
func WithSpawnFlag(ctx context.Context, f *SpawnFlag) context.Context {
	return context.WithValue(ctx, ctxKeySpawnFlag, f)
}

// SpawnFlagFromContext extracts the SpawnFlag from a context.
func SpawnFlagFromContext(ctx context.Context) *SpawnFlag {
	f, _ := ctx.Value(ctxKeySpawnFlag).(*SpawnFlag)
	return f
}

// --- DeferredActivation ---

// DeferredActivation tracks which deferred tools have been activated via
// fetch_tools during a run. Thread-safe; the fetch_tools tool sets it from
// a tool goroutine, the executor reads it before each turn to inject
// activated tools into the ChatRequest.
type DeferredActivation struct {
	mu        sync.Mutex
	activated map[string]struct{}
}

// NewDeferredActivation creates a new (empty) DeferredActivation tracker.
func NewDeferredActivation() *DeferredActivation {
	return &DeferredActivation{activated: make(map[string]struct{})}
}

// Activate marks the given tool names as activated.
func (d *DeferredActivation) Activate(names []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, n := range names {
		d.activated[n] = struct{}{}
	}
}

// ActivatedNames returns the set of activated tool names.
func (d *DeferredActivation) ActivatedNames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.activated))
	for n := range d.activated {
		out = append(out, n)
	}
	return out
}

// WithDeferredActivation attaches a DeferredActivation to the context.
func WithDeferredActivation(ctx context.Context, da *DeferredActivation) context.Context {
	return context.WithValue(ctx, ctxKeyDeferredActivation, da)
}

// DeferredActivationFromContext extracts the DeferredActivation from a context.
func DeferredActivationFromContext(ctx context.Context) *DeferredActivation {
	da, _ := ctx.Value(ctxKeyDeferredActivation).(*DeferredActivation)
	return da
}
