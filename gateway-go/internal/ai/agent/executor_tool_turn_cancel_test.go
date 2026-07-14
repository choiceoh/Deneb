package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

type toolTurnCapture struct {
	persisted     []llm.Message
	activityTurns [][]ToolActivity
}

func (c *toolTurnCapture) configure(cfg *AgentConfig) {
	cfg.OnMessagePersist = func(msg llm.Message) {
		c.persisted = append(c.persisted, msg)
	}
	cfg.OnToolTurn = func(_ int, activities []ToolActivity) {
		c.activityTurns = append(c.activityTurns, append([]ToolActivity(nil), activities...))
	}
}

func toolTurnTestConfig() AgentConfig {
	return AgentConfig{
		MaxTurns:  5,
		Timeout:   5 * time.Second,
		MaxTokens: 4096,
	}
}

func toolTurnTestStreamer(specs ...toolUseSpec) *fakeLLMStreamer {
	return &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		buildToolUseTurnEventsWithNames(specs, 100, 30),
	}}
}

func decodeToolTurnBlocks(t *testing.T, msg llm.Message) []llm.ContentBlock {
	t.Helper()
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil {
		t.Fatalf("decode %s message blocks: %v (content=%s)", msg.Role, err, msg.Content)
	}
	return blocks
}

func collectToolTurnBlocks(t *testing.T, messages []llm.Message) (uses, results []llm.ContentBlock) {
	t.Helper()
	for i, msg := range messages {
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil {
			continue // plain text content
		}
		for _, block := range blocks {
			if block.Type == "" {
				t.Fatalf("message %d contains a zero-value content block: %s", i, msg.Content)
			}
			switch block.Type {
			case "tool_use":
				uses = append(uses, block)
			case "tool_result":
				results = append(results, block)
			}
		}
	}
	return uses, results
}

func assertToolTurnBalanced(t *testing.T, messages []llm.Message) {
	t.Helper()
	uses, results := collectToolTurnBlocks(t, messages)
	if len(uses) != len(results) {
		t.Fatalf("tool block count is unbalanced: uses=%d results=%d", len(uses), len(results))
	}
	resultIDs := make(map[string]int, len(results))
	for _, block := range results {
		resultIDs[block.ToolUseID]++
	}
	for _, block := range uses {
		if resultIDs[block.ID] != 1 {
			t.Errorf("tool_use %q has %d matching results, want 1", block.ID, resultIDs[block.ID])
		}
	}
}

func assertCanceledToolTurnCommit(t *testing.T, result *AgentResult, capture *toolTurnCapture, wantActivities int) []llm.ContentBlock {
	t.Helper()
	if result.StopReason != "aborted" {
		t.Errorf("StopReason = %q, want aborted", result.StopReason)
	}
	if result.TurnsPersisted != 2 {
		t.Errorf("TurnsPersisted = %d, want 2", result.TurnsPersisted)
	}
	if len(capture.persisted) != 2 {
		t.Fatalf("persisted messages = %d, want assistant + tool_result", len(capture.persisted))
	}
	if capture.persisted[0].Role != "assistant" || capture.persisted[1].Role != "user" {
		t.Fatalf("persisted roles = %q, %q; want assistant, user", capture.persisted[0].Role, capture.persisted[1].Role)
	}
	if len(capture.activityTurns) != 1 {
		t.Fatalf("OnToolTurn calls = %d, want 1", len(capture.activityTurns))
	}
	if len(capture.activityTurns[0]) != wantActivities {
		t.Errorf("OnToolTurn activities = %d, want %d", len(capture.activityTurns[0]), wantActivities)
	}
	assertToolTurnBalanced(t, capture.persisted)
	assertToolTurnBalanced(t, result.FinalMessages)
	return decodeToolTurnBlocks(t, capture.persisted[1])
}

