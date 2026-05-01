package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// --- Test doubles ---

// fakeLLMStreamer returns pre-built SSE event sequences for each turn.
type fakeLLMStreamer struct {
	mu    sync.Mutex
	turns [][]llm.StreamEvent // one event sequence per turn
	idx   int
	delay time.Duration // optional delay before sending events
}

func (f *fakeLLMStreamer) next() []llm.StreamEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idx >= len(f.turns) {
		return nil
	}
	events := f.turns[f.idx]
	f.idx++
	return events
}

func (f *fakeLLMStreamer) StreamChat(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return f.stream(), nil
}

func (f *fakeLLMStreamer) Complete(_ context.Context, _ llm.ChatRequest) (string, error) {
	return "", nil
}

func (f *fakeLLMStreamer) stream() <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent, 64)
	events := f.next()
	go func() {
		defer close(ch)
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		for _, ev := range events {
			ch <- ev
		}
	}()
	return ch
}

// fakeToolExecutor records calls and returns configurable outputs.
type fakeToolExecutor struct {
	mu       sync.Mutex
	calls    []toolCall
	outputs  map[string]string // name -> output
	errors   map[string]error  // name -> error
	execTime time.Duration     // simulated execution time
}

type toolCall struct {
	Name  string
	Input json.RawMessage
}

func newFakeToolExecutor() *fakeToolExecutor {
	return &fakeToolExecutor{
		outputs: make(map[string]string),
		errors:  make(map[string]error),
	}
}

func (f *fakeToolExecutor) Execute(_ context.Context, name string, input json.RawMessage) (string, error) {
	if f.execTime > 0 {
		time.Sleep(f.execTime)
	}
	f.mu.Lock()
	f.calls = append(f.calls, toolCall{Name: name, Input: input})
	f.mu.Unlock()

	if err, ok := f.errors[name]; ok {
		return "", err
	}
	if out, ok := f.outputs[name]; ok {
		return out, nil
	}
	return "ok", nil
}

func (f *fakeToolExecutor) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeToolExecutor) callNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, len(f.calls))
	for i, c := range f.calls {
		names[i] = c.Name
	}
	return names
}

// --- SSE event builders ---

// buildTextTurnEvents creates a complete Anthropic SSE sequence for a text-only turn.
func buildTextTurnEvents(text string, inputTokens, outputTokens int) []llm.StreamEvent {
	return []llm.StreamEvent{
		messageStartEvent(inputTokens),
		contentBlockStartEvent(0, "text", ""),
		textDeltaEvent(0, text),
		contentBlockStopEvent(0),
		messageDeltaEvent("end_turn", outputTokens),
		{Type: "message_stop"},
	}
}

type toolUseSpec struct {
	id        string
	name      string
	inputJSON string
}

func messageStartEvent(inputTokens int) llm.StreamEvent {
	payload, _ := json.Marshal(llm.MessageStart{
		Message: struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}{
			ID:    "msg_test",
			Model: "test-model",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: inputTokens},
		},
	})
	return llm.StreamEvent{Type: "message_start", Payload: payload}
}

func contentBlockStartEvent(index int, blockType, id string) llm.StreamEvent {
	block := llm.ContentBlock{Type: blockType}
	if id != "" {
		block.ID = id
	}
	payload, _ := json.Marshal(llm.ContentBlockStart{
		Index:        index,
		ContentBlock: block,
	})
	return llm.StreamEvent{Type: "content_block_start", Payload: payload}
}

func textDeltaEvent(index int, text string) llm.StreamEvent {
	payload, _ := json.Marshal(llm.ContentBlockDelta{
		Index: index,
		Delta: llm.ContentBlockDeltaInner{Type: "text_delta", Text: text},
	})
	return llm.StreamEvent{Type: "content_block_delta", Payload: payload}
}

func toolInputDeltaEvent(index int, name, inputJSON string) llm.StreamEvent {
	// For tool_use, the name comes from content_block_start. The delta carries the JSON input.
	// We need to update the content_block_start to include the name.
	// Actually, the name is set in content_block_start; the delta carries partial_json.
	payload, _ := json.Marshal(llm.ContentBlockDelta{
		Index: index,
		Delta: llm.ContentBlockDeltaInner{Type: "input_json_delta", PartialJSON: inputJSON},
	})
	return llm.StreamEvent{Type: "content_block_delta", Payload: payload}
}

