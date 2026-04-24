// steer_test.go — Unit tests for SteerQueue and the drain-inject path.
package chat

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestSteerQueue_Enqueue_AcceptsAndTrims(t *testing.T) {
	q := NewSteerQueue()
	if !q.Enqueue("sess", "  nudge  ") {
		t.Fatal("expected accept for non-empty note")
	}
	if got := q.Len("sess"); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}
	notes := q.Drain("sess")
	if len(notes) != 1 || notes[0] != "nudge" {
		t.Fatalf("drain = %v, want [\"nudge\"]", notes)
	}
}

func TestSteerQueue_Enqueue_RejectsEmptyAndWhitespace(t *testing.T) {
	q := NewSteerQueue()
	if q.Enqueue("sess", "") {
		t.Error("empty note should be rejected")
	}
	if q.Enqueue("sess", "   \n\t ") {
		t.Error("whitespace-only note should be rejected")
	}
	if q.Enqueue("", "ok") {
		t.Error("empty session should be rejected")
	}
	if got := q.Len("sess"); got != 0 {
		t.Errorf("Len = %d, want 0 after rejections", got)
	}
}

func TestSteerQueue_Enqueue_MultipleConcat(t *testing.T) {
	q := NewSteerQueue()
	q.Enqueue("sess", "first")
	q.Enqueue("sess", "second")
	q.Enqueue("sess", "third")
	notes := q.Drain("sess")
	if len(notes) != 3 {
		t.Fatalf("drain len = %d, want 3", len(notes))
	}
	if notes[0] != "first" || notes[1] != "second" || notes[2] != "third" {
		t.Errorf("drain order = %v, want [first second third]", notes)
	}
}

func TestSteerQueue_Drain_EmptyReturnsNil(t *testing.T) {
	q := NewSteerQueue()
	if got := q.Drain("sess"); got != nil {
		t.Errorf("Drain on empty = %v, want nil", got)
	}
	if got := q.Drain(""); got != nil {
		t.Errorf("Drain with empty session = %v, want nil", got)
	}
}

func TestSteerQueue_Restore_PrependsOrder(t *testing.T) {
	q := NewSteerQueue()
	q.Enqueue("sess", "newcomer")
	// Simulate a drain-then-restore path (no tool_result found).
	q.Restore("sess", []string{"original-1", "original-2"})
	notes := q.Drain("sess")
	if len(notes) != 3 {
		t.Fatalf("drain len = %d, want 3", len(notes))
	}
	want := []string{"original-1", "original-2", "newcomer"}
	for i, w := range want {
		if notes[i] != w {
			t.Errorf("notes[%d] = %q, want %q", i, notes[i], w)
		}
	}
}

func TestSteerQueue_Clear(t *testing.T) {
	q := NewSteerQueue()
	q.Enqueue("sess", "x")
	q.Clear("sess")
	if q.Len("sess") != 0 {
		t.Error("Clear did not empty the queue")
	}
}

func TestSteerQueue_Reset(t *testing.T) {
	q := NewSteerQueue()
	q.Enqueue("s1", "a")
	q.Enqueue("s2", "b")
	q.Reset()
	if q.Len("s1") != 0 || q.Len("s2") != 0 {
		t.Error("Reset did not empty all queues")
	}
}

// TestSteerQueue_ConcurrentEnqueueDrain exercises the race detector: many
// concurrent Enqueues paired with concurrent Drains must not panic and must
// preserve at-most-once delivery (sum of observed notes equals inserted count).
func TestSteerQueue_ConcurrentEnqueueDrain(t *testing.T) {
	q := NewSteerQueue()
	const writers = 8
	const perWriter = 50

	var wg sync.WaitGroup
	// Producers
	for i := range writers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range perWriter {
				q.Enqueue("sess", "note")
			}
		}(i)
	}
	// Consumers that drain and tally
	var mu sync.Mutex
	var total int
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 30 {
				got := q.Drain("sess")
				mu.Lock()
				total += len(got)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// Final drain to catch any stragglers.
	total += len(q.Drain("sess"))
	want := writers * perWriter
	if total != want {
		t.Errorf("total notes observed = %d, want %d", total, want)
	}
}

// TestSteerInject_AppendsToLastToolResult confirms the core drain-and-
// inject behaviour: the marker must land on the LAST tool_result block in the
// LAST user message, and the original messages slice must not be mutated.
func TestSteerInject_AppendsToLastToolResult(t *testing.T) {
	// Build: user(text) / assistant(tool_use) / user(tool_result) / assistant(text)
	toolResultBlock := llm.ContentBlock{
		Type:      "tool_result",
		ToolUseID: "tu_1",
		Content:   "ran tool X",
	}
	msgs := []llm.Message{
		llm.NewTextMessage("user", "please run X"),
		llm.NewBlockMessage("assistant", []llm.ContentBlock{{Type: "tool_use", ID: "tu_1", Name: "x"}}),
		llm.NewBlockMessage("user", []llm.ContentBlock{toolResultBlock}),
	}
	orig := cloneMessagesForTest(msgs)

	marker := "\n\n[사용자 조정: skip tests]"
	out, ok := injectSteerMarker(msgs, marker)
	if !ok {
		t.Fatalf("injectSteerMarker returned ok=false; expected injection")
	}

	// Original slice untouched.
	if !messagesEqual(msgs, orig) {
		t.Error("original messages slice was mutated; expected shallow-copy isolation")
	}
	// Injection landed on the tool_result block.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(out[2].Content, &blocks); err != nil {
		t.Fatalf("unmarshal injected block: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Type != "tool_result" {
		t.Fatalf("unexpected blocks %v", blocks)
	}
	if !strings.Contains(blocks[0].Content, "skip tests") {
		t.Errorf("tool_result content = %q, want to contain marker", blocks[0].Content)
	}
	if !strings.HasPrefix(blocks[0].Content, "ran tool X") {
		t.Errorf("tool_result content lost original text: %q", blocks[0].Content)
	}
}