func TestRunAgent_CancelAfterSuccessfulSequentialToolCommitsResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var executed []string
	exec := toolExecFunc(func(_ context.Context, name string, _ json.RawMessage) (string, error) {
		executed = append(executed, name)
		cancel() // The side effect completed, but the next call must not start.
		return "committed", nil
	})
	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "write", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "send", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "do both")},
		streamer, exec, StreamHooks{}, nil, nil,
	))
	results := assertCanceledToolTurnCommit(t, result, capture, 1)

	if !reflect.DeepEqual(executed, []string{"write"}) {
		t.Errorf("executed = %v, want only write", executed)
	}
	if len(results) != 2 {
		t.Fatalf("tool results = %d, want 2", len(results))
	}
	if results[0].ToolUseID != "call-1" || results[0].Content != "committed" || results[0].IsError {
		t.Errorf("successful result was not preserved: %+v", results[0])
	}
	if results[1].ToolUseID != "call-2" || results[1].Content != interruptedBeforeToolStart || !results[1].IsError {
		t.Errorf("unstarted result = %+v, want synthetic interruption", results[1])
	}
	if !reflect.DeepEqual(result.InterruptedToolNames, []string{"send"}) {
		t.Errorf("InterruptedToolNames = %v, want [send]", result.InterruptedToolNames)
	}
	if len(result.ToolActivities) != 1 || result.ToolActivities[0].Name != "write" || result.ToolActivities[0].IsError {
		t.Errorf("ToolActivities = %+v, want successful write only", result.ToolActivities)
	}
	if !reflect.DeepEqual(capture.activityTurns[0], result.ToolActivities) {
		t.Errorf("OnToolTurn activities = %+v, result activities = %+v", capture.activityTurns[0], result.ToolActivities)
	}
}

func TestRunAgent_CancelDuringSequentialToolCommitsErrorAndSyntheticResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var executed []string
	exec := toolExecFunc(func(ctx context.Context, name string, _ json.RawMessage) (string, error) {
		executed = append(executed, name)
		cancel()
		return "", ctx.Err()
	})
	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "slow", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "later", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "run")},
		streamer, exec, StreamHooks{}, nil, nil,
	))
	results := assertCanceledToolTurnCommit(t, result, capture, 1)

	if !reflect.DeepEqual(executed, []string{"slow"}) {
		t.Errorf("executed = %v, want only slow", executed)
	}
	if len(results) != 2 || !results[0].IsError || !strings.Contains(results[0].Content, context.Canceled.Error()) {
		t.Fatalf("first result = %+v, want actual context cancellation error", results)
	}
	if results[1].Content != interruptedBeforeToolStart || !results[1].IsError {
		t.Errorf("second result = %+v, want synthetic interruption", results[1])
	}
	if !reflect.DeepEqual(result.InterruptedToolNames, []string{"slow", "later"}) {
		t.Errorf("InterruptedToolNames = %v, want [slow later]", result.InterruptedToolNames)
	}
	if len(result.ToolActivities) != 1 || result.ToolActivities[0].Name != "slow" || !result.ToolActivities[0].IsError {
		t.Errorf("ToolActivities = %+v, want failed slow only", result.ToolActivities)
	}
}

func TestRunAgent_ToolTurnTimeoutCommitsBalancedResults(t *testing.T) {
	exec := toolExecFunc(func(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "slow", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "later", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	cfg.Timeout = 250 * time.Millisecond
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "run")},
		streamer, exec, StreamHooks{}, nil, nil,
	))
	if result.StopReason != "timeout" {
		t.Errorf("StopReason = %q, want timeout", result.StopReason)
	}
	if result.TurnsPersisted != 2 || len(capture.persisted) != 2 {
		t.Fatalf("persist counts = result:%d callback:%d, want 2", result.TurnsPersisted, len(capture.persisted))
	}
	assertToolTurnBalanced(t, capture.persisted)
	assertToolTurnBalanced(t, result.FinalMessages)
	results := decodeToolTurnBlocks(t, capture.persisted[1])
	if len(results) != 2 || !results[0].IsError || !strings.Contains(results[0].Content, context.DeadlineExceeded.Error()) {
		t.Fatalf("first result = %+v, want actual deadline error", results)
	}
	if results[1].Content != interruptedBeforeToolStart || !results[1].IsError {
		t.Errorf("second result = %+v, want synthetic interruption", results[1])
	}
	if !reflect.DeepEqual(result.InterruptedToolNames, []string{"slow", "later"}) {
		t.Errorf("InterruptedToolNames = %v, want [slow later]", result.InterruptedToolNames)
	}
}

func TestRunAgent_CancelBeforeSequentialDispatchBalancesAllCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var executeCount atomic.Int32
	exec := toolExecFunc(func(context.Context, string, json.RawMessage) (string, error) {
		executeCount.Add(1)
		return "unexpected", nil
	})
	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "first", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "second", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	cfg.OnTurn = func(int, int) { cancel() }
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "run")},
		streamer, exec, StreamHooks{}, nil, nil,
	))
	results := assertCanceledToolTurnCommit(t, result, capture, 0)

	if executeCount.Load() != 0 {
		t.Errorf("executor calls = %d, want 0", executeCount.Load())
	}
	if len(result.ToolActivities) != 0 {
		t.Errorf("ToolActivities = %+v, want empty", result.ToolActivities)
	}
	if !reflect.DeepEqual(result.InterruptedToolNames, []string{"first", "second"}) {
		t.Errorf("InterruptedToolNames = %v, want [first second]", result.InterruptedToolNames)
	}
	for _, block := range results {
		if block.Type != "tool_result" || !block.IsError || block.Content != interruptedBeforeToolStart {
			t.Errorf("result = %+v, want synthetic interruption", block)
		}
	}
}