func contentBlockStopEvent(index int) llm.StreamEvent {
	payload, _ := json.Marshal(struct {
		Index int `json:"index"`
	}{Index: index})
	return llm.StreamEvent{Type: "content_block_stop", Payload: payload}
}

func messageDeltaEvent(stopReason string, outputTokens int) llm.StreamEvent {
	payload, _ := json.Marshal(llm.MessageDelta{
		Delta: struct {
			StopReason string `json:"stop_reason"`
		}{StopReason: stopReason},
		Usage: struct {
			OutputTokens int `json:"output_tokens"`
		}{OutputTokens: outputTokens},
	})
	return llm.StreamEvent{Type: "message_delta", Payload: payload}
}

// contentBlockStartToolUseEvent creates a content_block_start for a tool_use block
// with the tool name and ID set (which is how Anthropic sends them).
func contentBlockStartToolUseEvent(index int, id, name string) llm.StreamEvent {
	block := llm.ContentBlock{Type: "tool_use", ID: id, Name: name}
	payload, _ := json.Marshal(llm.ContentBlockStart{
		Index:        index,
		ContentBlock: block,
	})
	return llm.StreamEvent{Type: "content_block_start", Payload: payload}
}

// buildToolUseTurnEventsWithNames creates SSE events with proper tool names in content_block_start.
func buildToolUseTurnEventsWithNames(tools []toolUseSpec, inputTokens, outputTokens int) []llm.StreamEvent {
	events := []llm.StreamEvent{messageStartEvent(inputTokens)}
	for i, t := range tools {
		events = append(events,
			contentBlockStartToolUseEvent(i, t.id, t.name),
			toolInputDeltaEvent(i, t.name, t.inputJSON),
			contentBlockStopEvent(i),
		)
	}
	events = append(events,
		messageDeltaEvent("tool_use", outputTokens),
		llm.StreamEvent{Type: "message_stop"},
	)
	return events
}

// --- Integration Tests ---

func TestRunAgent_HappyPath_TextOnly(t *testing.T) {
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildTextTurnEvents("Hello, world!", 100, 50),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  5,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
		System:    llm.SystemString("You are a test assistant."),
	}

	messages := []llm.Message{llm.NewTextMessage("user", "Hi")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if result.Text != "Hello, world!" {
		t.Errorf("Text = %q, want %q", result.Text, "Hello, world!")
	}
	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
	if result.Turns != 1 {
		t.Errorf("Turns = %d, want 1", result.Turns)
	}
	if result.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", result.Usage.OutputTokens)
	}
}

func TestRunAgent_SingleToolCall(t *testing.T) {
	tools := newFakeToolExecutor()
	tools.outputs["read"] = "file contents here"

	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			// Turn 1: LLM calls the read tool.
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "read", inputJSON: `{"path":"test.go"}`},
			}, 100, 30),
			// Turn 2: LLM responds with text after seeing tool result.
			buildTextTurnEvents("The file contains test code.", 200, 60),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "Read test.go")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	if result.Text != "The file contains test code." {
		t.Errorf("Text = %q, want %q", result.Text, "The file contains test code.")
	}
	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
	if result.Turns != 2 {
		t.Errorf("Turns = %d, want 2", result.Turns)
	}
	if tools.callCount() != 1 {
		t.Fatalf("tool call count = %d, want 1", tools.callCount())
	}
	if names := tools.callNames(); names[0] != "read" {
		t.Errorf("tool name = %q, want %q", names[0], "read")
	}
}

