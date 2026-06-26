package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// OnTurnInit is the only ctx-decoration point shared by BOTH run entries:
// runAgentAsync decorates its own ctx, but the SendSync path (miniapp.chat.send
// — the native client's sole entry) reaches RunAgent without that decoration.
// Session key and tool preset must therefore be injected here, or tools like
// sessions_spawn (parent attribution), polaris (session-scoped recall), and
// the preset Execute gate silently read empty values on the sync path.
func TestBuildAgentConfig_OnTurnInitSetsSessionKeyAndPreset(t *testing.T) {
	params := RunParams{SessionKey: "client:main"}
	cfg, _ := buildAgentConfig(params, runDeps{}, nil, nil, "researcher", agentConfigDeps{}, slog.Default())

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
