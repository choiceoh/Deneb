// run_agent_config.go — agent.RunConfig assembly for one chat turn:
// buildAgentConfig (tools, budgets, hooks, persister), extended-thinking
// resolution (session level -> llm.ThinkingConfig, reasoning sandwich),
// transcript persistence (buildMessagePersister, NO_REPLY sanitizing),
// and skill nudger/usage plumbing. Called from executeAgentRun (run_exec.go).
package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

type agentConfigDeps struct {
	Tools            *ToolRegistry
	MaxTokens        int
	SubagentNotifyCh <-chan string
	EmitAgentFn      func(kind, sessionKey, runID string, payload map[string]any)
	Transcript       TranscriptStore
	// SkillNudger fires background skill reviews after every N tool
	// invocations. Nil disables iteration-based nudging.
	SkillNudger SkillNudger
	// SkillUsageRecorder attributes each turn's outcome to the skills consulted
	// that turn, feeding the genesis Evolver's success-rate gate. Nil disables.
	SkillUsageRecorder SkillUsageRecorder
}

// buildAgentConfig constructs the agent.AgentConfig, building tool lists and
// wiring all turn-level hooks. Returns the config along with the spawn flag
// for the run orchestrator.
func buildAgentConfig(
	params RunParams,
	deps runDeps,
	cachedSession *session.Session,
	systemPrompt json.RawMessage,
	sessionToolPreset string,
	acd agentConfigDeps,
	logger *slog.Logger,
) (cfg agent.AgentConfig, spawnFlag *SpawnFlag) {
	// Build tool list from registry (uses stored descriptions and schemas).
	// If a tool preset is active, filter the tool list to only include allowed tools.
	var tools []llm.Tool
	if acd.Tools != nil {
		allowed := toolpreset.AllowedTools(toolpreset.Preset(sessionToolPreset))
		rawTools := acd.Tools.FilteredLLMTools(allowed)

		// Cache-stable ordering: built-in tools form a sorted prefix,
		// dynamic tools (plugins, MCP) are sorted separately and appended.
		builtinNames := make(map[string]struct{}, len(acd.Tools.Names()))
		for _, name := range acd.Tools.Names() {
			builtinNames[name] = struct{}{}
		}
		partition := PartitionTools(rawTools, builtinNames)
		tools = partition.MergedTools()
	}

	maxTokens := acd.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// RunCache lives for the entire agent run (across all turns) and caches
	// idempotent tool results (find, tree). Invalidated on mutation tools.
	runCache := NewRunCache()

	// skillConsults records which skills the agent reads during this run so the
	// post-turn hook can attribute each turn's outcome to them (genesis usage
	// signal). Run-scoped; shared with the skills tool via OnTurnInit.
	skillConsults := NewSkillConsultLog()

	// FileCache lives for the entire agent run and deduplicates repeated file reads.
	fileCache := agent.NewFileCache(agent.DefaultFileCacheMaxItems)

	// SpawnFlag: tracks whether sessions_spawn was called during this run.
	spawnFlag = NewSpawnFlag()

	// Verification gate state: armed by successful write/edit, disarmed by a
	// successful build/test exec; consulted when the model tries to finish.
	verifyGate := &verifyGateState{}

	// DeferredActivation: tracks which deferred tools have been activated via
	// fetch_tools during this run.
	deferredActivation := NewDeferredActivation()

	// Resolve thinking config from the session's ThinkingLevel setting.
	var thinkingCfg *llm.ThinkingConfig
	if cachedSession != nil && cachedSession.ThinkingLevel != "" {
		thinkingCfg = resolveThinkingConfig(cachedSession.ThinkingLevel)
	}
	// Interleaved thinking is an additive flag: it requires extended thinking
	// to be enabled (otherwise there's nothing to interleave). When
	// thinkingCfg is nil the interleaved bit has no effect.
	if thinkingCfg != nil && cachedSession != nil && cachedSession.InterleavedThinking != nil && *cachedSession.InterleavedThinking {
		thinkingCfg.Interleaved = true
	}

	// Override max tokens if the caller (e.g., OpenAI HTTP endpoint) specified one.
	if params.MaxTokens != nil && *params.MaxTokens > 0 {
		maxTokens = *params.MaxTokens
	}

	// Mode-aware agent config: Chat mode gets reduced limits for quick
	// conversational replies; other modes use the default agent capabilities.
	// Cron runs get the most generous budget — they can (a) deliver the
	// primary message, (b) post one or two short progress updates via
	// message.send so the user is not silently waiting, and (c) still update
	// wikis / projects without truncating. 50 turns is the current ceiling;
	// keep this high only while the cron-side progress-reporting rule in the
	// job prompts stays active, otherwise the user perceives the run as hung.
	maxTurns := defaultMaxTurns         // 25
	agentTimeout := defaultAgentTimeout // 60min
	if cachedSession != nil {
		switch {
		case cachedSession.Mode == session.ModeChat:
			maxTurns = 10
			agentTimeout = 10 * time.Minute
		case cachedSession.SpawnedBy != "":
			// Sub-agents are scoped delegations, not open-ended sessions: a
			// stuck child should fail fast and report back instead of holding
			// a local vLLM slot for the full 60-minute default.
			agentTimeout = 15 * time.Minute
		case cachedSession.Kind == session.KindCron:
			maxTurns = 50
		}
	}

	maxOutputRecovery := 1
	maxOutputScaleFactors := []float64{1.5}

	// Skill-nudger hook state: tracks per-run tool activity so we can
	// hand a clean snapshot to the background review goroutine. Zero cost
	// when acd.SkillNudger is nil or disabled.
	skillNudgerEnabled := shouldEnableSkillNudger(acd.SkillNudger, params, sessionToolPreset)
	var nudgerMu sync.Mutex
	var nudgerActivities []SkillNudgeToolActivity
	var nudgerTurns int

	cfg = agent.AgentConfig{
		MaxTurns:         maxTurns,
		Timeout:          agentTimeout,
		Model:            "", // set by caller after model resolution
		System:           systemPrompt,
		Tools:            tools,
		MaxTokens:        maxTokens,
		Thinking:         thinkingCfg,
		Temperature:      params.Temperature,
		TopP:             params.TopP,
		FrequencyPenalty: params.FrequencyPenalty,
		PresencePenalty:  params.PresencePenalty,
		StopSequences:    params.Stop,
		ResponseFormat:   params.ResponseFormat,
		ToolChoice:       params.ToolChoice,
		// Drop base64 image bytes from the message history after turn 0 so that
		// subsequent tool-call turns don't retransmit the full image payload.
		StripImagesAfterFirstTurn: hasImageAttachment(params.Attachments),
		// Deferred context injection on turn 1+: subagent completion
		// notifications via non-blocking channel reads.
		DeferredSystemText: deferredSubagentNotifications(acd.SubagentNotifyCh),
		// Emit heartbeat at each turn so WS clients know the agent is alive.
		OnTurn: func(turn int, accumulatedTokens int) {
			if acd.EmitAgentFn != nil {
				acd.EmitAgentFn("heartbeat", params.SessionKey, params.ClientRunID, map[string]any{
					"turn":   turn,
					"tokens": accumulatedTokens,
					"ts":     time.Now().UnixMilli(),
				})
			}
		},
		// Post-turn hook: (1) attribute this turn's outcome to the skills
		// consulted during it (genesis usage signal), then (2) feed the skill
		// nudger. Both are cheap no-ops when their dependency is nil.
		OnToolTurn: func(turn int, activities []agent.ToolActivity) {
			recordTurnSkillUsage(acd.SkillUsageRecorder, skillConsults, activities, params.SessionKey)
			if !skillNudgerEnabled {
				return
			}
			nudgerMu.Lock()
			nudgerTurns = turn
			for _, a := range activities {
				nudgerActivities = append(nudgerActivities, SkillNudgeToolActivity{
					Name:    a.Name,
					IsError: a.IsError,
				})
			}
			if len(activities) == 0 {
				nudgerMu.Unlock()
				return
			}
			snapshot := SkillNudgeSnapshot{
				Turns:          nudgerTurns,
				ToolActivities: append([]SkillNudgeToolActivity(nil), nudgerActivities...),
				Label:          params.SessionKey,
				Model:          params.Model,
			}
			nudgerMu.Unlock()
			acd.SkillNudger.OnToolCalls(context.Background(), params.SessionKey, len(activities), snapshot)
		},
		// Inject a fresh TurnContext at the start of each turn so that tools
		// executing in parallel within the same turn can share results via $ref.
		OnTurnInit: func(ctx context.Context) context.Context {
			ctx = WithTurnContext(ctx, NewTurnContext())
			ctx = WithRunCache(ctx, runCache)
			ctx = WithSkillConsultLog(ctx, skillConsults)
			ctx = WithFileCache(ctx, fileCache)
			ctx = WithToolPreset(ctx, sessionToolPreset)
			ctx = WithDeferredActivation(ctx, deferredActivation)
			ctx = WithSpawnFlag(ctx, spawnFlag)
			ctx = WithVerifyGate(ctx, verifyGate)
			// Cron/scheduled runs deliver their final text via the run-completion
			// layer, so an in-loop message-tool send is a benign no-op rather than
			// an outage. Without this flag on the tool context, the message tool
			// returns an error the model translates into a "전송이 안 됐네요, 직접
			// 전달드릴게요" apology that then leaks into the delivered report.
			// runAgentAsync sets this on its own ctx, but the SendSync/cron path
			// reaches RunAgent only through this OnTurnInit — so it must be set
			// here too. See message.go's AutoDeliveryFromContext branch.
			if params.AutoDeliveredOutput {
				ctx = WithAutoDelivery(ctx)
			}
			return ctx
		},
		DynamicToolsProvider: func() []llm.Tool {
			names := deferredActivation.ActivatedNames()
			if len(names) == 0 {
				return nil
			}
			return acd.Tools.DeferredLLMTools(names)
		},
		MaxOutputTokensRecovery:     maxOutputRecovery,
		MaxOutputTokensScaleFactors: maxOutputScaleFactors,
		SpawnDetected:               spawnFlag.IsSet,
		ToolLoopDetector:            agent.NewToolLoopDetector(agent.DefaultToolLoopConfig(), logger),
		// Per-turn message persistence: persist each assistant and tool_result
		// message immediately to transcript so intermediate findings survive
		// across runs (fixes the "short-term memory loss" bug).
		OnMessagePersist: buildMessagePersister(deps, params, logger),
	}

	// Reasoning sandwich (docs/research/ideal-agent-environment-harness.md §11):
	// when enabled, boost the planning (turn 0) thinking budget and keep later
	// turns at baseline. Opt-in via DENEB_REASONING_SANDWICH and only when the
	// session already has thinking enabled, so default behavior is unchanged.
	// Thinking is a request-level param, so per-turn variation is cache-safe.
	if thinkingCfg != nil && reasoningSandwichEnabled() {
		cfg.ThinkingModulator = planningSandwichThinking(thinkingCfg, cfg.MaxTokens)
	}

	// Verification gate (docs/research/ideal-agent-environment-harness.md §10):
	// a run that wrote/edited files must run a verification command before its
	// finish is accepted; the gate injects one demand prompt, then yields.
	// Default ON (inert for non-mutating runs); DENEB_VERIFY_GATE=0 disables.
	if verifyGateEnabled() {
		cfg.FinalizeGate = func(int) string { return verifyGate.finalizePrompt() }
	}

	return cfg, spawnFlag
}

