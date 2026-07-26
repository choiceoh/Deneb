package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

type scriptedStreamAttempt struct {
	events   []llm.StreamEvent
	err      error
	holdOpen bool
}

type scriptedRetryStreamer struct {
	mu                      sync.Mutex
	attempts                []scriptedStreamAttempt
	contexts                []context.Context
	requirePreviousCanceled bool
	calls                   int
}

func (s *scriptedRetryStreamer) StreamChat(ctx context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.requirePreviousCanceled && len(s.contexts) > 0 {
		select {
		case <-s.contexts[len(s.contexts)-1].Done():
		default:
			return nil, errors.New("previous stream attempt was not canceled before retry")
		}
	}
	s.contexts = append(s.contexts, ctx)
	s.calls++
	if len(s.attempts) == 0 {
		return nil, errors.New("unexpected StreamChat call")
	}
	attempt := s.attempts[0]
	s.attempts = s.attempts[1:]
	if attempt.err != nil {
		return nil, attempt.err
	}

	events := make(chan llm.StreamEvent, len(attempt.events))
	for _, event := range attempt.events {
		events <- event
	}
	if !attempt.holdOpen {
		close(events)
	}
	return events, nil
}

func TestRunStreamingTurnWithPolicyCancelsEachAttempt(t *testing.T) {
	parent := context.Background()
	streamer := &scriptedRetryStreamer{
		requirePreviousCanceled: true,
		attempts: []scriptedStreamAttempt{
			{events: partialTextEvents("partial"), holdOpen: true},
			{events: buildTextTurnEvents("recovered", 10, 5)},
		},
	}

	outcome, err := runStreamingTurnWithPolicy(
		parent,
		streamer,
		llm.ChatRequest{Stream: true},
		StreamHooks{},
		10*time.Millisecond,
		nil,
		0,
		false,
		0,
	)
	testutil.NoError(t, err)
	if outcome.result.text != "recovered" {
		t.Fatalf("result text = %q, want recovered", outcome.result.text)
	}
	if len(streamer.contexts) != 2 {
		t.Fatalf("attempt contexts = %d, want 2", len(streamer.contexts))
	}
	for i, attemptCtx := range streamer.contexts {
		if !errors.Is(attemptCtx.Err(), context.Canceled) {
			t.Errorf("attempt %d context error = %v, want canceled after attempt", i+1, attemptCtx.Err())
		}
	}
	if parent.Err() != nil {
		t.Fatalf("parent context was canceled: %v", parent.Err())
	}
}

func (*scriptedRetryStreamer) Complete(context.Context, llm.ChatRequest) (string, error) {
	return "", nil
}

