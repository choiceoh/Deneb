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