// recordTurnSkillUsage attributes one turn's outcome to the skills consulted
// during it, feeding the genesis Evolver real success-rate signal instead of
// empty stats. The turn counts as a failure for every consulted skill if any
// tool in it errored. No-op when no recorder is wired or nothing was consulted.
func recordTurnSkillUsage(rec SkillUsageRecorder, log *SkillConsultLog, activities []agent.ToolActivity, sessionKey string) {
	if rec == nil || log == nil {
		return
	}
	consulted := log.DrainNew()
	if len(consulted) == 0 {
		return
	}
	errMsg := ""
	for _, a := range activities {
		if a.IsError {
			errMsg = "turn failed: tool " + a.Name + " errored"
			break
		}
	}
	for _, name := range consulted {
		rec.RecordSkillUse(sessionKey, name, errMsg == "", errMsg)
	}
}

func shouldEnableSkillNudger(nudger SkillNudger, params RunParams, sessionToolPreset string) bool {
	if nudger == nil || !nudger.Enabled() {
		return false
	}
	if params.EphemeralUser || params.EphemeralAssistant {
		return false
	}
	if sessionToolPreset == string(toolpreset.PresetSelfReview) {
		return false
	}
	return !strings.HasPrefix(params.SessionKey, "system:")
}

func resolveThinkingConfig(level string) *llm.ThinkingConfig {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "minimal":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 1024}
	case "low":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	case "medium":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 10240}
	case "high":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 32768}
	case "xhigh":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 65536}
	case "adaptive":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 16384}
	default:
		return nil
	}
}

