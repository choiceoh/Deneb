package sessionops

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func toolTurn() toolport.ChatMessage {
	return toolport.ChatMessage{Role: "assistant", Content: json.RawMessage(
		`[{"type":"thinking","thinking":"위키를 보자","signature":"QQQQSIGQQQQ"},` +
			`{"type":"tool_use","name":"morning_letter","input":{"date":"2026-08-26"}}]`,
	)}
}

// sessions(action=search) matches with SearchableText (transcript/store.go), so
// it has to RENDER with it too — otherwise a hit on a tool name prints an empty
// line and the search claims to have found something it cannot show.
func TestSessionRenderingShowsToolCallsNotBlankRows(t *testing.T) {
	msg := toolTurn()
	rendered := msg.SearchableText()
	if !strings.Contains(rendered, "[도구 morning_letter]") {
		t.Fatalf("tool call not rendered: %q", rendered)
	}
	if strings.Contains(rendered, "QQQQSIG") {
		t.Errorf("signature leaked into a session rendering: %q", rendered)
	}
	// TextContent — the "what was said" answer — stays empty for this turn, which
	// is exactly why the renderers had to switch.
	if got := msg.TextContent(); got != "" {
		t.Errorf("TextContent = %q, want empty", got)
	}
}
