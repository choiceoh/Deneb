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
//
// Implemented as a close-once channel: Request() writes the reason then closes
// the done channel (via sync.Once), establishing a happens-before guarantee
// so any goroutine that observes the close also sees the reason value.
type ContinuationSignal struct {
	once   sync.Once
	done   chan struct{}
	reason string
}

// NewContinuationSignal creates a new (unset) ContinuationSignal.
func NewContinuationSignal() *ContinuationSignal {
	return &ContinuationSignal{done: make(chan struct{})}
}

// Request marks the signal as requested with the given reason.
// Safe to call multiple times; only the first call takes effect.
func (s *ContinuationSignal) Request(reason string) {
	s.once.Do(func() {
		s.reason = reason
		close(s.done)
	})
}

// Requested reports whether continue_run was called (non-blocking).
func (s *ContinuationSignal) Requested() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Reason returns the continuation reason (empty if not requested).
func (s *ContinuationSignal) Reason() string {
	select {
	case <-s.done:
		return s.reason
	default:
		return ""
	}
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
// fetch_tools during a run. The fetch_tools tool sends names through a
// buffered channel from tool goroutines; the executor drains and accumulates
// them between turns via ActivatedNames(). The channel eliminates the need
// for a mutex: cross-goroutine transfer is handled by the channel send/receive,
// and the accumulated state (collected/seen) is only touched by the single
// executor goroutine.
type DeferredActivation struct {
	ch        chan []string
	collected []string
	seen      map[string]bool
}

// NewDeferredActivation creates a new (empty) DeferredActivation tracker.
func NewDeferredActivation() *DeferredActivation {
	return &DeferredActivation{
		ch:   make(chan []string, 16),
		seen: make(map[string]bool),
	}
}

// Activate marks the given tool names as activated.
// Called from tool goroutines; non-blocking.
func (d *DeferredActivation) Activate(names []string) {
	select {
	case d.ch <- names:
	default:
		// Buffer full — should not happen in practice (16 slots).
	}
}

// ActivatedNames drains pending activations and returns all activated tool names.
// Called from the executor goroutine between turns (single reader).
func (d *DeferredActivation) ActivatedNames() []string {
	for {
		select {
		case names := <-d.ch:
			for _, n := range names {
				if !d.seen[n] {
					d.seen[n] = true
					d.collected = append(d.collected, n)
				}
			}
		default:
			return d.collected
		}
	}
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