func TestRunAgent_SequentialCancelAfterPrepareDoesNotDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var executeCount atomic.Int32
	var starts, emits, results []string
	exec := toolExecFunc(func(context.Context, string, json.RawMessage) (string, error) {
		executeCount.Add(1)
		return "unexpected", nil
	})
	hooks := StreamHooks{
		OnToolStart: func(name, _ string, _ []byte) {
			starts = append(starts, name)
			cancel()
		},
		OnToolEmit: func(_ string, id string, _ []byte) {
			emits = append(emits, id)
		},
		OnToolResult: func(_ string, id, _ string, _ bool) {
			results = append(results, id)
		},
	}
	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "write", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "send", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "run")},
		streamer, exec, hooks, nil, nil,
	))
	toolResults := assertCanceledToolTurnCommit(t, result, capture, 1)

	if executeCount.Load() != 0 {
		t.Errorf("executor calls = %d, want 0", executeCount.Load())
	}
	if !reflect.DeepEqual(starts, []string{"write"}) || !reflect.DeepEqual(emits, []string{"call-1"}) {
		t.Errorf("start/emit hooks = %v/%v, want write/call-1", starts, emits)
	}
	if !reflect.DeepEqual(results, []string{"call-1"}) {
		t.Errorf("result hooks = %v, want cancellation result for call-1", results)
	}
	if len(toolResults) != 2 || !toolResults[0].IsError || !strings.Contains(toolResults[0].Content, context.Canceled.Error()) {
		t.Fatalf("first result = %+v, want prepared cancellation error", toolResults)
	}
	if toolResults[1].Content != interruptedBeforeToolStart || !toolResults[1].IsError {
		t.Errorf("second result = %+v, want synthetic interruption", toolResults[1])
	}
	if !reflect.DeepEqual(result.InterruptedToolNames, []string{"write", "send"}) {
		t.Errorf("InterruptedToolNames = %v, want [write send]", result.InterruptedToolNames)
	}
}

func TestRunAgent_CancelAfterOrdinaryToolErrorOnlyMarksContextFailureInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	exec := toolExecFunc(func(ctx context.Context, name string, _ json.RawMessage) (string, error) {
		if name == "validate" {
			return "", errors.New("invalid input")
		}
		cancel()
		return "", ctx.Err()
	})
	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "validate", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "slow", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "run")},
		streamer, exec, StreamHooks{}, nil, nil,
	))
	toolResults := assertCanceledToolTurnCommit(t, result, capture, 2)

	if len(toolResults) != 2 || !strings.Contains(toolResults[0].Content, "invalid input") ||
		!strings.Contains(toolResults[1].Content, context.Canceled.Error()) {
		t.Fatalf("tool results = %+v, want ordinary error then context error", toolResults)
	}
	if !reflect.DeepEqual(result.InterruptedToolNames, []string{"slow"}) {
		t.Errorf("InterruptedToolNames = %v, want only [slow]", result.InterruptedToolNames)
	}
}

