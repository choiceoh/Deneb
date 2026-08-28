package chat

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/runstate"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

func TestChatPortRejectsTypedNilHandler(t *testing.T) {
	var handler *Handler
	var runner chatport.SyncRunner = handler
	var streamRunner chatport.SyncStreamRunner = handler
	if runner.ChatReady() {
		t.Fatal("typed nil handler reported ready")
	}
	if streamRunner.ChatReady() {
		t.Fatal("typed nil stream handler reported ready")
	}
	if (&Handler{}).ChatReady() {
		t.Fatal("partially initialized handler reported ready")
	}
	if _, err := runner.RunSync(context.Background(), chatport.SyncRequest{}); err == nil {
		t.Fatal("typed nil RunSync returned nil error")
	}
	if _, err := streamRunner.RunSyncStream(context.Background(), chatport.SyncRequest{}, nil); err == nil {
		t.Fatal("typed nil RunSyncStream returned nil error")
	}
}

func TestSyncOptionsFromPortPreservesDirectUserProvenance(t *testing.T) {
	options := syncOptionsFromPort(chatport.SyncRequest{
		GateUntrustedTools:     true,
		TrustedDirectUserInput: true,
	})
	if !options.GateUntrustedTools || !options.TrustedDirectUserInput {
		t.Fatalf("sync options = gate:%v trusted:%v, want both true", options.GateUntrustedTools, options.TrustedDirectUserInput)
	}
}

func TestBeginDrainMarksHandlerUnreadyAndRejectsNewSyncRuns(t *testing.T) {
	h := &Handler{abort: NewAbortTracker()}
	t.Cleanup(h.abort.Close)

	if !h.ChatReady() {
		t.Fatal("new handler reported unready before drain")
	}
	if err := h.BeginDrain(context.Background()); err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if h.ChatReady() {
		t.Fatal("draining handler still reported ready")
	}
	if _, err := h.SendSync(context.Background(), "client:main", "hello", "", nil); !errors.Is(err, ErrRuntimeDraining) {
		t.Fatalf("SendSync error = %v, want ErrRuntimeDraining", err)
	}
	if _, err := h.SendSyncStream(context.Background(), "client:main", "hello", "", nil, nil); !errors.Is(err, ErrRuntimeDraining) {
		t.Fatalf("SendSyncStream error = %v, want ErrRuntimeDraining", err)
	}
}

