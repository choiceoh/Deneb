package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestComposeBeforeAPICall_AllNilReturnsNil(t *testing.T) {
	got := agent.ComposeBeforeAPICall(nil, nil, nil)
	if got != nil {
		t.Fatalf("expected nil when all hooks are nil, got non-nil")
	}
	// Zero args: also nil.
	if agent.ComposeBeforeAPICall() != nil {
		t.Fatalf("expected nil for empty hook list")
	}
}

func TestComposeBeforeAPICall_SingleHookReturnedDirectly(t *testing.T) {
	called := 0
	h := func(m []llm.Message) []llm.Message {
		called++
		return m
	}
	fn := agent.ComposeBeforeAPICall(nil, h, nil)
	if fn == nil {
		t.Fatalf("expected non-nil composed fn")
	}
	fn([]llm.Message{})
	if called != 1 {
		t.Fatalf("expected single hook invoked once, got %d", called)
	}
}

func TestComposeBeforeAPICall_ChainThreadsMessagesInOrder(t *testing.T) {
	// Three hooks each record their tag; the composed fn must call them
	// in registration order so side effects land as h1 → h2 → h3.
	seq := []string{}
	mk := func(tag string) func([]llm.Message) []llm.Message {
		return func(m []llm.Message) []llm.Message {
			seq = append(seq, tag)
			return m
		}
	}
	fn := agent.ComposeBeforeAPICall(mk("a"), mk("b"), mk("c"))
	fn([]llm.Message{{Role: "user"}})
	if len(seq) != 3 || seq[0] != "a" || seq[1] != "b" || seq[2] != "c" {
		t.Fatalf("expected a→b→c order, got %v", seq)
	}
}

// TestComposeBeforeAPICall_ChainPassesMessagesThrough verifies each hook sees
// the output of the previous one (not the original input).
func TestComposeBeforeAPICall_ChainPassesMessagesThrough(t *testing.T) {
	appendBlock := func(tag string) func([]llm.Message) []llm.Message {
		return func(msgs []llm.Message) []llm.Message {
			if len(msgs) == 0 {
				return msgs
			}
			last := len(msgs) - 1
			// Decode existing blocks (if any), append a new one, re-encode.
			var blocks []llm.ContentBlock
			if len(msgs[last].Content) > 0 {
				_ = json.Unmarshal(msgs[last].Content, &blocks)
			}
			blocks = append(blocks, llm.ContentBlock{Type: "text", Text: tag})
			raw, _ := json.Marshal(blocks)
			out := append([]llm.Message(nil), msgs...)
			out[last] = llm.Message{Role: msgs[last].Role, Content: raw}
			return out
		}
	}
	fn := agent.ComposeBeforeAPICall(appendBlock("a"), appendBlock("b"), appendBlock("c"))
	got := fn([]llm.Message{{Role: "user"}})
	if len(got) != 1 {
		t.Fatalf("message count = %d, want 1", len(got))
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(got[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal blocks: %v", err)
	}
	if len(blocks) != 3 || blocks[0].Text != "a" || blocks[1].Text != "b" || blocks[2].Text != "c" {
		t.Fatalf("expected ordered blocks a/b/c, got %+v", blocks)
	}
}

func TestComposeBeforeAPICall_FiltersNilAmongMultiple(t *testing.T) {
	got := []string{}
	mk := func(tag string) func([]llm.Message) []llm.Message {
		return func(m []llm.Message) []llm.Message {
			got = append(got, tag)
			return m
		}
	}
	fn := agent.ComposeBeforeAPICall(nil, mk("x"), nil, mk("y"), nil)
	if fn == nil {
		t.Fatalf("expected non-nil composed fn")
	}
	fn(nil)
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("expected nils filtered, chain x→y, got %v", got)
	}
}
