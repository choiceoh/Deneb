package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func toolUseMessage(t *testing.T, name, args string) llm.Message {
	t.Helper()
	blocks := []llm.ContentBlock{{
		Type:  "tool_use",
		ID:    "call_1",
		Name:  name,
		Input: llm.FlexibleFromRaw([]byte(args)),
	}}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	return llm.Message{Role: "assistant", Content: llm.FlexibleFromRaw(raw)}
}

// The summarizer is told to preserve concrete facts (paths, commands, queries).
// For many tools that fact lives in the ARGUMENT while the result is terse —
// five exec calls whose results all read "STUCK" used to leave no trace of what
// ran (observed 2026-08-26 in a live compaction input).
func TestSerializeMessagesKeepsToolArguments(t *testing.T) {
	out := serializeMessages([]llm.Message{
		toolUseMessage(t, "exec", `{"command":"echo STUCK"}`),
	})

	if !strings.Contains(out, "echo STUCK") {
		t.Fatalf("the command must survive into the summarizer input: %q", out)
	}
}

func TestSerializeMessagesCapsHugeArguments(t *testing.T) {
	body := strings.Repeat("가", 5000)
	out := serializeMessages([]llm.Message{
		toolUseMessage(t, "write", `{"file_path":"/w/a.md","content":"`+body+`"}`),
	})

	if !strings.Contains(out, "/w/a.md") {
		t.Fatalf("the identifying argument must survive: %q", out[:200])
	}
	if len([]rune(out)) > maxToolArgRunes+200 {
		t.Fatalf("a file body must not flood the summarizer: %d runes", len([]rune(out)))
	}
}

func TestSerializeMessagesKeepsBareFormForEmptyArguments(t *testing.T) {
	out := serializeMessages([]llm.Message{toolUseMessage(t, "status", `{}`)})

	if !strings.Contains(out, "<tool: status>") {
		t.Fatalf("empty arguments keep the bare form: %q", out)
	}
}