// reasoningSandwichEnabled reports whether the planning-phase reasoning boost
// (the "reasoning sandwich" — docs/research/ideal-agent-environment-harness.md
// §11) is turned on. Off by default: it changes per-turn thinking budget, and
// the latency/quality trade-off should be validated live before defaulting on.
// Enable with DENEB_REASONING_SANDWICH=1 (or true/on/yes).
func reasoningSandwichEnabled() bool {
	return envFlagEnabled("DENEB_REASONING_SANDWICH")
}

// thinkingBudgetLadder lists the NAMED extended-thinking tiers in increasing
// order: minimal/low/medium/high/xhigh (see resolveThinkingConfig).
// boostThinkingBudget walks one step up this ladder. Note "adaptive" (16384) is
// intentionally NOT a ladder entry — it is not a named tier, so boosting from
// adaptive lands on the next tier above it (high=32768). Do not insert 16384
// here; that would change the boost semantics.
var thinkingBudgetLadder = []int{1024, 4096, 10240, 32768, 65536}

// boostThinkingBudget returns the next budget tier strictly above b, capped at
// the top of the ladder. A value already at or above the top is unchanged.
func boostThinkingBudget(b int) int {
	for _, step := range thinkingBudgetLadder {
		if step > b {
			return step
		}
	}
	return b
}

