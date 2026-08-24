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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// parallelSafeToolVet returns the executor's parallel-turn vet backed by the
// generated parallelSafeTools classification (tool_classification.json).
// DENEB_PARALLEL_TOOLS=0 is the operator kill-switch: it returns nil, which
// keeps every turn fully sequential.
func parallelSafeToolVet() func(string) bool {
	if os.Getenv("DENEB_PARALLEL_TOOLS") == "0" {
		return nil
	}
	return func(name string) bool {
		_, ok := parallelSafeTools[name]
		return ok
	}
}

const (
	// BriefcaseStreamIdleTimeout is pinned into deterministic benchmark runs so
	// DENEB_STREAM_IDLE_TIMEOUT_MS cannot change a signed run's stream policy.
	BriefcaseStreamIdleTimeout = 180 * time.Second
	// BriefcaseParallelToolsEnabled records the benchmark's fixed sequential
	// tool policy. Production runs retain the environment-controlled vet above.
	BriefcaseParallelToolsEnabled = false
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
	// SkillUsageRecorder attributes the completed run's outcome to the skills
	// consulted during it, feeding the genesis Evolver's success-rate gate. Nil disables.
	SkillUsageRecorder SkillUsageRecorder
	// ReplayDeferredTools are deferred tools the session transcript proves were
	// activated in earlier runs (replayActivatedTools, deferred_replay.go);
	// they re-enter this run's Tools array and pre-seed DeferredActivation.
	ReplayDeferredTools []string
	// InitialDeferredTools are required by exact-trigger skills auto-loaded for
	// this turn. They enter the first provider request without a fetch_tools hop.
	InitialDeferredTools []string
}

// agentExecutionPolicy is the resolved, immutable execution policy for one
// run. Keeping precedence resolution separate from AgentConfig assembly makes
// the caller-facing defaults and Briefcase overrides auditable in one place.
type agentExecutionPolicy struct {
	maxTurns              int
	timeout               time.Duration
	maxTokens             int
	maxToolCallAttempts   *int
	thinking              *llm.ThinkingConfig
	maxOutputRecovery     int
	maxOutputScaleFactors []float64
	streamIdleTimeout     time.Duration
	parallelSafeTool      func(string) bool
}

// agentRunState owns the state shared by every turn in one agent run. The
// initializer below injects these exact instances into each fresh turn
// context; callers also receive spawnFlag and execStats for run-level reports.
type agentRunState struct {
	runCache           *RunCache
	blackboard         *toolport.Blackboard
	skillConsults      *SkillConsultLog
	fileCache          *agent.FileCache
	spawnFlag          *SpawnFlag
	verifyGate         *verifyGateState
	deferredActivation *DeferredActivation
	execStats          *toolport.ToolExecStats
}

func newAgentRunState(replayedDeferredTools []string) *agentRunState {
	state := &agentRunState{
		runCache:           NewRunCache(),
		blackboard:         toolport.NewBlackboard(),
		skillConsults:      NewSkillConsultLog(),
		fileCache:          agent.NewFileCache(agent.DefaultFileCacheMaxItems),
		spawnFlag:          NewSpawnFlag(),
		verifyGate:         &verifyGateState{},
		deferredActivation: NewDeferredActivation(),
		execStats:          toolport.NewToolExecStats(),
	}
	if len(replayedDeferredTools) > 0 {
		state.deferredActivation.Seed(replayedDeferredTools)
	}
	return state
}