func TestRunAgent_MaxTurns(t *testing.T) {
	// LLM keeps calling tools indefinitely. Post-grace-call: the executor
	// injects a wrap-up user message after the final budgeted turn and runs
	// one extra iteration. Since this test's streamer runs out of scripted
	// events on the grace iteration, the empty stream resolves as end_turn —
	// but the grace flag flips the terminal stop reason to max_turns_graceful.
	// Tool count stays at maxTurns (the grace iteration produces no tool calls
	// because the fake stream is empty).
	const maxTurns = 3
	turns := make([][]llm.StreamEvent, maxTurns)
	for i := range turns {
		turns[i] = buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: fmt.Sprintf("toolu_%d", i), name: "exec", inputJSON: `{"cmd":"ls"}`},
		}, 50, 20)
	}

	streamer := &fakeLLMStreamer{turns: turns}
	tools := newFakeToolExecutor()

	cfg := AgentConfig{
		MaxTurns:  maxTurns,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "loop")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	if result.StopReason != StopReasonMaxTurnsGraceful {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxTurnsGraceful)
	}
	if !result.BudgetExhaustedInjected {
		t.Error("BudgetExhaustedInjected = false, want true")
	}
	if result.Turns != maxTurns+1 {
		t.Errorf("Turns = %d, want %d (MaxTurns + 1 grace iteration)", result.Turns, maxTurns+1)
	}
	if tools.callCount() != maxTurns {
		t.Errorf("tool call count = %d, want %d (grace turn runs no tools here)",
			tools.callCount(), maxTurns)
	}
}

func TestRunAgent_Timeout(t *testing.T) {
	// Streamer delays longer than timeout.
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildTextTurnEvents("should not arrive", 100, 50),
		},
		delay: 500 * time.Millisecond,
	}

	cfg := AgentConfig{
		MaxTurns:  5,
		Timeout:   100 * time.Millisecond,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "slow")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if result.StopReason != "timeout" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "timeout")
	}
}

func TestRunAgent_ContextCancellation(t *testing.T) {
	// Cancel context before events arrive.
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildTextTurnEvents("should not arrive", 100, 50),
		},
		delay: 500 * time.Millisecond,
	}

	cfg := AgentConfig{
		MaxTurns:  5,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	messages := []llm.Message{llm.NewTextMessage("user", "abort")}
	result := testutil.Must(RunAgent(ctx, cfg, messages, streamer, nil, StreamHooks{}, nil, nil))

	if result.StopReason != "aborted" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "aborted")
	}
}

func TestRunAgent_ToolError(t *testing.T) {
	tools := newFakeToolExecutor()
	tools.errors["write"] = fmt.Errorf("permission denied: /etc/passwd")

	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			// Turn 1: LLM calls write.
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "write", inputJSON: `{"path":"/etc/passwd","content":"x"}`},
			}, 100, 30),
			// Turn 2: LLM responds with error explanation.
			buildTextTurnEvents("Permission denied for that file.", 200, 40),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	// Capture the tool_result to verify is_error.
	var capturedIsError bool
	hooks := StreamHooks{
		OnToolResult: func(_, _, _ string, isErr bool) {
			capturedIsError = isErr
		},
	}

	messages := []llm.Message{llm.NewTextMessage("user", "write to /etc/passwd")}
	result := testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, hooks, nil, nil))

	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
	if !capturedIsError {
		t.Error("expected tool result to be marked as error")
	}
	if result.Text != "Permission denied for that file." {
		t.Errorf("Text = %q, want error explanation", result.Text)
	}
}

// TestRunAgent_MultipleToolCalls verifies that when the LLM emits multiple
// tool_use blocks in a single turn, the executor runs them sequentially and
// feeds all results back on the next turn. Parallel tool execution has been
// removed; tools always run one at a time in stream order.
func TestRunAgent_MultipleToolCalls(t *testing.T) {
	tools := newFakeToolExecutor()
	tools.outputs["read"] = "file A"
	tools.outputs["grep"] = "match found"
	tools.outputs["find"] = "/src/main.go"

	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			// Turn 1: LLM emits 3 tool calls.
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "read", inputJSON: `{"path":"a.go"}`},
				{id: "toolu_2", name: "grep", inputJSON: `{"pattern":"func"}`},
				{id: "toolu_3", name: "find", inputJSON: `{"pattern":"*.go"}`},
			}, 100, 40),
			// Turn 2: LLM responds with summary.
			buildTextTurnEvents("Found relevant code.", 300, 50),
		},
	}

	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "search")}
	result, err := RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil)
	testutil.NoError(t, err)

	if tools.callCount() != 3 {
		t.Errorf("tool call count = %d, want 3", tools.callCount())
	}
	if result.Turns != 2 {
		t.Errorf("Turns = %d, want 2", result.Turns)
	}
}

