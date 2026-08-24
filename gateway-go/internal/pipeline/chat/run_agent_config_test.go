package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
)

type configNudgeCall struct {
	sessionKey string
	count      int
	snapshot   SkillNudgeSnapshot
}

type configNudger struct {
	calls []configNudgeCall
}

func (*configNudger) Enabled() bool { return true }

func (n *configNudger) OnToolCalls(_ context.Context, sessionKey string, count int, snapshot SkillNudgeSnapshot) {
	n.calls = append(n.calls, configNudgeCall{sessionKey: sessionKey, count: count, snapshot: snapshot})
}

func (*configNudger) Reset(string) {}

// OnTurnInit is the only ctx-decoration point shared by BOTH run entries:
// runAgentAsync decorates its own ctx, but the SendSync path (miniapp.chat.send
// — the native client's sole entry) reaches RunAgent without that decoration.
// Session key and tool preset must therefore be injected here, or tools like
// sessions_spawn (parent attribution), polaris (session-scoped recall), and
// the preset Execute gate silently read empty values on the sync path.
func TestBuildAgentConfigOnTurnInitReturnsSessionKeyAndPreset(t *testing.T) {
	params := RunParams{SessionKey: "client:main"}
	cfg, _, _, _ := buildAgentConfig(params, runDeps{}, nil, nil, "researcher", agentConfigDeps{}, slog.Default())

	if cfg.OnTurnInit == nil {
		t.Fatal("OnTurnInit must be set")
	}
	ctx := cfg.OnTurnInit(context.Background())

	if got := toolport.SessionKeyFromContext(ctx); got != "client:main" {
		t.Errorf("session key from OnTurnInit ctx = %q, want %q", got, "client:main")
	}
	if got := toolport.ToolPresetFromContext(ctx); got != "researcher" {
		t.Errorf("tool preset from OnTurnInit ctx = %q, want %q", got, "researcher")
	}
}

func TestBuildAgentConfigWithRunLimitsOverridesModeDefaults(t *testing.T) {
	wantTimeout := 7 * time.Minute
	wantSeed := int64(42001)
	deps := runDeps{runLimits: RunLimits{MaxTurns: 123, Timeout: wantTimeout}, samplingSeed: &wantSeed}
	cfg, _, _, _ := buildAgentConfig(RunParams{}, deps, nil, nil, "briefcase", agentConfigDeps{}, slog.Default())

	if cfg.MaxTurns != 123 {
		t.Fatalf("MaxTurns = %d, want 123", cfg.MaxTurns)
	}
	if cfg.Timeout != wantTimeout {
		t.Fatalf("Timeout = %s, want %s", cfg.Timeout, wantTimeout)
	}
	if cfg.Seed == nil || *cfg.Seed != wantSeed {
		t.Fatalf("Seed = %v, want %d", cfg.Seed, wantSeed)
	}
}

func TestBuildAgentConfigWithBriefcaseModeAppliesDeterministicLimits(t *testing.T) {
	t.Setenv("DENEB_STREAM_IDLE_TIMEOUT_MS", "-1")
	t.Setenv("DENEB_PARALLEL_TOOLS", "1")
	maxTurns, maxTokens, maxToolCallAttempts := 7, 1234, 3
	cfg, _, _, _ := buildAgentConfig(RunParams{
		MaxTurns: &maxTurns, MaxTokens: &maxTokens, MaxToolCallAttempts: &maxToolCallAttempts,
	}, runDeps{
		briefcaseMode: true, runLimits: RunLimits{MaxTurns: 99, Timeout: time.Minute},
	}, nil, nil, string(toolpreset.PresetBriefcase), agentConfigDeps{MaxTokens: maxTokens}, slog.Default())

	if cfg.MaxTurns != maxTurns || cfg.MaxTokens != maxTokens || cfg.MaxTotalOutputTokens != maxTokens {
		t.Fatalf("briefcase budgets = turns %d tokens %d total %d", cfg.MaxTurns, cfg.MaxTokens, cfg.MaxTotalOutputTokens)
	}
	if cfg.MaxStreamBytes <= 0 || !cfg.DisableBudgetGrace || !cfg.DisableTokenFeedback || !cfg.DisableStreamRetry ||
		!cfg.RequireProviderModel || !cfg.RequireExplicitStopReason || !cfg.RequireStrictStopShape ||
		cfg.MaxOutputTokensRecovery != 0 || cfg.ToolLoopDetector != nil {
		t.Fatalf("briefcase hard-limit flags are incomplete: %+v", cfg)
	}
	if cfg.MaxToolCallAttempts == nil || *cfg.MaxToolCallAttempts != maxToolCallAttempts {
		t.Fatalf("MaxToolCallAttempts = %v, want %d", cfg.MaxToolCallAttempts, maxToolCallAttempts)
	}
	if cfg.StreamIdleTimeout != BriefcaseStreamIdleTimeout {
		t.Fatalf("StreamIdleTimeout = %s, want fixed %s", cfg.StreamIdleTimeout, BriefcaseStreamIdleTimeout)
	}
	if cfg.ParallelSafeTool != nil {
		t.Fatal("ParallelSafeTool must be nil in Briefcase mode despite DENEB_PARALLEL_TOOLS")
	}
}