// turnInitializer is the shared context-decoration point for asynchronous and
// SendSync runs. The ordering is deliberate: general run state is installed
// before optional per-run policies such as dry-run and auto-delivery.
func (s *agentRunState) turnInitializer(params RunParams, sessionToolPreset string) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		ctx = WithSessionKey(ctx, params.SessionKey)
		ctx = WithTurnContext(ctx, NewTurnContext())
		ctx = WithRunCache(ctx, s.runCache)
		ctx = toolport.WithBlackboard(ctx, s.blackboard)
		ctx = WithSkillConsultLog(ctx, s.skillConsults)
		ctx = WithFileCache(ctx, s.fileCache)
		ctx = WithToolPreset(ctx, sessionToolPreset)
		ctx = WithDeferredActivation(ctx, s.deferredActivation)
		ctx = WithSpawnFlag(ctx, s.spawnFlag)
		ctx = WithVerifyGate(ctx, s.verifyGate)
		ctx = toolport.WithToolExecStats(ctx, s.execStats)
		if params.ToolDryRun {
			ctx = toolport.WithToolDryRun(ctx)
		}
		if params.AutoDeliveredOutput {
			ctx = WithAutoDelivery(ctx)
		}
		return ctx
	}
}

func (s *agentRunState) dynamicToolsProvider(registry *ToolRegistry) func() []llm.Tool {
	return func() []llm.Tool {
		names := s.deferredActivation.ActivatedNames()
		if len(names) == 0 || registry == nil {
			return nil
		}
		return registry.DeferredLLMTools(names)
	}
}

// agentTurnHooks owns the mutable bookkeeping used by the heartbeat and
// skill-nudger callbacks. Its mutex protects only the nudger's
// accumulated snapshot; external callbacks run after it is released.
type agentTurnHooks struct {
	params           RunParams
	emitAgentFn      func(kind, sessionKey, runID string, payload map[string]any)
	nudger           SkillNudger
	nudgerEnabled    bool
	nudgeCtx         context.Context
	nudgerMu         sync.Mutex
	nudgerActivities []SkillNudgeToolActivity
	nudgerTurns      int
}

func newAgentTurnHooks(params RunParams, deps runDeps, acd agentConfigDeps, sessionToolPreset string) *agentTurnHooks {
	nudgeCtx := deps.callbacks.shutdownCtx
	if nudgeCtx == nil {
		// Reviews intentionally outlive a request. Production supplies the
		// server lifecycle context; Background is a test-only fallback.
		nudgeCtx = context.Background()
	}
	return &agentTurnHooks{
		params:        params,
		emitAgentFn:   acd.EmitAgentFn,
		nudger:        acd.SkillNudger,
		nudgerEnabled: shouldEnableSkillNudger(acd.SkillNudger, params, sessionToolPreset),
		nudgeCtx:      nudgeCtx,
	}
}

func (h *agentTurnHooks) onTurn(turn, accumulatedTokens int) {
	if h.emitAgentFn == nil {
		return
	}
	h.emitAgentFn("heartbeat", h.params.SessionKey, h.params.ClientRunID, map[string]any{
		"turn":   turn,
		"tokens": accumulatedTokens,
		"ts":     time.Now().UnixMilli(),
	})
}

func (h *agentTurnHooks) onToolTurn(turn int, activities []agent.ToolActivity) {
	if !h.nudgerEnabled {
		return
	}
	snapshot, ok := h.recordNudgeSnapshot(turn, activities)
	if !ok {
		return
	}
	h.nudger.OnToolCalls(h.nudgeCtx, h.params.SessionKey, len(activities), snapshot)
}

func (h *agentTurnHooks) recordNudgeSnapshot(turn int, activities []agent.ToolActivity) (SkillNudgeSnapshot, bool) {
	h.nudgerMu.Lock()
	defer h.nudgerMu.Unlock()

	h.nudgerTurns = turn
	for _, activity := range activities {
		h.nudgerActivities = append(h.nudgerActivities, SkillNudgeToolActivity{
			Name:    activity.Name,
			IsError: activity.IsError,
		})
	}
	if len(activities) == 0 {
		return SkillNudgeSnapshot{}, false
	}
	return SkillNudgeSnapshot{
		Turns:          h.nudgerTurns,
		ToolActivities: append([]SkillNudgeToolActivity(nil), h.nudgerActivities...),
		Label:          h.params.SessionKey,
		Model:          h.params.Model,
	}, true
}

