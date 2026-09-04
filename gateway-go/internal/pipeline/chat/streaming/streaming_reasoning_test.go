package streaming

import (
	"encoding/json"
	"sync"
	"testing"
)

// TestEmitThinkingCarriesFullReasoning locks in the live-reasoning path: the
// broadcaster accumulates the whole reasoning stream and a throttled thinking
// frame carries it as `reasoningFull` (distinct from the chip-sized `preview`),
// so the client can grow a live expandable reasoning block.
func TestEmitThinkingCarriesFullReasoning(t *testing.T) {
	// appendThinking accumulates the full reasoning across deltas — the source of
	// the reasoningFull payload, unbounded by the 512-rune chip tail.
	acc := NewBroadcaster(nil, "s1", "r1")
	acc.appendThinking("첫 번째 조각. ")
	acc.appendThinking("두 번째 조각.")
	if got := acc.fullReasoning(); got != "첫 번째 조각. 두 번째 조각." {
		t.Fatalf("fullReasoning() = %q, want the concatenation of deltas", got)
	}

	// A throttled thinking frame carries reasoningFull for the client's live
	// expandable reasoning block (the chip still uses only `preview`).
	var mu sync.Mutex
	var full string
	sink := func(event string, data []byte) int {
		if event != EventThinking {
			return 1
		}
		var m struct {
			Payload struct {
				ReasoningFull string `json:"reasoningFull"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(data, &m)
		if m.Payload.ReasoningFull != "" {
			mu.Lock()
			full = m.Payload.ReasoningFull
			mu.Unlock()
		}
		return 1
	}
	sb := NewBroadcaster(sink, "s1", "r1")
	sb.EmitThinking("발신자 주소를 확인해야 한다. ")
	mu.Lock()
	got := full
	mu.Unlock()
	if got != "발신자 주소를 확인해야 한다. " {
		t.Fatalf("emitted reasoningFull = %q, want the accumulated reasoning", got)
	}
}

// TestMarkThinkingBreakMatchesTheAssembledShape locks the live reasoning text to
// the shape of AgentResult.Thinking: the executor puts a blank line between two
// turns' thinking (appendRunSection), so the stream must too. Without it the two
// texts differ by exactly those separators, and the SSE translator that matches
// one against the other loses its fast path on every multi-turn run.
func TestMarkThinkingBreakMatchesTheAssembledShape(t *testing.T) {
	sb := NewBroadcaster(nil, "s1", "r1")
	sb.appendThinking("first turn thought.")
	sb.MarkThinkingBreak()
	sb.appendThinking("second turn thought.")
	if got, want := sb.fullReasoning(), "first turn thought.\n\nsecond turn thought."; got != want {
		t.Fatalf("fullReasoning() = %q, want %q", got, want)
	}
}

func TestMarkThinkingBreakLeavesNoTrailingSeam(t *testing.T) {
	// The last turn of a run is followed by no further reasoning, and the
	// assembled text has no trailing blank line either.
	sb := NewBroadcaster(nil, "s1", "r1")
	sb.appendThinking("only thought.")
	sb.MarkThinkingBreak()
	if got := sb.fullReasoning(); got != "only thought." {
		t.Fatalf("fullReasoning() = %q, want no trailing seam", got)
	}

	// A seam before anything streamed is not a seam at all.
	empty := NewBroadcaster(nil, "s1", "r1")
	empty.MarkThinkingBreak()
	empty.appendThinking("first thought.")
	if got := empty.fullReasoning(); got != "first thought." {
		t.Fatalf("fullReasoning() = %q, want no leading seam", got)
	}
}

func TestMarkThinkingBreakLeavesTheChipTailAlone(t *testing.T) {
	// The chip tail is a rolling 512-rune preview; a blank line there would only
	// eat context out of a line the operator reads at a glance.
	sb := NewBroadcaster(nil, "s1", "r1")
	sb.appendThinking("first turn thought.")
	sb.MarkThinkingBreak()
	sb.appendThinking("second turn thought.")
	sb.thinkingMu.Lock()
	tail := string(sb.thinkingTail)
	sb.thinkingMu.Unlock()
	if tail != "first turn thought.second turn thought." {
		t.Fatalf("thinkingTail = %q, want the seam kept out of the chip", tail)
	}
}

func TestSetReasoningVisibleSilencesTheBlockNotTheChip(t *testing.T) {
	// `/think show off` must reach the expandable block, and only it: the chip
	// is a liveness indicator ("깊이 생각 중"), not the thinking text.
	var mu sync.Mutex
	var frames []map[string]any
	sink := func(event string, data []byte) int {
		if event != EventThinking {
			return 1
		}
		var m struct {
			Payload map[string]any `json:"payload"`
		}
		_ = json.Unmarshal(data, &m)
		mu.Lock()
		frames = append(frames, m.Payload)
		mu.Unlock()
		return 1
	}

	sb := NewBroadcaster(sink, "s1", "r1")
	sb.SetReasoningVisible(false)
	sb.EmitThinking("발신자 주소부터 확인해야 한다. ")

	mu.Lock()
	defer mu.Unlock()
	if len(frames) == 0 {
		t.Fatal("no thinking frame emitted — the chip must keep working")
	}
	for i, p := range frames {
		if _, has := p["reasoningFull"]; has {
			t.Fatalf("frame %d carried reasoningFull for a session with thinking off", i)
		}
	}
}
