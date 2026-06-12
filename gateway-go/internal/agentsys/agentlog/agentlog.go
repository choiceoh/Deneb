// Package agentlog provides detailed JSONL logging for AI agent runs.
//
// Each agent run records structured events (start, prep, turn, tool, end, error)
// to a per-session JSONL file under ~/.deneb/agent-logs/{sessionKey}.jsonl.
// The AI agent can query its own past run logs via the agent_logs tool
// to diagnose issues and understand prior execution context.
package agentlog

import "encoding/json"

// LogEntry is a single line in the agent log JSONL file.
type LogEntry struct {
	Ts      int64           `json:"ts"` //nolint:staticcheck // ST1003 — JSON field name
	Type    string          `json:"type"`
	RunID   string          `json:"runId"`
	Session string          `json:"session"`
	Data    json.RawMessage `json:"data"`
}

// Log entry types.
const (
	TypeRunStart = "run.start"
	TypeRunPrep  = "run.prep"
	TypeTurnLLM  = "turn.llm"
	TypeTurnTool = "turn.tool"
	TypeRunEnd   = "run.end"
	TypeRunError = "run.error"
	// TypeRunCache is an engine-side prefix-cache sample paired to a run via
	// runId, emitted asynchronously right after run.end (chat's
	// engine_cache_sample.go). Separate from run.end because the sample is a
	// best-effort HTTP scrape that must not delay the reply path.
	TypeRunCache = "run.cache"

	// Standalone behavioral events — not part of an agent run, emitted via
	// Writer.LogEvent under a system:* session key. They make the background /
	// autonomous layer (which has no per-run JSONL of its own) observable: what
	// it tried to do and what the outcome was.
	TypeProactiveRelay = "proactive.relay" // autonomous delivery decision (relayNative)
	TypeBackgroundJob  = "background.job"  // a background job cycle (cron, gmail/dropbox poll, heartbeat)
)

// Session keys for the standalone behavioral event streams. Each lands in its
// own JSONL so the funnel is easy to read in isolation.
const (
	SessionProactive  = "system:proactive"
	SessionBackground = "system:background"
)

// RunStartData records agent run initialization.
type RunStartData struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Message  string `json:"message"` // user input (truncated to maxMessageLen)
	Channel  string `json:"channel,omitempty"`
	// ThinkingLevel is the session's extended-thinking setting in effect for
	// this run ("low".."xhigh"); empty = thinking off. Lets per-model latency
	// analysis tell thinking-heavy runs apart from genuinely slow models.
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

// RunPrepData records context assembly metrics.
type RunPrepData struct {
	SystemPromptChars int   `json:"systemPromptChars"`
	ContextMessages   int   `json:"contextMessages"`
	PrepMs            int64 `json:"prepMs"`
	// RecallChars is the size of recall evidence injected during context prep
	// (server-side wiki/diary/transcript/polaris search). 0 means nothing prior
	// was recalled for this run — useful for measuring how often recall fires.
	RecallChars int `json:"recallChars,omitempty"`
}