func TestRunAgent_OnTurnInit_Hook(t *testing.T) {
	type ctxKey struct{}
	var capturedTurnValues []int

	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "exec", inputJSON: `{"cmd":"echo"}`},
			}, 50, 20),
			buildTextTurnEvents("done", 100, 30),
		},
	}

	turnCounter := 0
	tools := &contextCapturingToolExecutor{
		key:     ctxKey{},
		capture: &capturedTurnValues,
	}

	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
		OnTurnInit: func(ctx context.Context) context.Context {
			turnCounter++
			return context.WithValue(ctx, ctxKey{}, turnCounter)
		},
	}

	messages := []llm.Message{llm.NewTextMessage("user", "test")}
	_, err := RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil)
	testutil.NoError(t, err)

	if turnCounter != 2 {
		t.Errorf("OnTurnInit called %d times, want 2", turnCounter)
	}
	if len(capturedTurnValues) != 1 || capturedTurnValues[0] != 1 {
		t.Errorf("captured turn values = %v, want [1]", capturedTurnValues)
	}
}

// contextCapturingToolExecutor extracts a value from ctx during tool execution.
type contextCapturingToolExecutor struct {
	key     any
	capture *[]int
	mu      sync.Mutex
}

func (c *contextCapturingToolExecutor) Execute(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
	if v, ok := ctx.Value(c.key).(int); ok {
		c.mu.Lock()
		*c.capture = append(*c.capture, v)
		c.mu.Unlock()
	}
	return "ok", nil
}

func TestRunAgent_OnTurn_Callback(t *testing.T) {
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "exec", inputJSON: `{}`},
			}, 100, 30),
			buildTextTurnEvents("done", 200, 50),
		},
	}

	tools := newFakeToolExecutor()
	var turnCalls []struct{ turn, tokens int }

	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
		OnTurn: func(turn int, accumulatedTokens int) {
			turnCalls = append(turnCalls, struct{ turn, tokens int }{turn, accumulatedTokens})
		},
	}

	messages := []llm.Message{llm.NewTextMessage("user", "test")}
	_, err := RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil)
	testutil.NoError(t, err)

	if len(turnCalls) != 2 {
		t.Fatalf("OnTurn called %d times, want 2", len(turnCalls))
	}
	if turnCalls[0].turn != 1 {
		t.Errorf("first OnTurn turn = %d, want 1", turnCalls[0].turn)
	}
	if turnCalls[1].turn != 2 {
		t.Errorf("second OnTurn turn = %d, want 2", turnCalls[1].turn)
	}
	// Second call should have higher accumulated tokens.
	if turnCalls[1].tokens <= turnCalls[0].tokens {
		t.Errorf("expected increasing accumulated tokens: %d then %d", turnCalls[0].tokens, turnCalls[1].tokens)
	}
}

func TestRunAgent_StreamHooks_Called(t *testing.T) {
	tools := newFakeToolExecutor()
	tools.outputs["exec"] = "done"

	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "exec", inputJSON: `{"cmd":"ls"}`},
			}, 100, 30),
			buildTextTurnEvents("finished", 200, 40),
		},
	}

	var (
		textDeltas   []string
		toolStarts   int32
		toolEmits    int32
		toolResults  int32
		thinkingHits int32
	)

	hooks := StreamHooks{
		OnTextDelta: func(text string) {
			textDeltas = append(textDeltas, text)
		},
		OnThinking: func() {
			atomic.AddInt32(&thinkingHits, 1)
		},
		OnToolStart: func(name, _ string, _ []byte) {
			atomic.AddInt32(&toolStarts, 1)
		},
		OnToolEmit: func(_, _ string) {
			atomic.AddInt32(&toolEmits, 1)
		},
		OnToolResult: func(_, _, _ string, _ bool) {
			atomic.AddInt32(&toolResults, 1)
		},
	}

	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "test")}
	_, err := RunAgent(context.Background(), cfg, messages, streamer, tools, hooks, nil, nil)
	testutil.NoError(t, err)

	if len(textDeltas) == 0 {
		t.Error("OnTextDelta was never called")
	}
	if atomic.LoadInt32(&toolStarts) != 1 {
		t.Errorf("OnToolStart called %d times, want 1", toolStarts)
	}
	if atomic.LoadInt32(&toolEmits) != 1 {
		t.Errorf("OnToolEmit called %d times, want 1", toolEmits)
	}
	if atomic.LoadInt32(&toolResults) != 1 {
		t.Errorf("OnToolResult called %d times, want 1", toolResults)
	}
}