func TestBuildAgentConfigProductionPreservesParallelToolPolicy(t *testing.T) {
	t.Setenv("DENEB_PARALLEL_TOOLS", "1")
	cfg, _, _, _ := buildAgentConfig(RunParams{}, runDeps{}, nil, nil, "", agentConfigDeps{}, slog.Default())

	if cfg.StreamIdleTimeout != 0 {
		t.Fatalf("production StreamIdleTimeout = %s, want zero so the executor can apply its normal env/default policy", cfg.StreamIdleTimeout)
	}
	if cfg.ParallelSafeTool == nil {
		t.Fatal("production ParallelSafeTool unexpectedly disabled")
	}
}

// This pins the highest-risk assembly seam: deterministic Briefcase policy,
// session-vs-run override precedence, replayed deferred-tool cache ordering,
// shared turn context, post-turn observers, and verification-gate exclusion
// all meet in buildAgentConfig. A refactor may split those responsibilities,
// but it must not silently reorder or drop any of them.
func TestBuildAgentConfig_PreservesCombinedPolicyAndHookContracts(t *testing.T) {
	t.Setenv("DENEB_PARALLEL_TOOLS", "1")
	t.Setenv("DENEB_REASONING_SANDWICH", "1")
	t.Setenv("DENEB_VERIFY_GATE", "1")

	registry := NewToolRegistry()
	registry.RegisterTool(ToolDef{Name: "write", Description: "write"})
	registry.RegisterTool(ToolDef{Name: "read", Description: "read"})
	registry.RegisterTool(ToolDef{Name: "wiki", Description: "wiki", Deferred: true})
	registry.RegisterTool(ToolDef{Name: "notebook", Description: "notebook", Deferred: true})

	interleaved := true
	cachedSession := &session.Session{
		ModelConfig: session.ModelConfig{
			ThinkingLevel:       "high",
			InterleavedThinking: &interleaved,
		},
		AgentConfig: session.AgentConfig{SpawnedBy: "client:main"},
	}
	maxTurns, maxTokens, maxToolCallAttempts := 7, 1234, 3
	params := RunParams{
		SessionKey:          "client:briefcase:risk",
		ClientRunID:         "run-risk",
		Model:               "requested-role",
		Thinking:            "off",
		MaxTurns:            &maxTurns,
		MaxTokens:           &maxTokens,
		MaxToolCallAttempts: &maxToolCallAttempts,
		AutoDeliveredOutput: true,
		ToolDryRun:          true,
	}

	nudger := &configNudger{}
	usage := &fakeUsageRecorder{}
	var heartbeat map[string]any
	acd := agentConfigDeps{
		Tools:       registry,
		MaxTokens:   9999,
		SkillNudger: nudger,
		ReplayDeferredTools: []string{
			"wiki",
			"notebook",
		},
		EmitAgentFn: func(kind, sessionKey, runID string, payload map[string]any) {
			if kind != "heartbeat" || sessionKey != params.SessionKey || runID != params.ClientRunID {
				t.Fatalf("unexpected heartbeat envelope: kind=%q session=%q run=%q", kind, sessionKey, runID)
			}
			heartbeat = payload
		},
	}
	shutdownCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	deps := runDeps{
		briefcaseMode: true,
		runLimits:     RunLimits{MaxTurns: 99, Timeout: time.Minute},
		callbacks:     CallbackSnapshot{shutdownCtx: shutdownCtx},
	}

	cfg, spawnFlag, execStats, skillConsults := buildAgentConfig(
		params,
		deps,
		cachedSession,
		json.RawMessage(`"system"`),
		string(toolpreset.PresetBriefcase),
		acd,
		slog.Default(),
	)

	if got := toolNames(cfg.Tools); !slices.Equal(got, []string{"read", "write", "wiki", "notebook"}) {
		t.Fatalf("initial tool order = %v, want sorted eager prefix plus replay-order tail", got)
	}
	if got := toolNames(cfg.DynamicToolsProvider()); !slices.Equal(got, []string{"wiki", "notebook"}) {
		t.Fatalf("dynamic replay order = %v, want first-activation order", got)
	}
	if cfg.MaxTurns != maxTurns || cfg.Timeout != time.Minute || cfg.MaxTokens != maxTokens {
		t.Fatalf("resolved budgets = turns %d timeout %s tokens %d", cfg.MaxTurns, cfg.Timeout, cfg.MaxTokens)
	}
	maxToolCallAttempts = 99
	if cfg.MaxToolCallAttempts == nil || *cfg.MaxToolCallAttempts != 3 {
		t.Fatalf("MaxToolCallAttempts was not copied: %v", cfg.MaxToolCallAttempts)
	}
	if cfg.Thinking == nil || cfg.Thinking.Type != "disabled" || cfg.Thinking.Interleaved {
		t.Fatalf("per-run thinking=off must beat the interleaved session default: %+v", cfg.Thinking)
	}
	if cfg.ThinkingModulator != nil || cfg.FinalizeGate != nil {
		t.Fatal("Briefcase must exclude ambient reasoning and verification policies")
	}
	productionCfg, _, _, _ := buildAgentConfig(
		RunParams{}, runDeps{}, cachedSession, nil, "", agentConfigDeps{MaxTokens: 65536}, slog.Default(),
	)
	if productionCfg.ThinkingModulator == nil || productionCfg.FinalizeGate == nil {
		t.Fatal("production must retain enabled reasoning and verification policies")
	}
	if cfg.ToolLoopDetector != nil || cfg.ParallelSafeTool != nil || !cfg.RequireExplicitStopReason || !cfg.RequireStrictStopShape {
		t.Fatal("Briefcase deterministic policy was not applied completely")
	}

	ctx := cfg.OnTurnInit(context.Background())
	if toolport.SessionKeyFromContext(ctx) != params.SessionKey ||
		toolport.ToolPresetFromContext(ctx) != string(toolpreset.PresetBriefcase) ||
		!toolport.AutoDeliveryFromContext(ctx) || !toolport.ToolDryRunFromContext(ctx) {
		t.Fatal("turn context lost run identity or delivery/dry-run policy")
	}
	if toolport.SpawnFlagFromContext(ctx) != spawnFlag || toolport.ToolExecStatsFromContext(ctx) != execStats {
		t.Fatal("turn context did not retain the returned run-scoped state")
	}
	if verifyGateFromContext(ctx) == nil {
		t.Fatal("turn context must retain verification bookkeeping even when Briefcase disables finish gating")
	}

	cfg.OnTurn(4, 321)
	if heartbeat["turn"] != 4 || heartbeat["tokens"] != 321 {
		t.Fatalf("heartbeat payload = %+v", heartbeat)
	}
	toolport.SkillConsultLogFromContext(ctx).Add("risk-skill")
	cfg.OnToolTurn(4, []agent.ToolActivity{{Name: "read"}})
	// Skill-usage attribution moved out of the OnToolTurn hook (which now only
	// drives the nudger) to an end-of-run recordRunSkillUsage against the
	// returned consult log; the resolved model is supplied at record time.
	recordRunSkillUsage(usage, skillConsults, &agent.AgentResult{Text: "done"}, nil, params.SessionKey, "resolved-model", nil)
	if len(usage.calls) != 1 || usage.calls[0].skill != "risk-skill" || usage.calls[0].model != "resolved-model" {
		t.Fatalf("skill usage attribution = %+v", usage.calls)
	}
	if len(nudger.calls) != 1 || nudger.calls[0].sessionKey != params.SessionKey || nudger.calls[0].count != 1 ||
		nudger.calls[0].snapshot.Model != params.Model || nudger.calls[0].snapshot.Turns != 4 {
		t.Fatalf("skill nudger hook calls = %+v", nudger.calls)
	}
}

