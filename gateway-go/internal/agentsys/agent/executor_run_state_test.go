package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// blockingSecondTurnStreamer completes one scripted turn, then leaves the next
// stream open until the test cancels its context. It makes post-recovery and
// grace-turn stream cancellation deterministic without timer sleeps.
type blockingSecondTurnStreamer struct {
	first         []llm.StreamEvent
	calls         atomic.Int32
	secondStarted chan struct{}
	startedOnce   sync.Once
}

func (s *blockingSecondTurnStreamer) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	if s.calls.Add(1) == 1 {
		return runStateEventStream(s.first), nil
	}
	s.startedOnce.Do(func() { close(s.secondStarted) })
	return make(chan llm.StreamEvent), nil
}

func (s *blockingSecondTurnStreamer) Complete(context.Context, llm.ChatRequest) (string, error) {
	return "", nil
}

func runStateEventStream(events []llm.StreamEvent) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch
}

func cancelWhenTurnStarts(started <-chan struct{}, cancel context.CancelFunc) <-chan bool {
	observed := make(chan bool, 1)
	go func() {
		select {
		case <-started:
			observed <- true
		case <-time.After(time.Second):
			observed <- false
		}
		cancel()
	}()
	return observed
}

func TestRunAgentRunState_RecoveryCountSurvivesStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamer := &blockingSecondTurnStreamer{
		first:         textTurn(t, "partial answer", "max_tokens"),
		secondStarted: make(chan struct{}),
	}
	secondTurnObserved := cancelWhenTurnStarts(streamer.secondStarted, cancel)

	result := testutil.Must(RunAgent(
		ctx,
		AgentConfig{
			MaxTurns:                5,
			Timeout:                 3 * time.Second,
			MaxTokens:               1024,
			MaxOutputTokensRecovery: 1,
		},
		[]llm.Message{llm.NewTextMessage("user", "write")},
		streamer,
		nil,
		StreamHooks{},
		nil,
		nil,
	))

	if result.StopReason != "aborted" {
		t.Errorf("StopReason = %q, want aborted", result.StopReason)
	}
	if !<-secondTurnObserved {
		t.Fatal("recovery turn did not start before cancellation")
	}
	if result.MaxTokensRecoveries != 1 {
		t.Errorf("MaxTokensRecoveries = %d, want 1", result.MaxTokensRecoveries)
	}
	if result.BudgetGraceCall {
		t.Error("BudgetGraceCall = true after stream cancellation")
	}
	if len(result.FinalMessages) != 3 {
		t.Fatalf("FinalMessages = %d, want initial + recovered assistant/user exchange", len(result.FinalMessages))
	}
	if got := []string{result.FinalMessages[1].Role, result.FinalMessages[2].Role}; !reflect.DeepEqual(got, []string{"assistant", "user"}) {
		t.Errorf("recovery roles = %v, want [assistant user]", got)
	}
}

func TestRunAgentRunState_RecoveryCountSurvivesToolCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		textTurn(t, "partial answer", "max_tokens"),
		buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: "call-1", name: "slow", inputJSON: `{}`},
		}, 20, 5),
	}}
	exec := toolExecFunc(func(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
		cancel()
		return "", ctx.Err()
	})

	result := testutil.Must(RunAgent(
		ctx,
		AgentConfig{
			MaxTurns:                5,
			Timeout:                 3 * time.Second,
			MaxTokens:               1024,
			MaxOutputTokensRecovery: 1,
		},
		[]llm.Message{llm.NewTextMessage("user", "write")},
		streamer,
		exec,
		StreamHooks{},
		nil,
		nil,
	))

	if result.StopReason != "aborted" {
		t.Errorf("StopReason = %q, want aborted", result.StopReason)
	}
	if result.MaxTokensRecoveries != 1 {
		t.Errorf("MaxTokensRecoveries = %d, want 1", result.MaxTokensRecoveries)
	}
	if result.BudgetGraceCall {
		t.Error("BudgetGraceCall = true after tool cancellation")
	}
	assertToolTurnBalanced(t, result.FinalMessages)
}

