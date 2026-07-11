package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func journalTestRoles(messages []llm.Message) []string {
	roles := make([]string, len(messages))
	for i, message := range messages {
		roles[i] = message.Role
	}
	return roles
}

func journalTestCapture(target *[]llm.Message) func(llm.Message) {
	return func(message llm.Message) {
		*target = append(*target, message)
	}
}

func journalTestRequireRoles(t *testing.T, messages []llm.Message, want []string) {
	t.Helper()
	if got := journalTestRoles(messages); !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted roles = %v, want %v; messages=%+v", got, want, messages)
	}
}

func TestRunAgentMessageJournal_MaxTokensRecoveryPersistsExchange(t *testing.T) {
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		textTurn(t, "partial answer", "max_tokens"),
		textTurn(t, "final answer", "end_turn"),
	}}
	var persisted []llm.Message
	cfg := AgentConfig{
		MaxTurns:                5,
		Timeout:                 10 * time.Second,
		MaxTokens:               1024,
		MaxOutputTokensRecovery: 1,
		OnMessagePersist:        journalTestCapture(&persisted),
	}

	result := testutil.Must(RunAgent(
		context.Background(),
		cfg,
		[]llm.Message{llm.NewTextMessage("user", "write a long answer")},
		streamer,
		nil,
		StreamHooks{},
		nil,
		nil,
	))

	journalTestRequireRoles(t, persisted, []string{"assistant", "user", "assistant"})
	if result.TurnsPersisted != 3 {
		t.Fatalf("TurnsPersisted = %d, want 3 messages", result.TurnsPersisted)
	}
	if !strings.Contains(string(persisted[0].Content), "partial answer") {
		t.Errorf("first persisted assistant = %s, want truncated answer", persisted[0].Content)
	}
	if !strings.Contains(string(persisted[1].Content), "Output was truncated") {
		t.Errorf("persisted recovery prompt = %s, want resume instruction", persisted[1].Content)
	}
	if !strings.Contains(string(persisted[2].Content), "final answer") {
		t.Errorf("terminal persisted assistant = %s, want final answer", persisted[2].Content)
	}
}

func TestRunAgentMessageJournal_ThinkingRunawayPersistsExchange(t *testing.T) {
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		runawayThinkingTurn(t),
		textTurn(t, "direct final answer", "end_turn"),
	}}
	var persisted []llm.Message
	cfg := AgentConfig{
		Model:                   "deepseek-v4-flash",
		MaxTurns:                5,
		Timeout:                 10 * time.Second,
		MaxTokens:               1024,
		Thinking:                &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
		ThinkingOffRetry:        &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: "thinking"},
		MaxOutputTokensRecovery: 1,
		OnMessagePersist:        journalTestCapture(&persisted),
	}

	result := testutil.Must(RunAgent(
		context.Background(),
		cfg,
		[]llm.Message{llm.NewTextMessage("user", "answer directly")},
		streamer,
		nil,
		StreamHooks{},
		nil,
		nil,
	))

	journalTestRequireRoles(t, persisted, []string{"assistant", "user", "assistant"})
	if result.TurnsPersisted != 3 {
		t.Fatalf("TurnsPersisted = %d, want 3 messages", result.TurnsPersisted)
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(persisted[0].Content, &blocks); err != nil {
		t.Fatalf("decode persisted thinking assistant: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Type != "thinking" || blocks[0].Thinking == "" {
		t.Fatalf("persisted thinking assistant blocks = %+v, want non-empty thinking block", blocks)
	}
	if !strings.Contains(string(persisted[1].Content), "직전 응답이 분석") {
		t.Errorf("persisted thinking-off prompt = %s, want injected retry instruction", persisted[1].Content)
	}
	if !strings.Contains(string(persisted[2].Content), "direct final answer") {
		t.Errorf("terminal persisted assistant = %s, want direct final answer", persisted[2].Content)
	}
}

func TestRunAgentMessageJournal_FinalizeGatePersistsExchangeOnce(t *testing.T) {
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		buildTextTurnEvents("first finish", 10, 5),
		buildTextTurnEvents("verified finish", 12, 6),
	}}
	var persisted []llm.Message
	gateCalls := 0
	cfg := AgentConfig{
		MaxTurns:         5,
		Timeout:          10 * time.Second,
		MaxTokens:        1024,
		OnMessagePersist: journalTestCapture(&persisted),
		FinalizeGate: func(_ int) string {
			gateCalls++
			if gateCalls == 1 {
				return "[verification gate] run tests"
			}
			return ""
		},
	}

	result := testutil.Must(RunAgent(
		context.Background(),
		cfg,
		[]llm.Message{llm.NewTextMessage("user", "change a file")},
		streamer,
		nil,
		StreamHooks{},
		nil,
		nil,
	))

	journalTestRequireRoles(t, persisted, []string{"assistant", "user", "assistant"})
	if result.TurnsPersisted != 3 {
		t.Fatalf("TurnsPersisted = %d, want 3 messages", result.TurnsPersisted)
	}
	firstCount := 0
	for _, message := range persisted {
		if strings.Contains(string(message.Content), "first finish") {
			firstCount++
		}
	}
	if firstCount != 1 {
		t.Fatalf("first finishing assistant persisted %d times, want exactly once", firstCount)
	}
	if !strings.Contains(string(persisted[1].Content), "verification gate") {
		t.Errorf("persisted gate prompt = %s, want injected verification instruction", persisted[1].Content)
	}
	if !strings.Contains(string(persisted[2].Content), "verified finish") {
		t.Errorf("terminal persisted assistant = %s, want verified finish", persisted[2].Content)
	}
}

func TestRunAgentMessageJournal_TerminalAssistantPersistsExactlyOnce(t *testing.T) {
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		buildTextTurnEvents("done once", 10, 5),
	}}
	var persisted []llm.Message
	cfg := AgentConfig{
		MaxTurns:         3,
		Timeout:          10 * time.Second,
		MaxTokens:        1024,
		OnMessagePersist: journalTestCapture(&persisted),
	}

	result := testutil.Must(RunAgent(
		context.Background(),
		cfg,
		[]llm.Message{llm.NewTextMessage("user", "finish")},
		streamer,
		nil,
		StreamHooks{},
		nil,
		nil,
	))

	journalTestRequireRoles(t, persisted, []string{"assistant"})
	if result.TurnsPersisted != 1 {
		t.Fatalf("TurnsPersisted = %d, want 1 message", result.TurnsPersisted)
	}
	if !strings.Contains(string(persisted[0].Content), "done once") {
		t.Errorf("persisted assistant = %s, want terminal answer", persisted[0].Content)
	}
}