func TestFatalDrainCancelsActiveRunAndCompletesWithoutCleanup(t *testing.T) {
	h := &Handler{
		abort:       NewAbortTracker(),
		pending:     NewPendingQueue(),
		mergeWindow: NewMergeWindowTracker(),
	}
	t.Cleanup(h.abort.Close)

	runCtx, cancel := context.WithCancelCause(context.Background())
	entry := &runstate.AbortEntry{
		SessionKey: "client:main",
		ClientRun:  "run-fatal",
		CancelFn:   cancel,
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	if !h.abort.TryRegister(entry.ClientRun, entry) {
		t.Fatal("active run was not registered")
	}
	h.pending.Enqueue("client:main", RunParams{SessionKey: "client:main", Message: "queued"})

	fatalErr := errors.New("ambiguous fact journal")
	h.FatalDrain(fatalErr)

	if h.ChatReady() {
		t.Fatal("fatal-draining handler still reported ready")
	}
	if !errors.Is(context.Cause(runCtx), fatalErr) {
		t.Fatalf("active run cause=%v, want %v", context.Cause(runCtx), fatalErr)
	}
	if h.pending.Len("client:main") != 0 {
		t.Fatal("fatal drain retained a queued continuation")
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer drainCancel()
	if err := h.BeginDrain(drainCtx); err != nil {
		t.Fatalf("fatal drain waited for normal run cleanup: %v", err)
	}
	if h.abort.RegisterContinuation("late", entry) {
		t.Fatal("fatal drain admitted a late continuation")
	}
}

func TestStartOrQueueRunEnqueuesDuringDrainWhenParentStillActive(t *testing.T) {
	h := &Handler{
		abort:       NewAbortTracker(),
		pending:     NewPendingQueue(),
		mergeWindow: NewMergeWindowTracker(),
	}
	t.Cleanup(h.abort.Close)

	active := &runstate.AbortEntry{
		SessionKey: "client:main",
		ClientRun:  "run-parent",
		CancelFn:   func(error) {},
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	if !h.abort.TryRegister(active.ClientRun, active) {
		t.Fatal("parent run was not registered")
	}
	drained := h.abort.BeginDrain()
	t.Cleanup(func() {
		h.abort.Cleanup(active.ClientRun)
		select {
		case <-drained:
		default:
		}
	})

	h.startOrQueueRun("subnotify-child", RunParams{
		SessionKey:  "client:main",
		Message:     "child completed",
		ClientRunID: "subnotify-child",
	}, false)

	if h.pending.Len("client:main") != 1 {
		t.Fatalf("pending len = %d, want 1 (notification must enqueue during drain)", h.pending.Len("client:main"))
	}
	if h.abort.AcquireAdmission() {
		t.Fatal("new top-level admission opened during drain")
	}
}

func TestBeginDrainWaitsForAdmittedSyncRunToActuallyReturn(t *testing.T) {
	h := &Handler{
		abort:       NewAbortTracker(),
		pending:     NewPendingQueue(),
		mergeWindow: NewMergeWindowTracker(),
	}
	t.Cleanup(h.abort.Close)
	if !h.abort.AcquireAdmission() {
		t.Fatal("pre-drain sync request was not admitted")
	}

	started := make(chan struct{})
	release := make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		defer h.abort.ReleaseAdmission()
		_, err := h.withAdmittedSyncRunLifecycle(context.Background(), "client:main", "run-sync", false,
			func(context.Context) (*SyncResult, error) {
				close(started)
				<-release
				return &SyncResult{Text: "complete"}, nil
			})
		runDone <- err
	}()
	<-started

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	drainDone := make(chan error, 1)
	go func() { drainDone <- h.BeginDrain(drainCtx) }()
	select {
	case err := <-drainDone:
		t.Fatalf("drain returned before the admitted run: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("sync run failed: %v", err)
	}
	if err := <-drainDone; err != nil {
		t.Fatalf("drain failed after sync run returned: %v", err)
	}
}

func TestSyncOptionsFromPortPreservesRuntimeAndStreamContract(t *testing.T) {
	maxTokens, maxTurns, maxCalls := 10, 2, 3
	delivery := &chatport.DeliveryContext{Channel: "client", To: "client:main"}
	var gotEvent chatport.ToolStreamEvent
	var gotPhase string
	req := chatport.SyncRequest{
		MaxTokens:            &maxTokens,
		MaxTurns:             &maxTurns,
		MaxToolCallAttempts:  &maxCalls,
		SystemPrompt:         "system",
		Thinking:             "off",
		ToolPreset:           "boot",
		InitialDeferredTools: []string{"skill_lifecycle"},
		MaxHistoryTokens:     42,
		Delivery:             delivery,
		EphemeralUser:        true,
		EphemeralAssistant:   true,
		AutoDeliveredOutput:  true,
		SkipRecall:           true,
		FeedContext:          "feed",
		GateUntrustedTools:   true,
		BeforeToolCall:       func(string, string, []byte) (bool, string) { return true, "blocked" },
		OnToolResult:         func(string, string, string, bool) {},
		OnToolEvent:          func(event chatport.ToolStreamEvent) { gotEvent = event },
		OnProgress:           func(phase string) { gotPhase = phase },
		OnThinking:           func(string) {},
		SoftDeadline:         20 * time.Minute,
	}

	got := syncOptionsFromPort(req)
	if got.MaxTokens != req.MaxTokens || got.MaxTurns != req.MaxTurns ||
		got.MaxToolCallAttempts != req.MaxToolCallAttempts || got.SystemPrompt != req.SystemPrompt ||
		got.Thinking != req.Thinking || got.ToolPreset != req.ToolPreset ||
		!slices.Equal(got.InitialDeferredTools, req.InitialDeferredTools) ||
		got.MaxHistoryTokens != req.MaxHistoryTokens || got.Delivery != delivery ||
		!got.EphemeralUser || !got.EphemeralAssistant || !got.AutoDeliveredOutput ||
		!got.SkipRecall || got.FeedContext != req.FeedContext || !got.GateUntrustedTools ||
		got.BeforeToolCall == nil || got.OnToolResult == nil || got.OnThinking == nil || got.OnToolEvent == nil ||
		got.OnProgress == nil || got.SoftDeadline != req.SoftDeadline {
		t.Fatalf("port request mapping lost fields: %#v", got)
	}
	got.OnToolEvent(ToolStreamEvent{
		State: "completed", Tool: "wiki", ToolUseID: "tool-1", Detail: "done", IsError: true,
		ResultSummary: "3개 문서 · 2줄",
	})
	if gotEvent != (chatport.ToolStreamEvent{
		State: "completed", Tool: "wiki", ToolUseID: "tool-1", Detail: "done", IsError: true,
		ResultSummary: "3개 문서 · 2줄",
	}) {
		t.Fatalf("stream event = %#v", gotEvent)
	}
	got.OnProgress("preparing")
	if gotPhase != "preparing" {
		t.Fatalf("progress phase = %q, want preparing", gotPhase)
	}
}

func TestSyncResultToPortPreservesWireResultAndSelectedText(t *testing.T) {
	result := syncResultToPort(&SyncResult{
		Text:            "wrap-up",
		AllText:         "working\nanswer",
		DeliverableText: "answer NO_REPLY",
		Model:           "vllm/model",
		ProviderModel:   "served-model",
		FellBack:        true,
		InputTokens:     11,
		OutputTokens:    7,
		Turns:           3,
		StopReason:      "end_turn",
	})
	if result == nil || result.BestText != "answer" || result.Text != "wrap-up" ||
		result.AllText != "working\nanswer" || result.DeliverableText != "answer NO_REPLY" ||
		result.Model != "vllm/model" || result.ProviderModel != "served-model" ||
		!result.FellBack || result.InputTokens != 11 || result.OutputTokens != 7 ||
		result.Turns != 3 || result.StopReason != "end_turn" {
		t.Fatalf("port result = %#v", result)
	}
}
