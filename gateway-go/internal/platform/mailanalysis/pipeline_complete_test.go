package mailanalysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

func discardMailLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStreamSynthesisWalksEndpointChain(t *testing.T) {
	dead := newQueuedOpenAIServer(t, "boom")
	dead.statuses = []int{500}
	defer dead.Close()
	live := newQueuedOpenAIServer(t, "폴백 분석")
	defer live.Close()

	got, err := streamSynthesis(context.Background(), []SynthesisEndpoint{
		{Client: dead.Client(), Model: "main"},
		{Client: live.Client(), Model: "fallback"},
	}, "sys", "user", 64, false, "main", discardMailLogger(), false, nil)
	if err != nil || got != "폴백 분석" {
		t.Fatalf("streamSynthesis = %q/%v", got, err)
	}
	dead.mu.Lock()
	defer dead.mu.Unlock()
	live.mu.Lock()
	defer live.mu.Unlock()
	if len(dead.requests) != 1 || len(live.requests) != 1 {
		t.Fatalf("requests dead=%d live=%d", len(dead.requests), len(live.requests))
	}
}

func TestStreamSynthesisSkipsPrimaryAfterDeadline(t *testing.T) {
	dead := newQueuedOpenAIServer(t, "should-not-run")
	defer dead.Close()
	live := newQueuedOpenAIServer(t, "폴백 분석")
	defer live.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	got, err := streamSynthesis(ctx, []SynthesisEndpoint{
		{Client: dead.Client(), Model: "main"},
		{Client: live.Client(), Model: "fallback"},
	}, "sys", "user", 64, false, "main", discardMailLogger(), false, nil)
	if err != nil || got != "폴백 분석" {
		t.Fatalf("deadline recovery = %q/%v", got, err)
	}
	dead.mu.Lock()
	defer dead.mu.Unlock()
	if len(dead.requests) != 0 {
		t.Fatalf("primary was called after deadline: %d", len(dead.requests))
	}
}

func TestStreamSynthesisCanceledDoesNotWalkChain(t *testing.T) {
	live := newQueuedOpenAIServer(t, "should-not-run")
	defer live.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := streamSynthesis(ctx, []SynthesisEndpoint{
		{Client: live.Client(), Model: "main"},
	}, "sys", "user", 64, false, "main", discardMailLogger(), false, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if len(live.requests) != 0 {
		t.Fatalf("canceled context still called the model: %d", len(live.requests))
	}
}

func TestStreamSynthesisSkipPrimaryRecordsFailureOnDeadFallbackCandidate(t *testing.T) {
	dead := newQueuedOpenAIServer(t, "boom")
	dead.statuses = []int{500}
	defer dead.Close()
	live := newQueuedOpenAIServer(t, "폴백 분석")
	defer live.Close()
	var failed []string
	got, err := streamSynthesis(context.Background(), []SynthesisEndpoint{
		{Client: dead.Client(), Model: "main"},
		{Client: live.Client(), Model: "fallback"},
	}, "sys", "user", 64, false, "main", discardMailLogger(), true, func(model string) {
		failed = append(failed, model)
	})
	if err != nil || got != "폴백 분석" {
		t.Fatalf("skip-primary = %q/%v", got, err)
	}
	dead.mu.Lock()
	defer dead.mu.Unlock()
	if len(dead.requests) != 0 {
		t.Fatalf("skipPrimary still called main: %d", len(dead.requests))
	}
	if len(failed) != 1 || failed[0] != "main" {
		t.Fatalf("skipPrimary should record the skipped primary, got %v", failed)
	}
}

func TestRunFinalSynthesisAgentTimeoutSkipsPrimary(t *testing.T) {
	dead := newQueuedOpenAIServer(t, "should-not-run")
	defer dead.Close()
	live := newQueuedOpenAIServer(t, "폴백 분석")
	defer live.Close()
	deps := PipelineDeps{
		MainModel: "main",
		Logger:    discardMailLogger(),
		AgentSynthesisFn: func(context.Context, string) (string, error) {
			return "응답 생성이 시간 초과로 중단됐어요.", fmt.Errorf("agent synthesis incomplete: timeout")
		},
		SynthesisEndpoints: []SynthesisEndpoint{
			{Client: dead.Client(), Model: "main"},
			{Client: live.Client(), Model: "fallback"},
		},
	}
	got, err := runFinalSynthesis(context.Background(), deps, "prompt", 64)
	if err != nil || got != "폴백 분석" {
		t.Fatalf("agent timeout fallback = %q/%v", got, err)
	}
	dead.mu.Lock()
	defer dead.mu.Unlock()
	if len(dead.requests) != 0 {
		t.Fatalf("timed-out primary was retried: %d", len(dead.requests))
	}
}

func TestHasStage2LLMAcceptsEndpointChainWithoutPinnedClient(t *testing.T) {
	if hasStage2LLM(PipelineDeps{}) {
		t.Fatal("empty deps looked like a stage-2 LLM")
	}
	if !hasStage2LLM(PipelineDeps{SynthesisEndpoints: []SynthesisEndpoint{{Model: "x"}}}) {
		t.Fatal("endpoint chain should satisfy hasStage2LLM")
	}
}

func TestSkipPrimaryAfterAgent(t *testing.T) {
	if skipPrimaryAfterAgent(context.Background(), errors.New("chat handler unavailable"), "") {
		t.Fatal("wiring errors must still try the pinned main model")
	}
	if !skipPrimaryAfterAgent(context.Background(), errors.New("agent synthesis incomplete: timeout"), "시간 초과") {
		t.Fatal("incomplete timeout should skip the primary")
	}
	if !skipPrimaryAfterAgent(context.Background(), nil, "  ") {
		t.Fatal("empty agent output should skip the primary")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if !skipPrimaryAfterAgent(ctx, context.DeadlineExceeded, "") {
		t.Fatal("deadline exceeded should skip the primary")
	}
}
