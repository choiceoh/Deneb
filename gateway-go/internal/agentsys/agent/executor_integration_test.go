package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// --- Test doubles ---

// fakeLLMStreamer returns pre-built SSE event sequences for each turn.
type fakeLLMStreamer struct {
	mu               sync.Mutex
	turns            [][]llm.StreamEvent // one event sequence per turn
	idx              int
	delay            time.Duration         // optional delay before sending events
	recordedThinking []*llm.ThinkingConfig // per-turn ChatRequest.Thinking, captured in StreamChat
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

func (f *fakeLLMStreamer) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	f.mu.Lock()
	f.recordedThinking = append(f.recordedThinking, req.Thinking)
	f.mu.Unlock()
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
	mu             sync.Mutex
	calls          []toolCall
	outputs        map[string]string // name -> output
	errors         map[string]error  // name -> error
	execTime       time.Duration     // simulated execution time
	provenanceRoot string
	onExecute      func(name string, input json.RawMessage)
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
	if f.onExecute != nil {
		f.onExecute(name, input)
	}

	if err, ok := f.errors[name]; ok {
		return "", err
	}
	if out, ok := f.outputs[name]; ok {
		return out, nil
	}
	return "ok", nil
}

func (f *fakeToolExecutor) ToolProvenanceRoot() string {
	return f.provenanceRoot
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
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
			} `json:"usage"`
		}{
			ID:    "msg_test",
			Model: "test-model",
			Usage: struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
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
	var cbd llm.ContentBlockDelta
	cbd.Index = index
	cbd.Delta.Type = "text_delta"
	cbd.Delta.Text = text
	payload, _ := json.Marshal(cbd)
	return llm.StreamEvent{Type: "content_block_delta", Payload: payload}
}

func toolInputDeltaEvent(index int, name, inputJSON string) llm.StreamEvent {
	// For tool_use, the name comes from content_block_start. The delta carries the JSON input.
	var cbd llm.ContentBlockDelta
	cbd.Index = index
	cbd.Delta.Type = "input_json_delta"
	cbd.Delta.PartialJSON = inputJSON
	payload, _ := json.Marshal(cbd)
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
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
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
		OnThinking: func(string) {
			atomic.AddInt32(&thinkingHits, 1)
		},
		OnToolStart: func(name, _ string, _ []byte) {
			atomic.AddInt32(&toolStarts, 1)
		},
		OnToolEmit: func(_, _ string, _ []byte) {
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

func TestRunAgent_LogsToolProvenance(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "foo.go")
	inputJSON := fmt.Sprintf(`{"file_path":%q,"new_string":"package main"}`, targetPath)
	tools := newFakeToolExecutor()
	tools.outputs["edit"] = "edited"
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_prov", name: "edit", inputJSON: inputJSON},
			}, 100, 30),
			buildTextTurnEvents("finished", 200, 40),
		},
	}

	logs := agentlog.NewWriter(t.TempDir())
	runLog := agentlog.NewRunLogger(logs, "client:prov", "run-prov")
	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "edit file")}
	_, err := RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, runLog)
	testutil.NoError(t, err)

	entries := logs.Read(agentlog.ReadOpts{SessionKey: "client:prov", Type: agentlog.TypeTurnTool}).Entries
	if len(entries) != 1 {
		t.Fatalf("turn.tool entries = %d, want 1", len(entries))
	}
	var data agentlog.TurnToolData
	if err := json.Unmarshal(entries[0].Data, &data); err != nil {
		t.Fatalf("unmarshal turn.tool data: %v", err)
	}
	if data.Name != "edit" || data.ToolUseID != "toolu_prov" {
		t.Errorf("tool identity = %+v, want edit/toolu_prov", data)
	}
	if data.InputBytes != len(inputJSON) {
		t.Errorf("InputBytes = %d, want %d", data.InputBytes, len(inputJSON))
	}
	if len(data.InputHash) != 64 || len(data.OutputHash) != 64 {
		t.Errorf("hashes not full sha256: input=%q output=%q", data.InputHash, data.OutputHash)
	}
	if data.OutputLen != len("edited") {
		t.Errorf("OutputLen = %d, want %d", data.OutputLen, len("edited"))
	}
	if len(data.Targets) != 1 || !strings.HasSuffix(data.Targets[0], "/foo.go") {
		t.Errorf("Targets = %+v, want sanitized foo.go path", data.Targets)
	}
}