func TestRunStreamingTurnWithPolicyRetryBehavior(t *testing.T) {
	connectionErr := errors.New("dial failed")
	errorEvent := llm.StreamEvent{
		Type:    "error",
		Payload: llm.FlexibleFromRaw([]byte(`{"message":"upstream fault"}`)),
	}

	tests := []struct {
		name            string
		ctx             func() context.Context
		attempts        []scriptedStreamAttempt
		idleTimeout     time.Duration
		wantAttempts    int
		wantRetries     int
		wantRetryReason streamRetryReason
		wantTermination streamTerminationReason
		wantErr         error
		wantText        string
		wantCalls       int
	}{
		{
			name: "initial connection failure is not retried",
			attempts: []scriptedStreamAttempt{
				{err: connectionErr},
				{events: buildTextTurnEvents("must not run", 1, 1)},
			},
			wantAttempts:    1,
			wantTermination: streamTerminationInitialConnectErr,
			wantErr:         connectionErr,
			wantCalls:       1,
		},
		{
			name: "pre-output idle stream fails fast for model fallback",
			attempts: []scriptedStreamAttempt{
				{holdOpen: true},
			},
			idleTimeout:     10 * time.Millisecond,
			wantAttempts:    1,
			wantTermination: streamTerminationPreOutputIdle,
			wantRetryReason: streamRetryIdle,
			wantErr:         ErrStreamIdle,
			wantCalls:       1,
		},
		{
			name: "idle stream after partial output retries once and succeeds",
			attempts: []scriptedStreamAttempt{
				{events: partialTextEvents("partial"), holdOpen: true},
				{events: buildTextTurnEvents("recovered", 10, 5)},
			},
			idleTimeout:     10 * time.Millisecond,
			wantAttempts:    2,
			wantRetries:     1,
			wantRetryReason: streamRetryIdle,
			wantTermination: streamTerminationCompleted,
			wantText:        "recovered",
			wantCalls:       2,
		},
		{
			name: "second event error exhausts retry",
			attempts: []scriptedStreamAttempt{
				{events: []llm.StreamEvent{errorEvent}},
				{events: []llm.StreamEvent{errorEvent}},
			},
			idleTimeout:     -1,
			wantAttempts:    2,
			wantRetries:     1,
			wantRetryReason: streamRetryEvent,
			wantTermination: streamTerminationRetryBudgetSpent,
			wantErr:         ErrStreamEvent,
			wantCalls:       2,
		},
		{
			name: "cancellation terminates without retry",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			attempts: []scriptedStreamAttempt{
				{holdOpen: true},
				{events: buildTextTurnEvents("must not run", 1, 1)},
			},
			wantAttempts:    1,
			wantTermination: streamTerminationContextDone,
			wantErr:         context.Canceled,
			wantCalls:       1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx()
			}
			streamer := &scriptedRetryStreamer{attempts: tt.attempts}
			outcome, err := runStreamingTurnWithPolicy(
				ctx,
				streamer,
				llm.ChatRequest{Stream: true},
				StreamHooks{},
				tt.idleTimeout,
				nil,
				0,
				false,
				0,
			)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if outcome.attempts != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", outcome.attempts, tt.wantAttempts)
			}
			if outcome.retries != tt.wantRetries {
				t.Errorf("retries = %d, want %d", outcome.retries, tt.wantRetries)
			}
			if outcome.retryReason != tt.wantRetryReason {
				t.Errorf("retryReason = %q, want %q", outcome.retryReason, tt.wantRetryReason)
			}
			if outcome.terminationReason != tt.wantTermination {
				t.Errorf("terminationReason = %q, want %q", outcome.terminationReason, tt.wantTermination)
			}
			if outcome.result.text != tt.wantText {
				t.Errorf("text = %q, want %q", outcome.result.text, tt.wantText)
			}
			if streamer.calls != tt.wantCalls {
				t.Errorf("StreamChat calls = %d, want %d", streamer.calls, tt.wantCalls)
			}
		})
	}
}

func partialTextEvents(text string) []llm.StreamEvent {
	return []llm.StreamEvent{
		messageStartEvent(10),
		contentBlockStartEvent(0, "text", ""),
		textDeltaEvent(0, text),
	}
}

func TestRunStreamingTurnWithPolicyPreservesHooksAndResetsResult(t *testing.T) {
	firstAttempt := []llm.StreamEvent{
		messageStartEvent(5),
		contentBlockStartEvent(0, "text", ""),
		textDeltaEvent(0, "partial"),
		{Type: "error", Payload: llm.FlexibleFromRaw([]byte(`{"message":"interrupted"}`))},
	}
	streamer := &scriptedRetryStreamer{attempts: []scriptedStreamAttempt{
		{events: firstAttempt},
		{events: buildTextTurnEvents("recovered", 10, 5)},
	}}
	var deltas []string

	outcome, err := runStreamingTurnWithPolicy(
		context.Background(),
		streamer,
		llm.ChatRequest{Stream: true},
		StreamHooks{OnTextDelta: func(delta string) { deltas = append(deltas, delta) }},
		-1,
		nil,
		0,
		false,
		0,
	)
	testutil.NoError(t, err)

	if got := strings.Join(deltas, ","); got != "partial,recovered" {
		t.Errorf("hook deltas = %q, want partial,recovered", got)
	}
	if outcome.result.text != "recovered" {
		t.Errorf("result text = %q, want only retry output", outcome.result.text)
	}
}

