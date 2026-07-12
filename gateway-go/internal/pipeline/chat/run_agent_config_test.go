package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
)

// OnTurnInit is the only ctx-decoration point shared by BOTH run entries:
// runAgentAsync decorates its own ctx, but the SendSync path (miniapp.chat.send
// — the native client's sole entry) reaches RunAgent without that decoration.
// Session key and tool preset must therefore be injected here, or tools like
// sessions_spawn (parent attribution), polaris (session-scoped recall), and
// the preset Execute gate silently read empty values on the sync path.
func TestBuildAgentConfig_OnTurnInitSetsSessionKeyAndPreset(t *testing.T) {
	params := RunParams{SessionKey: "client:main"}
	cfg, _, _ := buildAgentConfig(params, runDeps{}, nil, nil, "researcher", agentConfigDeps{}, "m-test", slog.Default())

	if cfg.OnTurnInit == nil {
		t.Fatal("OnTurnInit must be set")
	}
	ctx := cfg.OnTurnInit(context.Background())

	if got := toolctx.SessionKeyFromContext(ctx); got != "client:main" {
		t.Errorf("session key from OnTurnInit ctx = %q, want %q", got, "client:main")
	}
	if got := toolctx.ToolPresetFromContext(ctx); got != "researcher" {
		t.Errorf("tool preset from OnTurnInit ctx = %q, want %q", got, "researcher")
	}
}

func TestBuildAgentConfig_HandlerRunLimitsOverrideModeDefaults(t *testing.T) {
	wantTimeout := 7 * time.Minute
	wantSeed := int64(42001)
	deps := runDeps{runLimits: RunLimits{MaxTurns: 123, Timeout: wantTimeout}, samplingSeed: &wantSeed}
	cfg, _, _ := buildAgentConfig(RunParams{}, deps, nil, nil, "briefcase", agentConfigDeps{}, "m-test", slog.Default())

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

func TestBuildAgentConfig_BriefcaseUsesHardDeterministicLimits(t *testing.T) {
	t.Setenv("DENEB_STREAM_IDLE_TIMEOUT_MS", "-1")
	t.Setenv("DENEB_PARALLEL_TOOLS", "1")
	maxTurns, maxTokens, maxToolCallAttempts := 7, 1234, 3
	cfg, _, _ := buildAgentConfig(RunParams{
		MaxTurns: &maxTurns, MaxTokens: &maxTokens, MaxToolCallAttempts: &maxToolCallAttempts,
	}, runDeps{
		briefcaseMode: true, runLimits: RunLimits{MaxTurns: 99, Timeout: time.Minute},
	}, nil, nil, string(toolpreset.PresetBriefcase), agentConfigDeps{MaxTokens: maxTokens}, "m-test", slog.Default())

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

func TestBuildAgentConfig_ProductionRetainsEnvironmentControlledParallelPolicy(t *testing.T) {
	t.Setenv("DENEB_PARALLEL_TOOLS", "1")
	cfg, _, _ := buildAgentConfig(RunParams{}, runDeps{}, nil, nil, "", agentConfigDeps{}, "m-test", slog.Default())

	if cfg.StreamIdleTimeout != 0 {
		t.Fatalf("production StreamIdleTimeout = %s, want zero so the executor can apply its normal env/default policy", cfg.StreamIdleTimeout)
	}
	if cfg.ParallelSafeTool == nil {
		t.Fatal("production ParallelSafeTool unexpectedly disabled")
	}
}

// The reasoning sandwich boosts BOTH ends — turn 0 (planning) and the
// verify/finish turn the gate is blocking (back half) — and stays out of the
// way (nil) in the middle so it composes cleanly with the effort router.
func TestReasoningSandwichThinking_BothEnds(t *testing.T) {
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
