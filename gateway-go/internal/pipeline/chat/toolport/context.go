package toolport

import (
	"context"
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
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
	ctxKeyDeferredActivation
	ctxKeySpawnFlag
	ctxKeyCheckpointer
	ctxKeyAutoDelivery
	ctxKeySkillConsult
	ctxKeyToolExecStats
	ctxKeyToolDryRun
	ctxKeyBlackboard
	ctxKeyTurnQuery
	ctxKeyWorkspaceDir
)

// WithWorkspaceDir pins the directory a run's tools work in, overriding the
// one they were registered with. Tools are registered ONCE at handler
// construction (chat_pipeline.go), so without this a per-request workspace
// could steer the prompt's context files but never the commands — exec kept
// running in the server-wide workspace no matter what the request asked for.
//
// Set only when a request names a workspace explicitly: the prompt path and the
// tool-registration path resolve their defaults through different helpers
// (leafbind vs configresolve), so defaulting here could silently move where
// commands run.
func WithWorkspaceDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, ctxKeyWorkspaceDir, dir)
}

// WorkspaceDirFromContext returns the run-scoped workspace, or "" when the run
// did not name one (the caller then keeps its registered default).
func WorkspaceDirFromContext(ctx context.Context) string {
	dir, _ := ctx.Value(ctxKeyWorkspaceDir).(string)
	return dir
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

// WithAutoDelivery marks the context of a run whose final reply text is
// delivered to the user's channel by the run-completion layer (e.g. the cron
// delivery layer), not by the agent's own in-loop message tool. The message
// tool reads this flag to turn a send-guard failure into a benign no-op
// instead of an error — see ToolMessage.
func WithAutoDelivery(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyAutoDelivery, true)
}

// AutoDeliveryFromContext reports whether this run's output is auto-delivered
// by the run-completion layer. Defaults to false.
func AutoDeliveryFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyAutoDelivery).(bool)
	return v
}

// WithToolDryRun marks the context of a run in which side-effect tools must
// not execute. The chat ToolRegistry consults this before dispatch: tools on
// its read-only allowlist run normally; everything else returns a stub result
// without invoking the tool fn. For eval/replay harnesses (behavioral skill
// replay, prompt-regression turns) that need the real tool loop without the
// real writes/sends.
func WithToolDryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyToolDryRun, true)
}

// ToolDryRunFromContext reports whether side-effect tools are suppressed for
// this run. Defaults to false.
func ToolDryRunFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyToolDryRun).(bool)
	return v
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

// WithBlackboard attaches a run-scoped typed blackboard for multi-tool I/O.
func WithBlackboard(ctx context.Context, board *Blackboard) context.Context {
	return context.WithValue(ctx, ctxKeyBlackboard, board)
}

// BlackboardFromContext extracts the Blackboard from a context.
func BlackboardFromContext(ctx context.Context) *Blackboard {
	board, _ := ctx.Value(ctxKeyBlackboard).(*Blackboard)
	return board
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

// --- Checkpointer ---

// Checkpointer is a narrow interface for snapshotting a file prior to an
// edit. Implemented by *pkg/checkpoint.CheckpointerAdapter (which wraps a
// Manager). Defining it in toolport/ avoids toolport/ depending on pkg/checkpoint.
//
// Snapshot is invoked BEFORE the tool rewrites the target file; an error-free
// return guarantees the agent can roll back the impending edit.
type Checkpointer interface {
	Snapshot(ctx context.Context, path string, reason string) error
}

// WithCheckpointer attaches a Checkpointer to the context. Tools like
// write/edit call Snapshot before modifying the file so the user can
// /rollback to the prior state within the session.
func WithCheckpointer(ctx context.Context, cp Checkpointer) context.Context {
	if cp == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyCheckpointer, cp)
}

// CheckpointerFromContext extracts the Checkpointer from a context.
// Returns nil if none was attached (in which case tools should skip
// snapshotting but still perform the write).
func CheckpointerFromContext(ctx context.Context) Checkpointer {
	cp, _ := ctx.Value(ctxKeyCheckpointer).(Checkpointer)
	return cp
}

// --- SpawnFlag ---

// SpawnFlag is an atomic flag set by sessions_spawn when a sub-agent is created.
// The executor reads it to suppress turn-budget warnings after spawning so the
// agent yields to the notification system instead of requesting more turns.
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
//
// active is a read-only snapshot of seen, republished by ActivatedNames()
// (executor goroutine) whenever the set grows, so tool goroutines can ask
// IsActive without touching seen. Production measurement (2026-07-05, 14d
// agent-logs): 20% of fetch_tools calls were same-input repeats inside one
// run — IsActive lets fetch_tools short-circuit those instead of re-emitting
// the full schema into history. The snapshot updates between turns, so a
// duplicate within the same turn still returns the schema (harmless).
type DeferredActivation struct {
	ch        chan []string
	collected []string
	seen      map[string]bool
	active    atomic.Value // map[string]bool — immutable snapshot of seen
}

// NewDeferredActivation creates a new (empty) DeferredActivation tracker.
func NewDeferredActivation() *DeferredActivation {
	return &DeferredActivation{
		ch:   make(chan []string, 16),
		seen: make(map[string]bool),
	}
}

// Seed marks tool names as already activated BEFORE the run starts — used by
// the history replay (chat/deferred_replay.go) to carry activation state
// across runs. Unlike Activate it writes the executor-owned state directly and
// publishes the IsActive snapshot immediately, so tools can short-circuit from
// turn 0. Must only be called before the agent loop spawns tool goroutines.
func (d *DeferredActivation) Seed(names []string) {
	for _, n := range names {
		if !d.seen[n] {
			d.seen[n] = true
			d.collected = append(d.collected, n)
		}
	}
	snapshot := make(map[string]bool, len(d.seen))
	for n := range d.seen {
		snapshot[n] = true
	}
	d.active.Store(snapshot)
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
	grew := false
	for {
		select {
		case names := <-d.ch:
			for _, n := range names {
				if !d.seen[n] {
					d.seen[n] = true
					d.collected = append(d.collected, n)
					grew = true
				}
			}
		default:
			if grew {
				snapshot := make(map[string]bool, len(d.seen))
				for n := range d.seen {
					snapshot[n] = true
				}
				d.active.Store(snapshot)
			}
			return d.collected
		}
	}
}

// IsActive reports whether the named tool has already been activated (as of
// the last ActivatedNames drain). Safe to call from tool goroutines — it only
// reads the immutable snapshot, never the executor-owned seen map.
func (d *DeferredActivation) IsActive(name string) bool {
	m, _ := d.active.Load().(map[string]bool)
	return m[name]
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

// WithTurnQuery attaches the turn's user message so relevance-aware tool
// post-processing (grep overflow rerank) can score output against what the
// turn is actually about. Plain string, read-only.
func WithTurnQuery(ctx context.Context, query string) context.Context {
	return context.WithValue(ctx, ctxKeyTurnQuery, query)
}

// TurnQueryFromContext returns the turn's user message, or "".
func TurnQueryFromContext(ctx context.Context) string {
	q, _ := ctx.Value(ctxKeyTurnQuery).(string)
	return q
}
