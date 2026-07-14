package chatport

import (
	"encoding/json"
	"testing"
)

func TestChatMessageWireJSONRoundTripsStably(t *testing.T) {
	message := ChatMessage{
		Role:    "assistant",
		Content: json.RawMessage(`[{"type":"text","text":"hello"}]`),
		Attachments: []ChatAttachment{{
			Type: "file", MimeType: "text/plain", URL: "file:///tmp/a.txt",
			Data: "YQ==", Name: "a.txt", Size: 1,
		}},
		Timestamp: 42,
		ParentID:  "parent-1",
		ID:        "message-1",
	}

	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"role":"assistant","content":[{"type":"text","text":"hello"}],"attachments":[{"type":"file","mimeType":"text/plain","url":"file:///tmp/a.txt","data":"YQ==","name":"a.txt","size":1}],"timestamp":42,"parentId":"parent-1","id":"message-1"}`
	if string(encoded) != want {
		t.Fatalf("wire JSON = %s\nwant      = %s", encoded, want)
	}

	var decoded ChatMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.TextContent(); got != "hello" {
		t.Fatalf("TextContent() = %q, want hello", got)
	}
}

func TestSearchResultEncodesStableWireJSON(t *testing.T) {
	result := SearchResult{
		SessionKey: "client:main",
		Matches: []MatchedMsg{{
			Index:   7,
			Message: NewTextChatMessage("user", "needle", 11),
			Context: []ChatMessage{NewTextChatMessage("assistant", "context", 12)},
		}},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"sessionKey":"client:main","matches":[{"index":7,"message":{"role":"user","content":"needle","timestamp":11},"context":[{"role":"assistant","content":"context","timestamp":12}]}]}`
	if string(encoded) != want {
		t.Fatalf("wire JSON = %s\nwant      = %s", encoded, want)
	}
}

func TestChatMessageTextContentParsesAndJoinsTextBlocks(t *testing.T) {
	message := ChatMessage{Content: json.RawMessage(`[
		{"type":"text","text":"one"},
		{"type":"tool_use","name":"read"},
		{"type":"text","text":"two"},
		{"type":"text","text":"three"}
	]`)}
	if got := message.TextContent(); got != "one\n\ntwo\n\nthree" {
		t.Fatalf("TextContent() = %q", got)
	}
}