func TestBuildAgentConfig_PreloadsExactTriggerDeferredTools(t *testing.T) {
	registry := NewToolRegistry()
	registry.RegisterTool(ToolDef{Name: "read", Description: "read"})
	registry.RegisterTool(ToolDef{Name: "wiki", Description: "wiki", Deferred: true})
	registry.RegisterTool(ToolDef{Name: "morning_letter", Description: "briefing", Deferred: true})

	acd := agentConfigDeps{
		Tools:                registry,
		ReplayDeferredTools:  []string{"wiki"},
		InitialDeferredTools: []string{"morning_letter", "wiki", "read", "missing_tool"},
	}
	cfg, _, _, _ := buildAgentConfig(
		RunParams{SessionKey: "cron:morning-letter:1"},
		runDeps{}, nil, nil, "", acd, slog.Default(),
	)
	if got := toolNames(cfg.Tools); !slices.Equal(got, []string{"read", "wiki", "morning_letter"}) {
		t.Fatalf("initial tools = %v, want replay then exact-trigger capability", got)
	}
	ctx := cfg.OnTurnInit(context.Background())
	activated := toolport.DeferredActivationFromContext(ctx)
	if activated == nil {
		t.Fatal("turn context missing deferred activation")
	}
	if got := activated.ActivatedNames(); !slices.Equal(got, []string{"wiki", "morning_letter"}) {
		t.Fatalf("seeded deferred tools = %v, want stable deduplicated order", got)
	}
}