func TestRunAgentRunState_GraceCancellationClearsInFlightFlag(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		streamer := &blockingSecondTurnStreamer{
			first: buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "call-1", name: "read", inputJSON: `{}`},
			}, 20, 5),
			secondStarted: make(chan struct{}),
		}
		secondTurnObserved := cancelWhenTurnStarts(streamer.secondStarted, cancel)

		result := testutil.Must(RunAgent(
			ctx,
			AgentConfig{MaxTurns: 1, Timeout: 3 * time.Second, MaxTokens: 1024},
			[]llm.Message{llm.NewTextMessage("user", "inspect")},
			streamer,
			newFakeToolExecutor(),
			StreamHooks{},
			nil,
			nil,
		))

		if result.StopReason != "aborted" {
			t.Errorf("StopReason = %q, want aborted", result.StopReason)
		}
		if !<-secondTurnObserved {
			t.Fatal("grace turn did not start before cancellation")
		}
		if !result.BudgetExhaustedInjected {
			t.Error("BudgetExhaustedInjected = false, want grace prompt injected")
		}
		if result.BudgetGraceCall {
			t.Error("BudgetGraceCall = true after grace stream cancellation")
		}
	})

	t.Run("tool", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "call-1", name: "read", inputJSON: `{}`},
			}, 20, 5),
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "call-2", name: "read", inputJSON: `{}`},
			}, 20, 5),
		}}
		var calls atomic.Int32
		exec := toolExecFunc(func(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
			if calls.Add(1) == 2 {
				cancel()
				return "", ctx.Err()
			}
			return "ok", nil
		})

		result := testutil.Must(RunAgent(
			ctx,
			AgentConfig{MaxTurns: 1, Timeout: 3 * time.Second, MaxTokens: 1024},
			[]llm.Message{llm.NewTextMessage("user", "inspect")},
			streamer,
			exec,
			StreamHooks{},
			nil,
			nil,
		))

		if result.StopReason != "aborted" {
			t.Errorf("StopReason = %q, want aborted", result.StopReason)
		}
		if !result.BudgetExhaustedInjected {
			t.Error("BudgetExhaustedInjected = false, want grace prompt injected")
		}
		if result.BudgetGraceCall {
			t.Error("BudgetGraceCall = true after grace tool cancellation")
		}
		assertToolTurnBalanced(t, result.FinalMessages)
	})
}

func TestAgentRunState_MultiTurnAggregation(t *testing.T) {
	state := newAgentRunState([]llm.Message{llm.NewTextMessage("user", "inspect")}, nil)
	narration := "Checking both sources now."
	final := "Final answer."

	state.recordTurn(&turnResult{
		text: narration,
		toolCalls: []llm.ContentBlock{
			{Type: "tool_use", Name: "read", Input: json.RawMessage(`{"path":"a"}`)},
			{Type: "tool_use", Name: "grep", Input: json.RawMessage(`{"q":"b"}`)},
		},
		contentBlocks: []llm.ContentBlock{{Type: "thinking", Thinking: "think one"}},
		usage: llm.TokenUsage{
			InputTokens:              10,
			OutputTokens:             3,
			CacheReadInputTokens:     4,
			CacheCreationInputTokens: 2,
		},
	})
	state.recordToolActivities([]ToolActivity{
		{Name: "read", Turn: 1, OutputRunes: 7},
		{Name: "grep", Turn: 1, OutputRunes: 11},
	})
	state.recordTurn(&turnResult{
		text:          final,
		contentBlocks: []llm.ContentBlock{{Type: "thinking", Thinking: "think two"}},
		usage: llm.TokenUsage{
			InputTokens:              20,
			OutputTokens:             5,
			CacheReadInputTokens:     6,
			CacheCreationInputTokens: 1,
		},
	})
	state.finalize()

	result := state.result
	if result.Text != final {
		t.Errorf("Text = %q, want %q", result.Text, final)
	}
	if want := narration + "\n\n" + final; result.AllText != want {
		t.Errorf("AllText = %q, want %q", result.AllText, want)
	}
	if result.DeliverableText != final {
		t.Errorf("DeliverableText = %q, want %q", result.DeliverableText, final)
	}
	if result.Thinking != "think one\n\nthink two" {
		t.Errorf("Thinking = %q, want exact two-turn join", result.Thinking)
	}
	if result.Usage != (llm.TokenUsage{InputTokens: 30, OutputTokens: 8, CacheReadInputTokens: 10, CacheCreationInputTokens: 3}) {
		t.Errorf("Usage = %+v, want summed usage", result.Usage)
	}
	if result.TotalTextChars != len(narration)+len(final) || result.TotalToolCalls != 2 {
		t.Errorf("totals = text:%d tools:%d", result.TotalTextChars, result.TotalToolCalls)
	}
	if want := map[string]int{"read": 1, "grep": 1}; !reflect.DeepEqual(result.ToolCounts, want) {
		t.Errorf("ToolCounts = %v, want %v", result.ToolCounts, want)
	}
	if state.priorToolOutputRunes != 18 || len(result.ToolActivities) != 2 {
		t.Errorf("tool activity aggregate = runes:%d activities:%d", state.priorToolOutputRunes, len(result.ToolActivities))
	}
}

func TestAgentRunState_TextHeadIsValidUTF8(t *testing.T) {
	state := newAgentRunState(nil, nil)
	text := strings.Repeat("가", 100) // 300 bytes; byte 200 lands mid-rune.
	stats := state.recordTurn(&turnResult{text: text})

	if !utf8.ValidString(stats.textHead) {
		t.Fatalf("textHead is invalid UTF-8: %q", stats.textHead)
	}
	if !strings.HasSuffix(stats.textHead, "…") {
		t.Errorf("textHead = %q, want truncation marker", stats.textHead)
	}
	if len(strings.TrimSuffix(stats.textHead, "…")) > 200 {
		t.Errorf("textHead payload bytes = %d, want <= 200", len(strings.TrimSuffix(stats.textHead, "…")))
	}
}