func TestRunAgent_LogsMutatingToolFileEffects(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(targetPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inputJSON := fmt.Sprintf(`{"file_path":%q,"content":"new\nline\n"}`, targetPath)
	tools := newFakeToolExecutor()
	tools.provenanceRoot = dir
	tools.outputs["write"] = "wrote"
	tools.onExecute = func(name string, input json.RawMessage) {
		if name != "write" {
			return
		}
		var p struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(input, &p); err == nil {
			_ = os.WriteFile(p.FilePath, []byte(p.Content), 0o644)
		}
	}
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_write", name: "write", inputJSON: inputJSON},
			}, 100, 30),
			buildTextTurnEvents("finished", 200, 40),
		},
	}

	logs := agentlog.NewWriter(t.TempDir())
	runLog := agentlog.NewRunLogger(logs, "client:effects", "run-effects")
	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "write file")}
	_, err := RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, runLog)
	testutil.NoError(t, err)

	entries := logs.Read(agentlog.ReadOpts{SessionKey: "client:effects", Type: agentlog.TypeTurnTool}).Entries
	if len(entries) != 1 {
		t.Fatalf("turn.tool entries = %d, want 1", len(entries))
	}
	var data agentlog.TurnToolData
	if err := json.Unmarshal(entries[0].Data, &data); err != nil {
		t.Fatalf("unmarshal turn.tool data: %v", err)
	}
	if len(data.FileEffects) != 1 {
		t.Fatalf("FileEffects = %+v, want one effect", data.FileEffects)
	}
	effect := data.FileEffects[0]
	if !effect.ExistsBefore || !effect.ExistsAfter || !effect.Changed {
		t.Errorf("effect existence/change = %+v, want existing changed file", effect)
	}
	if effect.BeforeHash == "" || effect.AfterHash == "" || effect.BeforeHash == effect.AfterHash {
		t.Errorf("hash transition = %q -> %q, want distinct hashes", effect.BeforeHash, effect.AfterHash)
	}
	if effect.BeforeLines != 1 || effect.AfterLines != 2 {
		t.Errorf("line counts = %d -> %d, want 1 -> 2", effect.BeforeLines, effect.AfterLines)
	}
	if effect.AddedLines != 2 || effect.RemovedLines != 1 {
		t.Errorf("line delta = +%d/-%d, want +2/-1", effect.AddedLines, effect.RemovedLines)
	}
}

func TestRunAgent_SkipsFileEffectsOutsideProvenanceRoot(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.go")
	if err := os.WriteFile(outsidePath, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inputJSON := fmt.Sprintf(`{"file_path":%q,"content":"changed\n"}`, outsidePath)
	tools := newFakeToolExecutor()
	tools.provenanceRoot = dir
	tools.outputs["write"] = "rejected"
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_write_outside", name: "write", inputJSON: inputJSON},
			}, 100, 30),
			buildTextTurnEvents("finished", 200, 40),
		},
	}

	logs := agentlog.NewWriter(t.TempDir())
	runLog := agentlog.NewRunLogger(logs, "client:effects-outside", "run-effects-outside")
	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 4096,
	}

	messages := []llm.Message{llm.NewTextMessage("user", "write outside file")}
	_, err := RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, runLog)
	testutil.NoError(t, err)

	entries := logs.Read(agentlog.ReadOpts{SessionKey: "client:effects-outside", Type: agentlog.TypeTurnTool}).Entries
	if len(entries) != 1 {
		t.Fatalf("turn.tool entries = %d, want 1", len(entries))
	}
	var data agentlog.TurnToolData
	if err := json.Unmarshal(entries[0].Data, &data); err != nil {
		t.Fatalf("unmarshal turn.tool data: %v", err)
	}
	if len(data.FileEffects) != 0 {
		t.Fatalf("FileEffects = %+v, want no effects for path outside provenance root", data.FileEffects)
	}
}

// TestRunAgent_ThinkingModulatorPerTurn asserts the AgentConfig.ThinkingModulator
// contract: the executor calls it per turn and uses its return value in the
// ChatRequest, falling back to cfg.Thinking when the modulator returns nil.
func TestRunAgent_ThinkingModulatorPerTurn(t *testing.T) {
	streamer := &fakeLLMStreamer{
		turns: [][]llm.StreamEvent{
			// Turn 0: a tool call so the loop continues to a second turn.
			buildToolUseTurnEventsWithNames([]toolUseSpec{
				{id: "toolu_1", name: "read", inputJSON: `{"path":"x"}`},
			}, 100, 30),
			// Turn 1: text end_turn.
			buildTextTurnEvents("done", 120, 20),
		},
	}
	tools := newFakeToolExecutor()

	baseline := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 5000}
	var seenActs [][]ToolActivity
	cfg := AgentConfig{
		MaxTurns:  10,
		Timeout:   10 * time.Second,
		MaxTokens: 200000,
		Thinking:  baseline,
		// Turn 0 is boosted; later turns return nil to exercise the fallback
		// to cfg.Thinking.
		ThinkingModulator: func(turn int, acts []ToolActivity) *llm.ThinkingConfig {
			seenActs = append(seenActs, append([]ToolActivity(nil), acts...))
			if turn == 0 {
				return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 32768}
			}
			return nil
		},
	}

	messages := []llm.Message{llm.NewTextMessage("user", "go")}
	_ = testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))

	if len(streamer.recordedThinking) < 2 {
		t.Fatalf("recorded %d turns of Thinking, want >= 2", len(streamer.recordedThinking))
	}
	if got := streamer.recordedThinking[0]; got == nil || got.BudgetTokens != 32768 {
		t.Errorf("turn 0 Thinking = %+v, want budget 32768 (modulator override)", got)
	}
	// Turn 1 modulator returned nil → executor must fall back to cfg.Thinking.
	if got := streamer.recordedThinking[1]; got == nil || got.BudgetTokens != 5000 {
		t.Errorf("turn 1 Thinking = %+v, want budget 5000 (fallback to cfg.Thinking)", got)
	}
	// The modulator receives this run's accumulated tool activities: none
	// before turn 0, the turn-0 read call (with its 1-based Turn and recorded
	// output size) before turn 1.
	if len(seenActs) < 2 || len(seenActs[0]) != 0 || len(seenActs[1]) != 1 {
		t.Fatalf("modulator activities = %+v, want [] then [read]", seenActs)
	}
	if a := seenActs[1][0]; a.Name != "read" || a.Turn != 1 || a.IsError {
		t.Errorf("turn-1 activity = %+v, want clean read at Turn 1", a)
	}
}