// TestSteerInject_NoToolResultReturnsFalse verifies the "stay pending"
// path: no tool_result yet means injection is skipped and the caller restores.
func TestSteerInject_NoToolResultReturnsFalse(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "hello"),
		llm.NewTextMessage("assistant", "hi"),
	}
	out, ok := injectSteerMarker(msgs, "\n\n[사용자 조정: x]")
	if ok {
		t.Error("expected ok=false when no tool_result exists")
	}
	if !messagesEqual(out, msgs) {
		t.Error("out should equal input when no injection happened")
	}
}

// TestSteerInject_PrefersLastToolResult covers the multi-turn case:
// when several tool_result messages exist, injection must target the LATEST.
func TestSteerInject_PrefersLastToolResult(t *testing.T) {
	msgs := []llm.Message{
		llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", Content: "old"},
		}),
		llm.NewBlockMessage("assistant", []llm.ContentBlock{{Type: "tool_use", ID: "tu_2", Name: "y"}}),
		llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu_2", Content: "new"},
		}),
	}
	out, ok := injectSteerMarker(msgs, "\n\n[사용자 조정: z]")
	if !ok {
		t.Fatal("expected ok=true")
	}

	// First tool_result must be untouched.
	var first []llm.ContentBlock
	if err := json.Unmarshal(out[0].Content, &first); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if strings.Contains(first[0].Content, "z") {
		t.Error("old tool_result was mutated; expected only the latest to change")
	}

	// Latest tool_result must carry the marker.
	var latest []llm.ContentBlock
	if err := json.Unmarshal(out[2].Content, &latest); err != nil {
		t.Fatalf("unmarshal latest: %v", err)
	}
	if !strings.Contains(latest[0].Content, "z") {
		t.Errorf("latest tool_result content = %q, want to contain marker", latest[0].Content)
	}
}

// TestSteerBeforeAPICall_EndToEnd glues SteerQueue + buildSteerBeforeAPICall
// together and checks the expected drain-and-inject happens on call.
func TestSteerBeforeAPICall_EndToEnd(t *testing.T) {
	q := NewSteerQueue()
	q.Enqueue("sess", "do extra step")

	hook := buildSteerBeforeAPICall(q, "sess", nil)
	if hook == nil {
		t.Fatal("buildSteerBeforeAPICall returned nil with valid inputs")
	}

	msgs := []llm.Message{
		llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", Content: "done"},
		}),
	}

	out := hook(msgs)
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(out[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(blocks[0].Content, "do extra step") {
		t.Errorf("marker not present in tool_result content: %q", blocks[0].Content)
	}
	// Queue must be drained after successful injection.
	if q.Len("sess") != 0 {
		t.Errorf("queue len = %d, want 0 after drain-and-inject", q.Len("sess"))
	}
}

// TestSteerBeforeAPICall_PreservesOrderOnNoToolResult: when nothing to inject
// into, the notes must be Restored so the NEXT call (after tools ran) sees them.
func TestSteerBeforeAPICall_PreservesOrderOnNoToolResult(t *testing.T) {
	q := NewSteerQueue()
	q.Enqueue("sess", "pending")

	hook := buildSteerBeforeAPICall(q, "sess", nil)
	msgs := []llm.Message{llm.NewTextMessage("user", "hi")}
	out := hook(msgs)

	if !messagesEqual(out, msgs) {
		t.Error("expected no mutation when no tool_result exists")
	}
	if q.Len("sess") != 1 {
		t.Errorf("queue len = %d, want 1 after restore", q.Len("sess"))
	}
	// Now simulate a tool_result arriving and a second call: injection should land.
	msgs2 := []llm.Message{
		llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", Content: "ok"},
		}),
	}
	out2 := hook(msgs2)
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(out2[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(blocks[0].Content, "pending") {
		t.Errorf("expected restored note to inject; got %q", blocks[0].Content)
	}
	if q.Len("sess") != 0 {
		t.Errorf("queue should be empty after successful injection; got len=%d", q.Len("sess"))
	}
}

// --- helpers -----------------------------------------------------------

func cloneMessagesForTest(in []llm.Message) []llm.Message {
	out := make([]llm.Message, len(in))
	for i, m := range in {
		cp := make(json.RawMessage, len(m.Content))
		copy(cp, m.Content)
		out[i] = llm.Message{Role: m.Role, Content: cp}
	}
	return out
}

func messagesEqual(a, b []llm.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role {
			return false
		}
		if string(a[i].Content) != string(b[i].Content) {
			return false
		}
	}
	return true
}
