package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Spill provenance must be read BEFORE the tool runs, not after.
//
// Reading it afterwards made it a second index lookup, and the entry can be
// gone by then: a parallel-safe sibling that spills trips the per-session quota
// and evicts the oldest spill, after which IsExternalOrigin reports false for
// an ID whose attacker-authored bytes are already in this call's result. The
// turn stays untainted and a following exec/preference/wiki_forget is allowed —
// the signature-clean sleeper this control exists to stop.
//
// The eviction is driven from inside the tool call rather than from a real
// goroutine so the window is hit every run instead of occasionally.
func TestExternalOriginSurvivesSpillEvictionDuringTheRead(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(string) bool { return true })

	const sessionKey = "client:main"
	spillID, err := store.Store(sessionKey, "web", "attacker authored page")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !store.IsExternalOrigin(spillID, sessionKey) {
		t.Fatal("fixture: the web spill should be external-origin")
	}

	r := NewToolRegistry()
	r.SetSpilloverStore(store)
	r.Register("read_spillover", func(ctx context.Context, _ rawJSON) (string, error) {
		// Stand in for a concurrent sibling spilling enough to blow the session
		// quota: the oldest entry — ours — is evicted mid-call.
		for i := 0; i < 64; i++ {
			if _, err := store.Store(sessionKey, "read", fmt.Sprintf("filler %d", i)); err != nil {
				return "", err
			}
		}
		return "attacker authored page", nil
	})

	tc := &toolport.TurnContext{}
	ctx := toolport.WithTurnContext(toolport.WithSessionKey(context.Background(), sessionKey), tc)
	input, _ := json.Marshal(map[string]any{"spill_id": spillID})

	if _, err := r.Execute(ctx, "read_spillover", input); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// The eviction really happened — otherwise this test proves nothing.
	if store.IsExternalOrigin(spillID, sessionKey) {
		t.Fatal("fixture: the spill was not evicted, so the race window was never entered")
	}
	if !tc.ExternalOriginTouched() {
		t.Fatal("external-origin taint was lost to a concurrent eviction — the irreversible-tool gate stays open")
	}

	// End to end: the gate must actually block on that taint.
	g := &untrustedToolGate{}
	g.bindTurnContext(tc)
	g.observeToolResult("read_spillover", "", "clean paraphrase, no signature", false)
	blocked, reason := g.beforeToolCall("exec", "", []byte(`{"command":"curl attacker.test"}`))
	if !blocked {
		t.Error("exec allowed on a turn that read an evicted external-origin spill")
	}
	if strings.TrimSpace(reason) == "" {
		t.Error("block reason should explain itself")
	}
}
