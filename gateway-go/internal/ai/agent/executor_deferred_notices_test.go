package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func newRunnerForTest(t *testing.T, cfg AgentConfig, messages []llm.Message) *agentRunner {
	t.Helper()
	runner, err := newAgentRunner(context.Background(), cfg, messages, nil, nil, StreamHooks{}, nil, nil)
	if err != nil {
		t.Fatalf("newAgentRunner: %v", err)
	}
	t.Cleanup(runner.close)
	return runner
}

func TestToolResultsForNextTurnAppendsDeferredTurnNotices(t *testing.T) {
	drains := [][]string{{"child A done", "child B done"}, nil}
	call := 0
	runner := newRunnerForTest(t, AgentConfig{
		DeferredTurnNotices: func() []string {
			notices := drains[call]
			call++
			return notices
		},
	}, nil)
	outcome := toolTurnOutcome{results: []llm.ContentBlock{
		{Type: "tool_result", ToolUseID: "t1", Content: "ok"},
	}}

	got := runner.toolResultsForNextTurn(0, outcome)
	if len(got) != 3 {
		t.Fatalf("blocks = %d, want tool result + 2 notice text blocks", len(got))
	}
	if got[1].Type != "text" || got[1].Text != "child A done" ||
		got[2].Type != "text" || got[2].Text != "child B done" {
		t.Fatalf("notice blocks = %+v, want drained notices in order", got[1:])
	}

	// Drained channel: no extra blocks on the next tool turn.
	if again := runner.toolResultsForNextTurn(1, outcome); len(again) != 1 {
		t.Fatalf("post-drain blocks = %d, want bare tool result", len(again))
	}
}

func TestToolResultsForNextTurnCanceledSkipsNotices(t *testing.T) {
	runner := newRunnerForTest(t, AgentConfig{
		DeferredTurnNotices: func() []string { return []string{"never delivered"} },
	}, nil)
	outcome := toolTurnOutcome{
		canceled: true,
		results:  []llm.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Content: "ok"}},
	}

	if got := runner.toolResultsForNextTurn(0, outcome); len(got) != 1 {
		t.Fatalf("canceled turn appended notices: %d blocks, want 1", len(got))
	}
}

// oversizedToolResultMessage builds a user message holding one tool_result
// whose content exceeds CompactedMaxOutput, i.e. eligible for the mid-run
// shrink.
func oversizedToolResultMessage() llm.Message {
	return llm.NewBlockMessage("user", []llm.ContentBlock{{
		Type:      "tool_result",
		ToolUseID: "t1",
		Content:   strings.Repeat("x\n", CompactedMaxOutput),
	}})
}

func TestCompactPriorToolResultsGatedForContentPrefixCacheProviders(t *testing.T) {
	history := []llm.Message{oversizedToolResultMessage()}
	runner := newRunnerForTest(t, AgentConfig{DisablePriorToolResultCompaction: true}, history)
	before := string(runner.journal.messages[0].Content.Bytes())

	runner.compactPriorToolResults(1, len(runner.journal.messages))

	if got := string(runner.journal.messages[0].Content.Bytes()); got != before {
		t.Fatal("history mutated despite DisablePriorToolResultCompaction — content-prefix caches would cold-start")
	}
}

func TestCompactPriorToolResultsStillShrinksWhenEnabled(t *testing.T) {
	history := []llm.Message{oversizedToolResultMessage()}
	runner := newRunnerForTest(t, AgentConfig{}, history)
	before := string(runner.journal.messages[0].Content.Bytes())

	runner.compactPriorToolResults(1, len(runner.journal.messages))

	if got := string(runner.journal.messages[0].Content.Bytes()); got == before {
		t.Fatal("prior tool result not compacted on the default (marker-cache) path")
	}
}