// minThinkingResponseHeadroom is the token room the planning turn must retain
// for its own output after the (boosted) thinking budget is carved out of
// max_tokens. Anthropic extended thinking requires budget_tokens < max_tokens,
// so a boost that would not leave this margin is dropped rather than risking a
// rejected request.
const minThinkingResponseHeadroom = 4096

// planningSandwichThinking returns a per-turn thinking selector that boosts the
// first (planning) turn one budget tier above the session baseline and uses the
// baseline for every later turn — the "front of the reasoning sandwich" (§11).
// Planning is where extra reasoning pays off most, while keeping middle
// tool-execution turns at baseline avoids the timeout cost of max-everywhere
// reasoning.
//
// The boost is applied only when the larger budget still leaves response
// headroom under maxTokens; otherwise the planning turn falls back to the
// baseline so it is never more likely to be rejected than a normal turn
// (Anthropic requires budget_tokens < max_tokens). maxTokens <= 0 means
// "unknown" and keeps the boost. Returns nil when base is nil so the caller
// leaves Thinking as-is.
func planningSandwichThinking(base *llm.ThinkingConfig, maxTokens int) func(turn int, acts []agent.ToolActivity) *llm.ThinkingConfig {
	if base == nil {
		return nil
	}
	boostedBudget := boostThinkingBudget(base.BudgetTokens)
	boosted := base
	if boostedBudget > base.BudgetTokens && (maxTokens <= 0 || boostedBudget <= maxTokens-minThinkingResponseHeadroom) {
		boosted = &llm.ThinkingConfig{
			Type:         base.Type,
			BudgetTokens: boostedBudget,
			Interleaved:  base.Interleaved,
		}
	}
	return func(turn int, _ []agent.ToolActivity) *llm.ThinkingConfig {
		if turn == 0 {
			return boosted
		}
		return base
	}
}

