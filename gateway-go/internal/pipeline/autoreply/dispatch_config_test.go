package autoreply

import (
	"context"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestDispatchFromConfig_AbortTrigger(t *testing.T) {
	mem := session.NewAbortMemory(100)
	deps := ReplyDeps{AbortMemory: mem}

	msg := &types.MsgContext{Body: "/stop"}
	cfg := DispatchConfig{SessionKey: "sess-1"}

	result := DispatchFromConfig(context.Background(), msg, cfg, deps)
	if !result.Handled {
		t.Fatal("expected abort to be handled")
	}
	if len(result.Payloads) != 0 {
		t.Fatalf("got %d, want no payloads for abort", len(result.Payloads))
	}
	if !mem.WasRecentlyAborted("sess-1", 5000) {
		t.Fatal("expected session to be recorded as recently aborted")
	}
}

func TestDispatchFromConfig_SkipRecentlyAborted(t *testing.T) {
	mem := session.NewAbortMemory(100)
	deps := ReplyDeps{AbortMemory: mem}

	// Record a recent abort.
	mem.Record("sess-1", time.Now().UnixMilli())

	msg := &types.MsgContext{Body: "hello"}
	cfg := DispatchConfig{SessionKey: "sess-1"}

	result := DispatchFromConfig(context.Background(), msg, cfg, deps)
	if !result.Handled {
		t.Fatal("expected recently aborted message to be handled (skipped)")
	}
	if len(result.Payloads) != 0 {
		t.Fatal("expected no payloads for skipped message")
	}
}

func TestDispatchFromConfig_CommandRouting(t *testing.T) {
	registry := handlers.NewCommandRegistry([]handlers.ChatCommandDefinition{
		{
			Key:         "ping",
			NativeName:  "ping",
			Description: "test ping",
			TextAliases: []string{"/ping"},
			AcceptsArgs: false,
			Scope:       handlers.ScopeText,
		},
	})

	router := handlers.NewCommandRouter(registry)
	router.Handle("ping", func(ctx handlers.CommandContext) (*handlers.CommandResult, error) {
		return &handlers.CommandResult{
			Reply:     "pong",
			SkipAgent: true,
		}, nil
	})

	deps := ReplyDeps{Registry: registry, Router: router}
	msg := &types.MsgContext{Body: "/ping"}
	cfg := DispatchConfig{SessionKey: "sess-1"}

	result := DispatchFromConfig(context.Background(), msg, cfg, deps)
	if !result.Handled {
		t.Fatal("expected command to be handled")
	}
	if len(result.Payloads) == 0 || result.Payloads[0].Text != "pong" {
		t.Fatalf("got %v, want 'pong' payload", result.Payloads)
	}
}

func TestExtractCommandKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/status", "status"},
		{"/model gpt-4", "model"},
		{"hello", ""},
		{"/reset\tnow", "reset"},
		{"/kill", "kill"},
	}
	for _, tt := range tests {
		got := extractCommandKey(tt.input)
		if got != tt.want {
			t.Errorf("extractCommandKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractCommandArgs(t *testing.T) {
	tests := []struct {
		normalized string
		cmdKey     string
		wantNil    bool
		wantRaw    string
	}{
		{"/model gpt-4", "model", false, "gpt-4"},
		{"/status", "status", true, ""},
		{"/kill now", "kill", false, "now"},
	}
	for _, tt := range tests {
		got := extractCommandArgs(tt.normalized, tt.cmdKey)
		if tt.wantNil {
			if got != nil {
				t.Errorf("extractCommandArgs(%q, %q) = %v, want nil", tt.normalized, tt.cmdKey, got)
			}
		} else {
			if got == nil {
				t.Fatalf("extractCommandArgs(%q, %q) = nil, want non-nil", tt.normalized, tt.cmdKey)
			}
			if got.Raw != tt.wantRaw {
				t.Errorf("extractCommandArgs(%q, %q).Raw = %q, want %q", tt.normalized, tt.cmdKey, got.Raw, tt.wantRaw)
			}
		}
	}
}

func TestOriginRouting(t *testing.T) {
	msg := &types.MsgContext{
		SessionOrigin: types.SessionOrigin{
			Channel:   "telegram",
			AccountID: "acc-1",
			ThreadID:  "thread-1",
		},
		To: "user-1",
	}
	routing := ResolveOriginRouting(msg)
	if routing.Channel != "telegram" || routing.To != "user-1" || routing.AccountID != "acc-1" || routing.ThreadID != "thread-1" {
		t.Errorf("unexpected routing: %+v", routing)
	}
}

func TestFollowupRunner_NoFollowup(t *testing.T) {
	runner := NewFollowupRunner(nil, 5)
	initial := AgentTurnConfig{}
	firstResult := &AgentTurnResult{
		Payloads: []types.ReplyPayload{{Text: "hello"}},
	}
	payloads := testutil.Must(runner.RunFollowups(context.Background(), initial, firstResult))
	if len(payloads) != 1 || payloads[0].Text != "hello" {
		t.Fatalf("got %v, want single payload 'hello'", payloads)
	}
}

func TestNeedsFollowup(t *testing.T) {
	if needsFollowup(nil) {
		t.Error("nil result should not need followup")
	}
	if needsFollowup(&AgentTurnResult{}) {
		t.Error("empty result should not need followup")
	}
	if needsFollowup(&AgentTurnResult{Payloads: []types.ReplyPayload{{Text: "hello"}}}) {
		t.Error("text-only result should not need followup")
	}
}