func TestRunAgentExposesStreamStats(t *testing.T) {
	t.Run("recovered retry", func(t *testing.T) {
		streamer := &scriptedRetryStreamer{attempts: []scriptedStreamAttempt{
			{events: []llm.StreamEvent{{
				Type:    "error",
				Payload: llm.FlexibleFromRaw([]byte(`{"message":"transient"}`)),
			}}},
			{events: buildTextTurnEvents("recovered", 10, 5)},
		}}

		result := testutil.Must(RunAgent(
			context.Background(),
			AgentConfig{MaxTurns: 1, Timeout: time.Second, MaxTokens: 128},
			[]llm.Message{llm.NewTextMessage("user", "hi")},
			streamer,
			nil,
			StreamHooks{},
			nil,
			nil,
		))

		want := StreamStats{
			Attempts:          2,
			Retries:           1,
			LastRetryReason:   "event_error",
			TerminationReason: "completed",
		}
		if result.Stream != want {
			t.Errorf("Stream = %+v, want %+v", result.Stream, want)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		streamer := &scriptedRetryStreamer{attempts: []scriptedStreamAttempt{
			{holdOpen: true},
			{events: buildTextTurnEvents("must not run", 1, 1)},
		}}

		result := testutil.Must(RunAgent(
			ctx,
			AgentConfig{MaxTurns: 1, Timeout: time.Second, MaxTokens: 128},
			[]llm.Message{llm.NewTextMessage("user", "hi")},
			streamer,
			nil,
			StreamHooks{},
			nil,
			nil,
		))

		if result.StopReason != "aborted" {
			t.Errorf("StopReason = %q, want aborted", result.StopReason)
		}
		want := StreamStats{Attempts: 1, TerminationReason: "context_done"}
		if result.Stream != want {
			t.Errorf("Stream = %+v, want %+v", result.Stream, want)
		}
		if streamer.calls != 1 {
			t.Errorf("StreamChat calls = %d, want 1", streamer.calls)
		}
	})
}

func TestRunAgentPreOutputIdleReturnsTimeoutWithoutSameModelRetry(t *testing.T) {
	streamer := &scriptedRetryStreamer{attempts: []scriptedStreamAttempt{
		{holdOpen: true},
		{events: buildTextTurnEvents("must not retry", 1, 1)},
	}}

	result := testutil.Must(RunAgent(
		context.Background(),
		AgentConfig{MaxTurns: 1, Timeout: time.Second, MaxTokens: 128, StreamIdleTimeout: 10 * time.Millisecond},
		[]llm.Message{llm.NewTextMessage("user", "hi")},
		streamer,
		nil,
		StreamHooks{},
		nil,
		nil,
	))

	if result.StopReason != "timeout" {
		t.Errorf("StopReason = %q, want timeout", result.StopReason)
	}
	want := StreamStats{Attempts: 1, LastRetryReason: "idle_timeout", TerminationReason: "pre_output_idle"}
	if result.Stream != want {
		t.Errorf("Stream = %+v, want %+v", result.Stream, want)
	}
	if streamer.calls != 1 {
		t.Errorf("StreamChat calls = %d, want 1", streamer.calls)
	}
}

func TestRunAgentInitialConnectionFailureKeepsErrorStage(t *testing.T) {
	streamer := &scriptedRetryStreamer{attempts: []scriptedStreamAttempt{
		{err: errors.New("dial failed")},
		{events: buildTextTurnEvents("must not run", 1, 1)},
	}}

	_, err := RunAgent(
		context.Background(),
		AgentConfig{MaxTurns: 1, Timeout: time.Second, MaxTokens: 128},
		[]llm.Message{llm.NewTextMessage("user", "hi")},
		streamer,
		nil,
		StreamHooks{},
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "stream chat (turn 0): dial failed") {
		t.Fatalf("error = %v, want initial stream chat stage", err)
	}
	if streamer.calls != 1 {
		t.Errorf("StreamChat calls = %d, want 1", streamer.calls)
	}
}