func TestRunAgent_ToolTurnCommitsResultsWithoutCancellation(t *testing.T) {
	var executed []string
	exec := toolExecFunc(func(_ context.Context, name string, _ json.RawMessage) (string, error) {
		executed = append(executed, name)
		return "out-" + name, nil
	})
	streamer := &fakeLLMStreamer{turns: [][]llm.StreamEvent{
		buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: "call-1", name: "read", inputJSON: `{}`},
			{id: "call-2", name: "grep", inputJSON: `{}`},
		}, 100, 30),
		buildTextTurnEvents("done", 120, 20),
	}}
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		context.Background(), cfg, []llm.Message{llm.NewTextMessage("user", "inspect")},
		streamer, exec, StreamHooks{}, nil, nil,
	))

	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", result.StopReason)
	}
	if !reflect.DeepEqual(executed, []string{"read", "grep"}) {
		t.Errorf("executed = %v, want [read grep]", executed)
	}
	if result.TurnsPersisted != 3 || len(capture.persisted) != 3 {
		t.Errorf("persist counts = result:%d callback:%d, want 3", result.TurnsPersisted, len(capture.persisted))
	}
	if len(capture.activityTurns) != 1 || len(capture.activityTurns[0]) != 2 {
		t.Fatalf("OnToolTurn calls/activities = %d/%v, want 1/2", len(capture.activityTurns), capture.activityTurns)
	}
	if !reflect.DeepEqual(result.ToolActivities, capture.activityTurns[0]) {
		t.Errorf("ToolActivities = %+v, OnToolTurn = %+v", result.ToolActivities, capture.activityTurns[0])
	}
	if len(result.InterruptedToolNames) != 0 {
		t.Errorf("InterruptedToolNames = %v, want empty", result.InterruptedToolNames)
	}
	assertToolTurnBalanced(t, capture.persisted)
	assertToolTurnBalanced(t, result.FinalMessages)
	results := decodeToolTurnBlocks(t, capture.persisted[1])
	if len(results) != 2 || results[0].Content != "out-read" || results[1].Content != "out-grep" {
		t.Errorf("persisted results = %+v, want ordered actual outputs", results)
	}
}

func TestRunAgent_CancelDuringParallelTurnPreservesSuccessfulSibling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fastDone := make(chan struct{})
	slowStarted := make(chan struct{}, 2)
	exec := toolExecFunc(func(ctx context.Context, name string, _ json.RawMessage) (string, error) {
		if name == "fast" {
			close(fastDone)
			return "fast-result", nil
		}
		slowStarted <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	})
	go func() {
		<-fastDone
		<-slowStarted
		<-slowStarted
		cancel()
	}()

	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "fast", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "slow-a", inputJSON: `{}`},
		toolUseSpec{id: "call-3", name: "slow-b", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	cfg.ParallelSafeTool = vetAll
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "parallel")},
		streamer, exec, StreamHooks{}, nil, nil,
	))
	results := assertCanceledToolTurnCommit(t, result, capture, 3)

	if len(results) != 3 || results[0].Content != "fast-result" || results[0].IsError {
		t.Fatalf("successful parallel sibling was not preserved: %+v", results)
	}
	if !results[1].IsError || !results[2].IsError {
		t.Errorf("canceled parallel results must be errors: %+v", results)
	}
	if !reflect.DeepEqual(result.InterruptedToolNames, []string{"slow-a", "slow-b"}) {
		t.Errorf("InterruptedToolNames = %v, want [slow-a slow-b]", result.InterruptedToolNames)
	}
	if len(result.ToolActivities) != 3 || result.ToolActivities[0].Name != "fast" || result.ToolActivities[0].IsError {
		t.Errorf("ToolActivities = %+v, want successful fast then two errors", result.ToolActivities)
	}
}

func TestRunAgent_ParallelCancelAfterPrepareClosesResultHook(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var executeCount atomic.Int32
	var starts, emits, results []string
	exec := toolExecFunc(func(context.Context, string, json.RawMessage) (string, error) {
		executeCount.Add(1)
		return "unexpected", nil
	})
	hooks := StreamHooks{
		OnToolStart: func(name, _ string, _ []byte) {
			starts = append(starts, name)
			cancel()
		},
		OnToolEmit: func(_ string, id string, _ []byte) {
			emits = append(emits, id)
		},
		OnToolResult: func(_ string, id, _ string, _ bool) {
			results = append(results, id)
		},
	}
	streamer := toolTurnTestStreamer(
		toolUseSpec{id: "call-1", name: "first", inputJSON: `{}`},
		toolUseSpec{id: "call-2", name: "second", inputJSON: `{}`},
	)
	capture := &toolTurnCapture{}
	cfg := toolTurnTestConfig()
	cfg.ParallelSafeTool = vetAll
	capture.configure(&cfg)

	result := testutil.Must(RunAgent(
		ctx, cfg, []llm.Message{llm.NewTextMessage("user", "parallel")},
		streamer, exec, hooks, nil, nil,
	))
	assertCanceledToolTurnCommit(t, result, capture, 0)

	if executeCount.Load() != 0 {
		t.Errorf("executor calls = %d, want 0", executeCount.Load())
	}
	if !reflect.DeepEqual(starts, []string{"first"}) || !reflect.DeepEqual(emits, []string{"call-1"}) {
		t.Errorf("start/emit hooks = %v/%v, want first/call-1", starts, emits)
	}
	if !reflect.DeepEqual(results, []string{"call-1"}) {
		t.Errorf("result hooks = %v, want synthetic close for prepared call-1 only", results)
	}
}
