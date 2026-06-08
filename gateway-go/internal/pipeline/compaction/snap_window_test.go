package compaction

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func trMsg(id string) llm.Message {
	return llm.NewBlockMessage("user", []llm.ContentBlock{
		{Type: "tool_result", ToolUseID: id, Content: "r"},
	})
}

func TestSnapWindowStart(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "q"),      // 0
		llm.NewTextMessage("assistant", "a"), // 1
		trMsg("t1"),                          // 2
		trMsg("t2"),                          // 3
		llm.NewTextMessage("assistant", "b"), // 4
	}
	cases := []struct{ in, want int }{
		{2, 4}, // on a tool_result → advance past the whole run
		{3, 4}, // on the second tool_result → advance
		{1, 1}, // on a normal message → unchanged
		{4, 4}, // on a normal message → unchanged
		{0, 0}, // on a normal message → unchanged
	}
	for _, c := range cases {
		if got := snapWindowStart(msgs, c.in); got != c.want {
			t.Errorf("snapWindowStart(_, %d) = %d, want %d", c.in, got, c.want)
		}
	}

	// Every message from startIdx on is a tool_result → returns len.
	allTR := []llm.Message{trMsg("a"), trMsg("b")}
	if got := snapWindowStart(allTR, 0); got != 2 {
		t.Errorf("all tool_result: got %d, want 2", got)
	}

	// startIdx == len → unchanged (no panic / no out-of-range).
	if got := snapWindowStart(msgs, len(msgs)); got != len(msgs) {
		t.Errorf("startIdx==len: got %d, want %d", got, len(msgs))
	}
}

func TestRecencyCompact_SnapsPastOrphanToolResult(t *testing.T) {
	// msg0 is large (forces a cut that keeps only the tail). The recency window
	// would otherwise start at the tool_result (msg1), orphaning it. Snap must
	// push the window start to msg2.
	big := strings.Repeat("가", 2000) // ~1000 tokens
	msgs := []llm.Message{
		llm.NewTextMessage("assistant", big), // 0 — evicted
		trMsg("t1"),                          // 1 — naive window start (orphan)
		llm.NewTextMessage("assistant", "x"), // 2 — clean boundary
		llm.NewTextMessage("user", "y"),      // 3
	}
	out, ok := RecencyCompact(NewConfig(100), msgs, nil)
	if !ok {
		t.Fatal("expected recency compaction to fire")
	}
	// out[0] is the drop-notice; the kept window is out[1:] and must not begin
	// with a tool_result (which would be an orphan).
	if len(out) < 2 {
		t.Fatalf("unexpected output length %d", len(out))
	}
	if isToolResultMessage(out[1].Content) {
		t.Fatal("kept window starts with an orphan tool_result — snap did not apply")
	}
}
