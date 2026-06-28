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
		// Pre-load deferred tools the preset wants active from turn 1 (e.g. the
		// self-review's skill_lifecycle) so the model can call them directly
		// instead of doing a fetch_tools dance it routinely skips. Gated by
		// preset — nil for main chat, so its toolset (and cache) is unchanged.
		if preload := toolpreset.PreloadedDeferredTools(toolpreset.Preset(sessionToolPreset)); len(preload) > 0 {
			rawTools = append(rawTools, acd.Tools.DeferredLLMTools(preload)...)
		}

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

	// Resolve thinking config from the session's ThinkingLevel setting. A
	// per-run override (params.Thinking — e.g. a cron payload's `thinking`
	// field) takes precedence, including "off" to disable the phase.
	thinkingLevel := ""
	if cachedSession != nil {
		thinkingLevel = cachedSession.ThinkingLevel
	}
	if params.Thinking != "" {
		thinkingLevel = params.Thinking
	}
	thinkingCfg := resolveThinkingConfig(thinkingLevel)
	// Interleaved thinking is an additive flag: it requires extended thinking
	// to be enabled (otherwise there's nothing to interleave). When
	// thinkingCfg is nil or disabled the interleaved bit has no effect.
	if thinkingCfg != nil && thinkingCfg.Type == "enabled" && cachedSession != nil && cachedSession.InterleavedThinking != nil && *cachedSession.InterleavedThinking {
		thinkingCfg.Interleaved = true
	}

	// Override max tokens if the caller (e.g., OpenAI HTTP endpoint) specified one.
	if params.MaxTokens != nil && *params.MaxTokens > 0 {
		maxTokens = *params.MaxTokens
	}

	// Mode-aware agent config: Chat (챗봇) mode gets reduced limits for quick
	// conversational replies; the default (업무 chat + sub-agents) and cron share
	// the full 50-turn budget so multi-step work — deep research, mail/project
	// synthesis, cron progress-reporting + wiki updates — runs without truncating.
	// The 60-min agentTimeout co-bounds wall-clock and the in-turn loop guard
	// stops a stuck agent, so the turn cap is headroom, not the safety mechanism.
	maxTurns := defaultMaxTurns         // 50
	agentTimeout := defaultAgentTimeout // 60min
	if cachedSession != nil {
		switch {
		case cachedSession.Mode == session.ModeChat:
			maxTurns = 20
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

	// Coding sessions bind fs/exec to their git worktree (applied in OnTurnInit so
	// the SendSync/native-client path picks it up too).
	var codingWorkspace string
	if cachedSession != nil && cachedSession.Mode == session.ModeCode {
		codingWorkspace = cachedSession.WorkspaceDir
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
			// Session key must be set HERE, not only in runAgentAsync's ctx
			// decoration: the SendSync path (miniapp.chat.send — the native
			// client's sole entry) reaches RunAgent without that decoration,
			// so tools reading SessionKeyFromContext (sessions_spawn parent
			// attribution, polaris session-scoped recall, subagents, spillover)
			// saw "" there. Same precedent as WithAutoDelivery below.
			ctx = WithSessionKey(ctx, params.SessionKey)
			ctx = WithTurnContext(ctx, NewTurnContext())
			ctx = WithRunCache(ctx, runCache)
			ctx = WithSkillConsultLog(ctx, skillConsults)
			ctx = WithFileCache(ctx, fileCache)
			ctx = WithToolPreset(ctx, sessionToolPreset)
			ctx = WithWorkspaceOverride(ctx, codingWorkspace)
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
		// across runs (fixes the "short-term memory loss" bug). Wrapped below so
		// the verification gate also observes each finishing assistant turn's
		// text (for the explicit "검증 불필요:" opt-out) on the same turn the
		// model tries to end.
		OnMessagePersist: verifyGateObservingPersister(buildMessagePersister(deps, params, logger), verifyGate),
	}

	// Reasoning sandwich (docs/research/ideal-agent-environment-harness.md §11):
	// when enabled, boost the planning (turn 0) thinking budget AND re-boost the
	// verify/finish turn (the back half — the gate's armed-and-finishing turn is
	// where a fix plan must form), keeping middle tool-execution turns at
	// baseline. Opt-in via DENEB_REASONING_SANDWICH and only when the session
	// already has thinking enabled, so default behavior is unchanged. The
	// modulator returns nil on no-opinion turns so it composes cleanly with the
	// effort router (see effortStepModulator). Thinking is a request-level param,
	// so per-turn variation is cache-safe.
	if thinkingCfg != nil && thinkingCfg.Type == "enabled" && reasoningSandwichEnabled() {
		cfg.ThinkingModulator = reasoningSandwichThinking(thinkingCfg, cfg.MaxTokens, verifyGate)
	}

	// Verification gate (docs/research/ideal-agent-environment-harness.md §10):
	// a run that wrote/edited files must run a verification command before its
	// finish is accepted. The gate escalates across two injections and then
	// HARD-BLOCKS — refusing finish until a verify command runs or the model
	// emits an explicit "검증 불필요: <이유>" opt-out (observed via the wrapped
	// persister). A still-armed run that escapes only via max_turns is logged by
	// the sentinel terminal probe (turn < 0). Default ON (inert for non-mutating
	// runs); DENEB_VERIFY_GATE=0 disables.
	if verifyGateEnabled() {
		cfg.FinalizeGate = func(turn int) string {
			if turn < 0 {
				// Terminal probe from the executor's max_turns path: the gate
				// never got the last word, so log a silent escape if still armed.
				verifyGate.logFinishedWhileArmed(logger)
				return ""
			}
			return verifyGate.finalizePrompt(logger)
		}
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
		// A "skills" tool error means the consult mechanism itself failed to load
		// the skill (e.g. a path/catalog bug) — not the skill performing badly.
		// Attributing it would pin the skill below the evolver's success-rate
		// threshold and trigger phantom re-evolutions, so skip it.
		if a.IsError && a.Name != "skills" {
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
	case "off", "none", "disabled":
		// Explicitly disable the thinking phase. On dual-mode vLLM models the
		// disabled config is translated to chat_template_kwargs (the only
		// effective control on e.g. deepseek-v4); applyModelTuning fills the
		// model's toggle kwarg. Providers without a toggle fall back to the
		// openai.go reasoning_effort floor; Anthropic simply omits thinking.
		return &llm.ThinkingConfig{Type: "disabled"}
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

// reasoningSandwichThinking returns the full "reasoning sandwich" (§11) per-turn
// selector: it boosts the FRONT (turn 0, planning) and the BACK (the
// verify/finish turn the verification gate is actively blocking — gate ==
// awaitingVerify) one budget tier above the session baseline, while leaving the
// middle tool-execution turns untouched. The front is where the plan forms; the
// back is where a fix/verify plan must form after the gate refuses an
// unverified finish — both pay off from extra reasoning, the middle does not (it
// would just add timeout cost).
//
// The boost is applied only when the larger budget still leaves response
// headroom under maxTokens; otherwise it falls back so a boosted turn is never
// more likely to be rejected than a normal one (Anthropic requires
// budget_tokens < max_tokens). maxTokens <= 0 means "unknown" and keeps the
// boost.
//
// IMPORTANT — composition contract: this selector returns nil on every
// non-boost turn (the executor then falls back to cfg.Thinking, and the effort
// router can layer its lowering policy underneath; see effortStepModulator).
// It returns the boost ONLY on the two boost turns. So when both the sandwich
// and the router are enabled, the boost turns win (sandwich returns non-nil)
// and the router governs the rest (sandwich returns nil → router's output).
// Returns nil when base is nil so the caller leaves Thinking as-is. gate may be
// nil (no verification gate) — then only the front boost fires.
func reasoningSandwichThinking(base *llm.ThinkingConfig, maxTokens int, gate *verifyGateState) func(turn int, acts []agent.ToolActivity) *llm.ThinkingConfig {
	if base == nil {
		return nil
	}
	boostedBudget := boostThinkingBudget(base.BudgetTokens)
	var boosted *llm.ThinkingConfig
	if boostedBudget > base.BudgetTokens && (maxTokens <= 0 || boostedBudget <= maxTokens-minThinkingResponseHeadroom) {
		boosted = &llm.ThinkingConfig{
			Type:         base.Type,
			BudgetTokens: boostedBudget,
			Interleaved:  base.Interleaved,
		}
	} else {
		// No headroom to boost: the boost turns still want MORE than the middle,
		// so pin them to the baseline explicitly (non-nil) rather than the
		// router's possibly-lowered output — the front/back never reason LESS
		// than a plain turn even when the tier can't grow.
		boosted = base
	}
	return func(turn int, _ []agent.ToolActivity) *llm.ThinkingConfig {
		if turn == 0 || gate.awaitingVerify() {
			return boosted
		}
		return nil // no opinion: fall back to cfg.Thinking, or compose under the router
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

// verifyGateObservingPersister wraps the per-turn persister so the verification
// gate observes each persisted assistant turn's text — recognizing an explicit
// "검증 불필요:" opt-out the moment the executor records a finishing turn (the
// executor persists the finishing assistant turn just BEFORE consulting the
// gate), so the model is never nagged after giving a valid reason.
//
// It deliberately returns nil whenever the inner persister is nil
// (EphemeralAssistant / no transcript): a non-nil callback would make the
// executor count phantom persists (result.TurnsPersisted) and suppress the
// aggregate transcript write. Those runs (heartbeats, tests) therefore do not
// honor the opt-out line and the gate simply hard-blocks toward verification —
// the safe asymmetry (a false-hard wastes a turn; a false-easy ships unverified
// code). gate may be nil, in which case the inner persister is returned as-is.
func verifyGateObservingPersister(inner func(msg llm.Message), gate *verifyGateState) func(msg llm.Message) {
	if inner == nil || gate == nil {
		return inner
	}
	return func(msg llm.Message) {
		if msg.Role == "assistant" {
			gate.observeFinishText(extractTextFromMessage(msg))
		}
		inner(msg)
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
