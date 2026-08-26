package transcript

import (
	"encoding/json"
	"strings"
	"testing"
)

func toolCallMsg() ChatMessage {
	return ChatMessage{Role: "assistant", Content: json.RawMessage(
		`[{"type":"thinking","thinking":"먼저 위키를 봐야겠다","signature":"AAAABBBBCCCCDDDD"},` +
			`{"type":"tool_use","name":"morning_letter","input":{"date":"2026-08-26"}}]`,
	)}
}

// Transcript search asks "does this message MENTION x", where a tool name is a
// legitimate hit. It used to match tool names only by accident — through the
// raw-JSON escape hatch, which also matched inside a thinking block's base64
// signature, so short queries hit noise. Narrowing that hatch removed both;
// SearchableText restores the signal without the noise.
func TestSearchFindsToolCallsButNotSignatures(t *testing.T) {
	store := NewMemoryTranscriptStore()
	if err := store.Append("client:main", toolCallMsg()); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("client:main", NewTextChatMessage("assistant", "모닝레터를 보냈어요", 2)); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"morning_letter", "위키를 봐야겠다", "2026-08-26"} {
		results, err := store.Search(q, 10)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(results) == 0 {
			t.Errorf("Search(%q) found nothing; tool calls are unsearchable", q)
		}
	}

	// The signature must never be searchable — it is provider bookkeeping, and
	// matching inside base64 is pure noise.
	results, err := store.Search("AAAABBBB", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("a thinking signature was searchable: %+v", results)
	}
}

// The message body a search matched must not carry the signature either — the
// match is returned to callers as evidence.
func TestSearchableTextExcludesTheSignature(t *testing.T) {
	msg := toolCallMsg()
	got := msg.SearchableText()
	if strings.Contains(got, "AAAABBBB") {
		t.Errorf("SearchableText leaked the signature: %q", got)
	}
	if !strings.Contains(got, "[도구 morning_letter]") {
		t.Errorf("SearchableText dropped the tool name: %q", got)
	}
	if !strings.Contains(got, "위키를 봐야겠다") {
		t.Errorf("SearchableText dropped the thinking prose: %q", got)
	}
}

// TextContent stays the "what did this SAY" answer — display, recall notes and
// the sub-agent last-text fallback must not start picking up tool markers.
func TestTextContentStaysFreeOfToolMarkers(t *testing.T) {
	msg := toolCallMsg()
	if got := msg.TextContent(); got != "" {
		t.Errorf("TextContent = %q, want empty for a tool-only message", got)
	}
}
