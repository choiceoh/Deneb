package llm

// Wire-invisibility contract for ContentBlock.Metadata: the code-only
// sideband (pkg/toolmeta) persists in the transcript but must NEVER reach a
// provider. Both wire paths project blocks through explicit structs; these
// tests pin that projection so adding a field to the Anthropic wireBlock (or
// flattening the OpenAI conversion) can't silently start leaking it.

import (
	"encoding/json"
	"strings"
	"testing"
)

func metadataResultMessage(t *testing.T) Message {
	t.Helper()
	blocks := []ContentBlock{
		{
			Type:      "tool_result",
			ToolUseID: "toolu_1",
			Content:   "Activated 1 tool(s): graphify. You can now call them directly.",
			Metadata:  json.RawMessage(`{"activatedTools":["graphify"],"secret":"code-only"}`),
		},
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	return Message{Role: "user", Content: raw}
}

func TestMetadata_TranscriptRoundTrip(t *testing.T) {
	msg := metadataResultMessage(t)
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blocks[0].Metadata), "activatedTools") {
		t.Fatalf("metadata must survive the transcript round-trip, got %s", blocks[0].Metadata)
	}
}

func TestMetadata_NotOnAnthropicWire(t *testing.T) {
	msg := metadataResultMessage(t)
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		t.Fatal(err)
	}
	wire, err := marshalAnthropicBlocks(blocks)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "metadata") || strings.Contains(string(wire), "code-only") {
		t.Fatalf("Metadata leaked onto the Anthropic wire: %s", wire)
	}
	if !strings.Contains(string(wire), "Activated 1 tool(s)") {
		t.Fatalf("content must still reach the wire: %s", wire)
	}
}

func TestMetadata_NotOnOpenAIWire(t *testing.T) {
	c := NewClient("http://localhost", "key")
	msgs := c.convertMessagesToOpenAI([]Message{metadataResultMessage(t)}, false)
	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "code-only") {
		t.Fatalf("Metadata leaked onto the OpenAI wire: %s", raw)
	}
	if !strings.Contains(string(raw), "Activated 1 tool(s)") {
		t.Fatalf("content must still reach the wire: %s", raw)
	}
}
