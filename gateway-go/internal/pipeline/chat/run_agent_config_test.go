package chat

import (
	"context"
	"log/slog"
	"testing"

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