func buildAgentTools(registry *ToolRegistry, sessionToolPreset string, replayedDeferredTools []string) []llm.Tool {
	if registry == nil {
		return nil
	}
	preset := sessionToolPreset
	rawTools := registry.FilteredLLMTools(toolwire.AllowedTools(preset))
	if preload := toolwire.PreloadedDeferredTools(preset); len(preload) > 0 {
		rawTools = append(rawTools, registry.DeferredLLMTools(preload)...)
	}

	// Cache-stable ordering: built-in tools form a sorted prefix, while
	// replayed activations retain their first-activation order at the tail.
	registeredNames := registry.Names()
	builtinNames := make(map[string]struct{}, len(registeredNames))
	for _, name := range registeredNames {
		builtinNames[name] = struct{}{}
	}
	tools := PartitionTools(rawTools, builtinNames).MergedTools()
	return appendReplayedDeferredTools(registry, tools, replayedDeferredTools)
}

func appendReplayedDeferredTools(registry *ToolRegistry, tools []llm.Tool, replayed []string) []llm.Tool {
	if len(replayed) == 0 {
		return tools
	}
	existing := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		existing[tool.Name] = struct{}{}
	}
	for _, tool := range registry.DeferredLLMTools(replayed) {
		if _, ok := existing[tool.Name]; !ok {
			tools = append(tools, tool)
		}
	}
	return tools
}

func mergeDeferredToolNames(groups ...[]string) []string {
	seen := make(map[string]struct{})
	var merged []string
	for _, group := range groups {
		for _, name := range group {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			merged = append(merged, name)
		}
	}
	return merged
}

func filterPreloadedDeferredToolNames(registry *ToolRegistry, names []string, preset string) []string {
	if registry == nil || len(names) == 0 {
		return nil
	}
	allowed := toolwire.AllowedTools(preset)
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := registry.DeferredToolDef(name); !ok {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[name]; !ok {
				continue
			}
		}
		filtered = append(filtered, name)
	}
	return filtered
}

func resolveAgentExecutionPolicy(params RunParams, deps runDeps, cachedSession *session.Session, configuredMaxTokens int) agentExecutionPolicy {
	maxTurns, timeout := resolveAgentRunLimits(params, deps, cachedSession)
	maxOutputRecovery, maxOutputScaleFactors, streamIdleTimeout, parallelSafeTool := resolveAgentOutputPolicy(deps.briefcaseMode)
	return agentExecutionPolicy{
		maxTurns:              maxTurns,
		timeout:               timeout,
		maxTokens:             resolveAgentMaxTokens(params, configuredMaxTokens),
		maxToolCallAttempts:   copyMaxToolCallAttempts(params.MaxToolCallAttempts),
		thinking:              resolveAgentThinking(params, cachedSession),
		maxOutputRecovery:     maxOutputRecovery,
		maxOutputScaleFactors: maxOutputScaleFactors,
		streamIdleTimeout:     streamIdleTimeout,
		parallelSafeTool:      parallelSafeTool,
	}
}

func resolveAgentMaxTokens(params RunParams, configured int) int {
	if params.MaxTokens != nil && *params.MaxTokens > 0 {
		return *params.MaxTokens
	}
	if configured > 0 {
		return configured
	}
	return defaultMaxTokens
}