func TestBuildAgentConfig_FiltersInitialDeferredToolsByPreset(t *testing.T) {
	registry := NewToolRegistry()
	registry.RegisterTool(ToolDef{Name: "read", Description: "read"})
	registry.RegisterTool(ToolDef{Name: "fetch_tools", Description: "fetch"})
	registry.RegisterTool(ToolDef{Name: "skill_lifecycle", Description: "lifecycle", Deferred: true})

	acd := agentConfigDeps{
		Tools:                registry,
		InitialDeferredTools: []string{"skill_lifecycle"},
	}
	cfg, _, _, _ := buildAgentConfig(
		RunParams{SessionKey: "client:main"},
		runDeps{}, nil, nil, "conversation", acd, slog.Default(),
	)
	if got := toolNames(cfg.Tools); slices.Contains(got, "skill_lifecycle") {
		t.Fatalf("conversation preset leaked preloaded skill_lifecycle: %v", got)
	}
	ctx := cfg.OnTurnInit(context.Background())
	activated := toolport.DeferredActivationFromContext(ctx)
	if activated == nil {
		t.Fatal("turn context missing deferred activation")
	}
	if got := activated.ActivatedNames(); len(got) != 0 {
		t.Fatalf("preset-excluded deferred tools were seeded: %v", got)
	}
}

func toolNames(tools []llm.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// The reasoning sandwich boosts BOTH ends — turn 0 (planning) and the
// verify/finish turn the gate is blocking (back half) — and stays out of the
// way (nil) in the middle so it composes cleanly with the effort router.
func TestReasoningSandwichThinkingReturnsBoostAtBothEnds(t *testing.T) {
	base := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 10240} // medium → boosts to 32768
	gate := &verifyGateState{}
	mod := reasoningSandwichThinking(base, 65536, gate)

	// Front: turn 0 boosts one tier.
	if got := mod(0, nil); got == nil || got.BudgetTokens != 32768 {
		t.Fatalf("turn 0 must boost to 32768, got %+v", got)
	}
	// Middle: no opinion (nil) so the executor falls back / the router composes.
	if got := mod(3, nil); got != nil {
		t.Fatalf("a middle turn must return nil (no opinion), got %+v", got)
	}
	// Back half: arm the gate (mutate + inject) so awaitingVerify is true.
	gate.recordTool("write", json.RawMessage(`{}`), "ok", nil)
	gate.finalizePrompt(nil) // injection arms the back-half trigger
	if got := mod(4, nil); got == nil || got.BudgetTokens != 32768 {
		t.Fatalf("the verify turn must re-boost to 32768, got %+v", got)
	}
	// Once verified, the back half disengages and the middle is nil again.
	gate.recordTool("exec", execInput("go build ./..."), "ok", nil)
	if got := mod(5, nil); got != nil {
		t.Fatalf("a verified later turn must return nil, got %+v", got)
	}
}

// When there is no headroom to grow a tier, the boost turns pin to the baseline
// (non-nil) rather than reasoning LESS than a plain turn — but the middle is
// still nil. gate==nil exercises the front-only path.
func TestReasoningSandwichThinking_NoHeadroomAndNilGate(t *testing.T) {
	base := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 10240}
	// maxTokens so low the 32768 tier can't fit with headroom → boost pins to base.
	mod := reasoningSandwichThinking(base, 12000, nil)
	if got := mod(0, nil); got != base {
		t.Fatalf("no-headroom front turn must pin to the baseline, got %+v", got)
	}
	if got := mod(2, nil); got != nil {
		t.Fatalf("middle turn must still be nil, got %+v", got)
	}
}