// TurnLLMData records a single LLM turn result.
type TurnLLMData struct {
	Turn         int    `json:"turn"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	StopReason   string `json:"stopReason,omitempty"`
	TextLen      int    `json:"textLen"`
	ToolCalls    int    `json:"toolCalls"`
	// Cache effectiveness per turn — on Anthropic/OpenRouter the prompt-cache
	// prefix is reused across turns, so cacheRead should rise turn-over-turn
	// in a healthy multi-turn run. A turn that reads 0 cache mid-conversation
	// signals a cache break (see .claude/rules/prompt-cache.md).
	CacheReadTokens     int `json:"cacheReadTokens,omitempty"`
	CacheCreationTokens int `json:"cacheCreationTokens,omitempty"`
}

// TurnToolData records a single tool execution within a turn.
type TurnToolData struct {
	Turn       int    `json:"turn"`
	Name       string `json:"name"`
	DurationMs int64  `json:"durationMs"`
	OutputLen  int    `json:"outputLen"`
	IsError    bool   `json:"isError,omitempty"`
	Error      string `json:"error,omitempty"`
}

// RunEndData records agent run completion. Beyond the raw token/turn totals it
// captures the whole-run *shape* (which tools ran and how often, cache
// effectiveness, whether compaction fired, whether the run was proactive) so a
// later analysis pass can answer "what is this agent actually doing" without
// re-deriving it from per-turn lines.
type RunEndData struct {
	StopReason   string `json:"stopReason"`
	Turns        int    `json:"turns"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	TotalMs      int64  `json:"totalMs"`
	TextLen      int    `json:"textLen"`
	// Model is the model that actually produced the answer — it differs from
	// run.start's model only when the fallback chain fired. Aggregation keeps
	// attributing the run to the requested model (failures belong there) but
	// counts a FallbackRun when this differs, so per-model stats surface how
	// often a model needed rescuing.
	Model string `json:"model,omitempty"`
	// CacheReadTokens/CacheCreationTokens are run totals (summed across turns).
	// High read : low creation == healthy prompt-cache reuse.
	CacheReadTokens     int `json:"cacheReadTokens,omitempty"`
	CacheCreationTokens int `json:"cacheCreationTokens,omitempty"`
	// ToolCalls is the total tool_use blocks across the whole run; ToolCounts
	// is the per-tool histogram (name -> invocation count). The histogram is
	// the cross-session tool-usage aggregate's data source (Phase 3).
	ToolCalls  int            `json:"toolCalls,omitempty"`
	ToolCounts map[string]int `json:"toolCounts,omitempty"`
	// MaxTokensRecoveries counts how many times the run hit the output-token
	// ceiling and auto-retried — a signal the model is over-running its budget.
	MaxTokensRecoveries int `json:"maxTokensRecoveries,omitempty"`
	// Compacted is true when Polaris compaction fired during this run (the
	// context outgrew its budget). Proactive is true for autonomous/auto-
	// delivered runs (heartbeat self-trigger, cron relay) vs. a user request,
	// so analysis can separate the two populations.
	Compacted bool `json:"compacted,omitempty"`
	Proactive bool `json:"proactive,omitempty"`
	// EffortDecision is the adaptive effort router's verdict for this run
	// ("routed:short-conversational", "kept:hard-signal:왜", …; empty when the
	// router gate is closed — non-dual-mode model or DENEB_ADAPTIVE_EFFORT
	// off). EffortEscalated marks a routed run that failed non-thinking and
	// was retried once with thinking restored. The journald copy of these
	// fields rotates away; this is the durable feed for the v2 learned-router
	// label pipeline and modeltuner-style aggregation.
	EffortDecision  string `json:"effortDecision,omitempty"`
	EffortEscalated bool   `json:"effortEscalated,omitempty"`
}

// RunErrorData records agent run failure.
type RunErrorData struct {
	Error   string `json:"error"`
	Aborted bool   `json:"aborted,omitempty"`
}

// RunCacheData carries a self-hosted vLLM engine's prefix-cache (APC) counter
// delta sampled right after a run. The /metrics counters are engine-global
// and cumulative, so the delta since this gateway's previous sample
// approximates the run's own share under the single-user, mostly-serial
// workload; overlapping runs smear into whichever samples next. Token counts:
// EngineHitTokens/EngineQueryTokens ≈ the window's APC hit rate. This is the
// only per-turn cache signal on the vLLM path — the engine does not report
// cached_tokens in per-request usage (run.end's CacheReadTokens stays 0).
type RunCacheData struct {
	EngineHitTokens   int64  `json:"engineHitTokens"`
	EngineQueryTokens int64  `json:"engineQueryTokens"`
	MetricsURL        string `json:"metricsURL,omitempty"`
}

// ProactiveRelayData records one proactive delivery decision: what the
// autonomous layer (cron report, gmail summary, wiki dreaming) tried to push to
// the user and whether it landed. relayNative is the single choke point, so this
// captures the whole proactive funnel — how often it fires, how much is
// suppressed, and why (the over-notification the project actively fights).
type ProactiveRelayData struct {
	Decision   string `json:"decision"`         // delivered | suppressed | dropped | error
	Reason     string `json:"reason,omitempty"` // silent_token | contentless | no_transcript_store | append_failed
	ContentLen int    `json:"contentLen,omitempty"`
	Preview    string `json:"preview,omitempty"` // short preview for eyeballing
}

// BackgroundJobData records one cycle of a background worker (cron job, gmail /
// dropbox poll, heartbeat, autonomous tick). It answers "did this run, and what
// did it find/do" — the questions that went unanswered when cron jobs and
// pollers silently died in production.
type BackgroundJobData struct {
	Kind       string `json:"kind"`             // "cron" | "gmailpoll" | "dropboxpoll" | "heartbeat" | "autonomous"
	Name       string `json:"name,omitempty"`   // job/task name (e.g. cron job id, "morning-letter")
	Outcome    string `json:"outcome"`          // "ok" | "skipped" | "error" | "empty" | "delivered"
	Detail     string `json:"detail,omitempty"` // human-readable note (what was found / why skipped)
	Found      int    `json:"found,omitempty"`  // items found this cycle (mails, changes, …)
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
}