func copyMaxToolCallAttempts(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func resolveAgentThinking(params RunParams, cachedSession *session.Session) *llm.ThinkingConfig {
	level := ""
	if cachedSession != nil {
		level = cachedSession.ThinkingLevel
	}
	if params.Thinking != "" {
		level = params.Thinking
	}
	thinking := resolveThinkingConfig(level)
	if thinking != nil && thinking.Type == "enabled" && cachedSession != nil &&
		cachedSession.InterleavedThinking != nil && *cachedSession.InterleavedThinking {
		thinking.Interleaved = true
	}
	return thinking
}

func resolveAgentRunLimits(params RunParams, deps runDeps, cachedSession *session.Session) (int, time.Duration) {
	maxTurns := defaultMaxTurns
	timeout := defaultAgentTimeout
	if cachedSession != nil {
		switch {
		case cachedSession.SpawnedBy != "":
			timeout = 15 * time.Minute
		case cachedSession.Kind == session.KindCron:
			maxTurns = 50
		}
	}
	if deps.runLimits.MaxTurns > 0 {
		maxTurns = deps.runLimits.MaxTurns
	}
	if params.MaxTurns != nil && *params.MaxTurns > 0 {
		maxTurns = *params.MaxTurns
	}
	if deps.runLimits.Timeout > 0 {
		timeout = deps.runLimits.Timeout
	}
	return maxTurns, timeout
}

func resolveAgentOutputPolicy(briefcaseMode bool) (int, []float64, time.Duration, func(string) bool) {
	parallelSafeTool := parallelSafeToolVet()
	if !briefcaseMode {
		return 1, []float64{1.5}, 0, parallelSafeTool
	}
	if !BriefcaseParallelToolsEnabled {
		parallelSafeTool = nil
	}
	return 0, nil, BriefcaseStreamIdleTimeout, parallelSafeTool
}

func applyBriefcaseAgentPolicy(cfg *agent.AgentConfig, briefcaseMode bool) {
	if !briefcaseMode {
		return
	}
	cfg.MaxTotalOutputTokens = cfg.MaxTokens
	cfg.MaxStreamBytes = cfg.MaxTokens * 16
	if cfg.MaxStreamBytes < 64<<10 {
		cfg.MaxStreamBytes = 64 << 10
	}
	if cfg.MaxStreamBytes > 8<<20 {
		cfg.MaxStreamBytes = 8 << 20
	}
	cfg.RequireExplicitStopReason = true
	cfg.RequireStrictStopShape = true
	// The signed hard cap replaces the ambient detector so the signed ToolGate
	// observes every within-budget attempt and keeps its remaining count exact.
	cfg.ToolLoopDetector = nil
}

func applyReasoningSandwichPolicy(cfg *agent.AgentConfig, briefcaseMode bool, thinking *llm.ThinkingConfig, verifyGate *verifyGateState) {
	if briefcaseMode || thinking == nil || thinking.Type != "enabled" || !reasoningSandwichEnabled() {
		return
	}
	cfg.ThinkingModulator = reasoningSandwichThinking(thinking, cfg.MaxTokens, verifyGate)
}

func applyVerificationAgentPolicy(cfg *agent.AgentConfig, briefcaseMode bool, verifyGate *verifyGateState, logger *slog.Logger) {
	if briefcaseMode || !verifyGateEnabled() {
		return
	}
	cfg.FinalizeGate = func(turn int) string {
		if turn < 0 {
			verifyGate.logFinishedWhileArmed(logger)
			return ""
		}
		return verifyGate.finalizePrompt(logger)
	}
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
) (cfg agent.AgentConfig, spawnFlag *SpawnFlag, execStats *toolport.ToolExecStats, skillConsults *SkillConsultLog) {
	initialDeferredTools := filterPreloadedDeferredToolNames(
		acd.Tools,
		mergeDeferredToolNames(acd.ReplayDeferredTools, acd.InitialDeferredTools),
		sessionToolPreset,
	)
	tools := buildAgentTools(acd.Tools, sessionToolPreset, initialDeferredTools)
	state := newAgentRunState(initialDeferredTools)
	policy := resolveAgentExecutionPolicy(params, deps, cachedSession, acd.MaxTokens)
	turnHooks := newAgentTurnHooks(params, deps, acd, sessionToolPreset)

	cfg = agent.AgentConfig{
		MaxTurns:            policy.maxTurns,
		Timeout:             policy.timeout,
		Model:               "", // set by caller after model resolution
		System:              systemPrompt,
		Tools:               tools,
		MaxTokens:           policy.maxTokens,
		MaxToolCallAttempts: policy.maxToolCallAttempts,
		Thinking:            policy.thinking,
		Temperature:         params.Temperature,
		TopP:                params.TopP,
		FrequencyPenalty:    params.FrequencyPenalty,
		PresencePenalty:     params.PresencePenalty,
		Seed:                deps.samplingSeed,
		StopSequences:       params.Stop,
		ResponseFormat:      params.ResponseFormat,
		ToolChoice:          params.ToolChoice,
		// Drop base64 image bytes from the message history after turn 0 so that
		// subsequent tool-call turns don't retransmit the full image payload.
		StripImagesAfterFirstTurn: hasImageAttachment(params.Attachments),
		// Late subagent completion notifications ride the next tool-results
		// user message (non-blocking channel drains) — never the mid-run
		// system prompt (prompt-cache: content-prefix providers).
		DeferredTurnNotices:         deferredSubagentNotifications(acd.SubagentNotifyCh),
		OnTurn:                      turnHooks.onTurn,
		OnToolTurn:                  turnHooks.onToolTurn,
		OnTurnInit:                  state.turnInitializer(params, sessionToolPreset),
		DynamicToolsProvider:        state.dynamicToolsProvider(acd.Tools),
		MaxOutputTokensRecovery:     policy.maxOutputRecovery,
		MaxOutputTokensScaleFactors: policy.maxOutputScaleFactors,
		DisableBudgetGrace:          deps.briefcaseMode,
		DisableTokenFeedback:        deps.briefcaseMode,
		DisableStreamRetry:          deps.briefcaseMode,
		RequireProviderModel:        deps.briefcaseMode,
		SpawnDetected:               state.spawnFlag.IsSet,
		ToolLoopDetector:            agent.NewToolLoopDetector(agent.DefaultToolLoopConfig(), logger),
		StreamIdleTimeout:           policy.streamIdleTimeout,
		ParallelSafeTool:            policy.parallelSafeTool,
		// Per-turn message persistence: persist each assistant and tool_result
		// message immediately to transcript so intermediate findings survive
		// across runs (fixes the "short-term memory loss" bug). Wrapped below so
		// the verification gate also observes each finishing assistant turn's
		// text (for the explicit "검증 불필요:" opt-out) on the same turn the
		// model tries to end.
		OnMessagePersist: verifyGateObservingPersister(buildMessagePersister(deps, params, logger), state.verifyGate),
	}
	applyBriefcaseAgentPolicy(&cfg, deps.briefcaseMode)

	// Both policies are per-request hooks: they do not alter prompt bytes or
	// the cache-stable tool ordering assembled above.
	applyReasoningSandwichPolicy(&cfg, deps.briefcaseMode, policy.thinking, state.verifyGate)
	applyVerificationAgentPolicy(&cfg, deps.briefcaseMode, state.verifyGate, logger)

	return cfg, state.spawnFlag, state.execStats, state.skillConsults
}

// recordRunSkillUsage attributes one completed run's outcome to every skill
// consulted during it. Waiting until the run ends prevents an early clean
// skill-read turn from hiding a later tool error or missing final answer.
func recordRunSkillUsage(rec SkillUsageRecorder, log *SkillConsultLog, result *agent.AgentResult, runErr error, sessionKey, model string, autoLoaded map[string]bool) {
	if rec == nil || log == nil {
		return
	}
	// Skill-review forks read SKILL.md bodies to JUDGE them — introspection,
	// not usage. Recording them inflated consult counts and wrote failure rows
	// that pinned innocent skills under the evolver's success-rate gate
	// (production skill_usage.jsonl 2026-06: most "skills" tool calls and most
	// topsolar-db "failures" came from review-fork sessions, not real use).
	if strings.HasPrefix(sessionKey, "system:skill-review:") {
		return
	}
	consulted := log.DrainNew()
	if len(consulted) == 0 {
		return
	}
	errMsg := skillRunFailure(result, runErr)
	attributed, _ := rec.(chatport.SkillUsageAttributionRecorder)
	for _, name := range consulted {
		if attributed == nil {
			rec.RecordSkillUse(sessionKey, name, errMsg == "", errMsg, model)
			continue
		}
		attributed.RecordSkillUseAttributed(sessionKey, name, errMsg == "", errMsg, model,
			skillUseAttribution(name, autoLoaded, result))
	}
}

// skillUseAttribution decides, deterministically, WHERE this run's outcome
// belongs for one consulted skill (Demystifying Agent Skills, 2608.14036):
// a consult is recorded with the WHOLE run's success, so a skill that was
// merely loaded and then ignored otherwise takes the blame for the turn.
//
// "Exercised" is evidence, not inference: the skill declares the tools its
// procedure needs (requires_tools), and the run either ran one of them or did
// not. A skill declaring no tools has nothing to check against and stays
// unknown rather than being guessed either way.
func skillUseAttribution(name string, autoLoaded map[string]bool, result *agent.AgentResult) chatport.SkillUseAttribution {
	attr := chatport.SkillUseAttribution{
		Delivery:  chatport.SkillDeliveryModelRead,
		Exercised: chatport.SkillExercisedUnknown,
	}
	if autoLoaded[name] {
		attr.Delivery = chatport.SkillDeliveryAutoLoad
	}
	required := skillRequiredToolSet(name)
	if len(required) == 0 || result == nil {
		return attr
	}
	attr.Exercised = chatport.SkillExercisedNo
	for _, a := range result.ToolActivities {
		if required[a.Name] {
			attr.Exercised = chatport.SkillExercisedYes
			break
		}
	}
	return attr
}

// skillRequiredToolSet reads requires_tools off the frozen snapshot — the same
// source the auto-activation path uses, so the two never disagree.
func skillRequiredToolSet(name string) map[string]bool {
	for _, s := range cachedResolvedSkills() {
		if s.Name != name || len(s.RequiresTools) == 0 {
			continue
		}
		set := make(map[string]bool, len(s.RequiresTools))
		for _, tool := range s.RequiresTools {
			set[tool] = true
		}
		return set
	}
	return nil
}

func skillRunFailure(result *agent.AgentResult, runErr error) string {
	if runErr != nil {
		return "run failed: " + runErr.Error()
	}
	if result == nil {
		return "run failed: no result"
	}
	for _, a := range result.ToolActivities {
		// A "skills" tool error means the consult mechanism itself failed to load
		// the skill (e.g. a path/catalog bug) — not the skill performing badly.
		// Attributing it would pin the skill below the evolver's success-rate
		// threshold and trigger phantom re-evolutions, so skip it.
		if a.IsError && a.Name != "skills" {
			return "run failed: tool " + a.Name + " errored"
		}
	}
	if strings.TrimSpace(result.Text) == "" && strings.TrimSpace(result.DeliverableText) == "" {
		return "run failed: no final deliverable"
	}
	switch result.StopReason {
	case "aborted", "timeout", "max_tokens", "max_turns":
		return "run failed: stop reason " + result.StopReason
	}
	return ""
}

func shouldEnableSkillNudger(nudger SkillNudger, params RunParams, sessionToolPreset string) bool {
	if nudger == nil || !nudger.Enabled() {
		return false
	}
	if params.EphemeralUser || params.EphemeralAssistant {
		return false
	}
	if sessionToolPreset == toolwire.PresetSelfReview {
		return false
	}
	// Cron sessions are excluded: a cron is an already-codified workflow (its
	// prompt follows an existing skill by construction), so reviewing it is
	// structurally a no-op. And because every cron run gets a FRESH session key
	// (cron:<job>:<ts>) its nudge backoff restarts at the base interval, while
	// the long-lived interactive session's backoff climbs to the cap — in
	// production (2026-07-04) that inverted the review input: the last 60
	// lifecycle decisions were ALL cron-session no-ops while the sessions where
	// genesis-worthy patterns actually appear (interactive work) were barely
	// reviewed. Skill improvement for cron-used skills still happens via the
	// usage-stats-driven Evolver — this gate only redirects the nudger.
	if session.IsCronSession(params.SessionKey) {
		return false
	}
	return !session.IsSystemSession(params.SessionKey)
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
// market letter tokens ("{{market:usd_krw}}") are substituted with their
// recorded display values (skipped in Briefcase — deterministic runs must not
// read time-sensitive process-global caches), and messages that end up with no
// substance (all empty text, no tool_use / tool_result / thinking / image
// blocks) are dropped entirely. Without the NO_REPLY strip, an assistant turn
// whose only text was "NO_REPLY" would be persisted with that literal token,
// and the model on the next turn would see it in history and hallucinate that
// it had replied — the "대답 안 하고 대답했다고 생각하는 경향" bug.
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
			sanitized, skip := sanitizeAssistantForTranscript(json.RawMessage(content.Bytes()), !deps.briefcaseMode)
			if skip {
				logger.Info("skipping persist of empty assistant turn",
					"session", params.SessionKey,
					"reason", "no user-visible content after silent-token strip")
				return
			}
			content = llm.FlexibleFromRaw(sanitized)
		}
		now := time.Now()
		if deps.briefcaseMode {
			now = deps.now()
		}
		chatMsg := ChatMessage{
			Role:      msg.Role,
			Content:   json.RawMessage(content.Bytes()), // rich blocks preserved
			Timestamp: now.UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, chatMsg); err != nil {
			logger.Error("per-turn message persist failed", "role", msg.Role, "error", err)
			deps.strictErrors.Record(err)
			return
		}
		if msg.Role == "user" && contentHasToolResult(content.Bytes()) {
			if receipts := chatport.ResolveToolResultReceiptStore(deps.transcript); receipts != nil {
				if err := receipts.DeleteToolResultReceipts(params.SessionKey); err != nil {
					logger.Warn("tool result recovery receipt cleanup failed",
						"session", params.SessionKey, "error", err)
				}
			}
		}
	}
}