// buildMessagePersister returns a callback that persists each message to the
// transcript store immediately. This ensures intermediate assistant text and
// tool results survive across runs — fixing the "short-term memory loss" bug
// where the agent forgot discoveries made in earlier turns.
//
// Assistant messages are sanitized via sanitizeAssistantForTranscript before
// persistence: the silent-reply token (NO_REPLY) is stripped from text blocks,
// and messages that end up with no substance (all empty text, no tool_use /
// tool_result / thinking / image blocks) are dropped entirely. Without this,
// an assistant turn whose only text was "NO_REPLY" would be persisted with
// that literal token, and the model on the next turn would see it in history
// and hallucinate that it had replied — the "대답 안 하고 대답했다고 생각하는
// 경향" bug.
func buildMessagePersister(
	deps runDeps,
	params RunParams,
	logger *slog.Logger,
) func(msg llm.Message) {
	// EphemeralAssistant turns suppress assistant + tool_result persistence:
	// returning nil here disables the executor's per-turn persist callback.
	// Heartbeat sets this true so autonomous progress ticks do not pollute the
	// user's short-term transcript; heartbeat state is kept in HEARTBEAT.md.
	if deps.transcript == nil || params.EphemeralAssistant {
		return nil
	}
	return func(msg llm.Message) {
		content := msg.Content
		if msg.Role == "assistant" {
			sanitized, skip := sanitizeAssistantForTranscript(content)
			if skip {
				logger.Info("skipping persist of empty assistant turn",
					"session", params.SessionKey,
					"reason", "no user-visible content after silent-token strip")
				return
			}
			content = sanitized
		}
		chatMsg := ChatMessage{
			Role:      msg.Role,
			Content:   content, // json.RawMessage — rich blocks preserved
			Timestamp: time.Now().UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, chatMsg); err != nil {
			logger.Error("per-turn message persist failed", "role", msg.Role, "error", err)
		}
	}
}

// sanitizeAssistantForTranscript strips NO_REPLY from assistant text blocks
// and reports whether the resulting message has enough substance to persist.
// Returns (content, skip). When skip=true, the caller must not persist the
// message at all — it would only pollute transcript history and confuse the
// model into thinking it replied when it did not.
//
// "Substance" = any non-text block (tool_use, tool_result, thinking, image),
// or a text block with non-empty content after stripping.
func sanitizeAssistantForTranscript(content json.RawMessage) (json.RawMessage, bool) {
	// Text-form message: Content is a JSON-encoded string.
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		stripped := StripSilentToken(text)
		if stripped == "" {
			return nil, true
		}
		if stripped == text {
			return content, false
		}
		raw, err := json.Marshal(stripped)
		if err != nil {
			return content, false
		}
		return raw, false
	}
	// Block-form message: Content is a JSON array of ContentBlocks.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return content, false
	}
	changed := false
	hasSubstance := false
	for i := range blocks {
		if blocks[i].Type == "text" {
			stripped := StripSilentToken(blocks[i].Text)
			if stripped != blocks[i].Text {
				blocks[i].Text = stripped
				changed = true
			}
			if stripped != "" {
				hasSubstance = true
			}
			continue
		}
		// tool_use, tool_result, thinking, image — any non-text block counts
		// as substance worth preserving in history.
		hasSubstance = true
	}
	if !hasSubstance {
		return nil, true
	}
	if !changed {
		return content, false
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		return content, false
	}
	return raw, false
}

// Compile-time interface compliance.