func contentHasToolResult(content []byte) bool {
	var blocks []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return false
	}
	for _, block := range blocks {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
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
// When substituteMarketTokens is true, market letter tokens
// ("{{market:usd_krw}}") in text blocks are also replaced with their recorded
// display values (toolwire.SubstituteMarketLetterTokens). This is the per-turn persist
// chokepoint for streamed/async runs: a turn that mimics the morning-letter
// skeleton (2026-07-11 production transcript, client:main) would otherwise
// persist raw template syntax and the native card would render it verbatim.
// Sibling substitutions: SyncResult.BestText (sync RPC response),
// substituteRunMarketTokens (async finalize), proactive relay
// (prepareProactiveDelivery). Thinking/tool blocks are never touched.
// Deterministic Briefcase runs pass false — process-global, time-sensitive
// presentation caches must not change score-visible bytes (BestTextRaw rule).
//
// "Substance" = any non-text block (tool_use, tool_result, thinking, image),
// or a text block with non-empty content after stripping.
func sanitizeAssistantForTranscript(content json.RawMessage, substituteMarketTokens bool) (json.RawMessage, bool) {
	sanitizeText := func(s string) string {
		s = StripSilentToken(s)
		if substituteMarketTokens {
			s = toolwire.SubstituteMarketLetterTokens(s)
		}
		return s
	}
	// Text-form message: Content is a JSON-encoded string.
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		stripped := sanitizeText(text)
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
			stripped := sanitizeText(blocks[i].Text)
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
