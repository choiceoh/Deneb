package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	transcriptstore "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newSyncTestHandler(server *httptest.Server, transcript TranscriptStore) *Handler {
	sm := session.NewManager()
	broadcast := func(event string, payload json.RawMessage) (int, []error) { return 1, nil }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = llm.NewClient(server.URL, "test-key")
	cfg.Transcript = transcript
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."
	cfg.MaxTokens = 1024
	return NewHandler(sm, broadcast, logger, cfg)
}

func TestSendSyncReturnsErrorForUninitializedHandler(t *testing.T) {
	h := &Handler{}
	_, err := h.SendSync(context.Background(), "sess-1", "hello", "", nil)
	if err == nil || !errors.Is(err, ErrRuntimeDraining) {
		t.Fatalf("expected ErrRuntimeDraining for nil abort, got: %v", err)
	}
}

func TestPrepareSyncRunPreservesExplicitZeroToolCallLimit(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	h := newSyncTestHandler(server, transcriptstore.NewMemoryTranscriptStore())
	defer h.Close()

	limit := 0 // zero is an intentional hard cap, not an omitted option.
	params, _, err := h.prepareSyncRun("sync-limit", "hello", "test-model", "sync-test", &SyncOptions{
		MaxToolCallAttempts: &limit,
	})
	testutil.NoError(t, err)
	if params.MaxToolCallAttempts == nil || *params.MaxToolCallAttempts != 0 {
		t.Fatalf("MaxToolCallAttempts = %v, want explicit zero", params.MaxToolCallAttempts)
	}
}

func TestSendSync_UsesDefaultModelWhenRequestModelEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("sync reply", "end_turn"))
	}))
	defer server.Close()

	transcript := transcriptstore.NewMemoryTranscriptStore()
	h := newSyncTestHandler(server, transcript)
	defer h.Close()

	result := testutil.Must(h.SendSync(context.Background(), "sync-default-model", "  hello sync  ", "", nil))
	if result.Text != "sync reply" {
		t.Fatalf("Text = %q, want %q", result.Text, "sync reply")
	}
	if result.Model != "test-model" {
		t.Fatalf("Model = %q, want %q", result.Model, "test-model")
	}

	msgs, total, err := transcript.Load("sync-default-model", 0)
	testutil.NoError(t, err)
	if total < 1 {
		t.Fatalf("transcript total = %d, want >= 1", total)
	}
	// Transcript user messages carry a leading "[<RFC3339 ts>] " prefix
	// (see executeAgentRun); strip when comparing to raw input.
	if msgs[0].Role != "user" || StripUserMessageTimestamp(msgs[0].TextContent()) != "hello sync" {
		t.Fatalf("first user content = %q, want sanitized input", msgs[0].TextContent())
	}
}

func TestSendSyncRecordsActivityOnlyWhenNotEphemeral(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("sync reply", "end_turn"))
	}))
	defer server.Close()

	var recorded []string
	h := newSyncTestHandler(server, transcriptstore.NewMemoryTranscriptStore())
	h.recordActivity = func(sessionKey string) {
		recorded = append(recorded, sessionKey)
	}
	defer h.Close()

	_, err := h.SendSync(context.Background(), "client:main", "hello", "", nil)
	testutil.NoError(t, err)
	if !reflect.DeepEqual(recorded, []string{"client:main"}) {
		t.Fatalf("recorded = %#v, want client:main", recorded)
	}

	_, err = h.SendSync(context.Background(), "client:main", "heartbeat", "", &SyncOptions{EphemeralUser: true})
	testutil.NoError(t, err)
	if !reflect.DeepEqual(recorded, []string{"client:main"}) {
		t.Fatalf("ephemeral user should not record activity, got %#v", recorded)
	}
}

func TestSendSyncStream_StreamsDeltaAndPreservesExplicitModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("stream reply", "end_turn"))
	}))
	defer server.Close()

	h := newSyncTestHandler(server, transcriptstore.NewMemoryTranscriptStore())
	defer h.Close()

	var deltas []string
	result, err := h.SendSyncStream(
		context.Background(),
		"sync-stream",
		"hello",
		"explicit-model",
		nil,
		func(delta string) { deltas = append(deltas, delta) },
	)
	testutil.NoError(t, err)
	if result.Text != "stream reply" {
		t.Fatalf("Text = %q, want %q", result.Text, "stream reply")
	}
	if result.Model != "explicit-model" {
		t.Fatalf("Model = %q, want %q", result.Model, "explicit-model")
	}
	if !reflect.DeepEqual(deltas, []string{"stream reply"}) {
		t.Fatalf("deltas = %#v, want %#v", deltas, []string{"stream reply"})
	}
}

func TestSendSyncStream_DeadlineReturnsAndPersistsTimeoutFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	store := transcriptstore.NewMemoryTranscriptStore()
	h := newSyncTestHandler(server, store)
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := h.SendSyncStream(ctx, "sync-timeout", "long request", "", nil, nil)
	testutil.NoError(t, err)
	if result == nil || result.StopReason != "timeout" || result.BestText() != fallbackForStopReason("timeout") {
		t.Fatalf("timeout result = %#v", result)
	}

	messages, _, err := store.Load("sync-timeout", 0)
	testutil.NoError(t, err)
	if len(messages) < 2 {
		t.Fatalf("transcript messages = %d, want user + terminal assistant fallback", len(messages))
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" || last.TextContent() != fallbackForStopReason("timeout") {
		t.Fatalf("terminal row = role %q text %q", last.Role, last.TextContent())
	}
	if got := h.sessions.Get("sync-timeout").Status; got != session.StatusDone {
		t.Fatalf("session status = %q, want done after persisted fallback", got)
	}
}

func TestBuildSyncResult_FillsAbortedEmptyFallback(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	h := newSyncTestHandler(server, transcriptstore.NewMemoryTranscriptStore())
	defer h.Close()

	result, err := h.buildSyncResult("", &chatRunResult{
		AgentResult: &agent.AgentResult{StopReason: "aborted"},
	})
	testutil.NoError(t, err)

	want := fallbackForStopReason("aborted")
	if got := result.BestText(); got != want {
		t.Fatalf("BestText() = %q, want fallback %q", got, want)
	}
	if result.StopReason != "aborted" {
		t.Fatalf("StopReason = %q, want aborted", result.StopReason)
	}
}

func TestBuildSyncResult_LeavesNormalEmptyEndTurnBlank(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	h := newSyncTestHandler(server, transcriptstore.NewMemoryTranscriptStore())
	defer h.Close()

	result, err := h.buildSyncResult("", &chatRunResult{
		AgentResult: &agent.AgentResult{StopReason: "end_turn"},
	})
	testutil.NoError(t, err)

	if got := result.BestText(); got != "" {
		t.Fatalf("BestText() = %q, want blank", got)
	}
}

// The interactive surfaces (chat.send reply, stream done frame) both read
// BestText — a letter composed in chat must surface substituted market numbers,
// not raw tokens (the proactive relay only covers the cron path).
func TestBestTextReturnsSubstitutedMarketTokens(t *testing.T) {
	market.RecordLetterTokens(map[string]string{market.LetterTokenUSDKRW: "1,531"})
	r := SyncResult{DeliverableText: `<stat value="{{market:usd_krw}}" label="USD/KRW"/>`}
	if got := r.BestTextRaw(); got != `<stat value="{{market:usd_krw}}" label="USD/KRW"/>` {
		t.Fatalf("BestTextRaw() = %q, want original model text", got)
	}
	if got := r.BestText(); got != `<stat value="1,531" label="USD/KRW"/>` {
		t.Fatalf("BestText() = %q, want substituted stat", got)
	}
}

func TestBestTextReturnsDeliverableOverFallbacks(t *testing.T) {
	cases := []struct {
		name string
		r    SyncResult
		want string
	}{
		{
			name: "deliverable preferred over short final-turn and raw accumulation",
			r: SyncResult{
				Text:            "위키 업데이트 완료.",
				DeliverableText: "## 메일 종합 분석\n본문",
				AllText:         "이제 위키 검색부터 할게요.\n\n## 메일 종합 분석\n본문",
			},
			want: "## 메일 종합 분석\n본문",
		},
		{
			name: "falls back to final turn when deliverable empty",
			r:    SyncResult{Text: "마지막 답변", DeliverableText: "", AllText: "누적"},
			want: "마지막 답변",
		},
		{
			name: "falls back to AllText when deliverable and final turn empty",
			r:    SyncResult{Text: "", DeliverableText: "", AllText: "누적 텍스트"},
			want: "누적 텍스트",
		},
		{
			name: "strips trailing NO_REPLY from the chosen deliverable",
			r:    SyncResult{DeliverableText: "답변 본문 " + SilentReplyToken},
			want: "답변 본문",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.BestText(); got != c.want {
				t.Fatalf("BestText() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAdmitSyncWhenSessionIdle_WaitsForActiveRun(t *testing.T) {
	h := newSteerTestHandler()
	h.mergeWindow = NewMergeWindowTracker()
	markActiveRun(h, "client:main")

	released := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		h.abort.Cleanup("active-client:main")
		close(released)
	}()

	start := time.Now()
	if err := h.admitSyncWhenSessionIdle(context.Background(), "client:main"); err != nil {
		t.Fatalf("admitSyncWhenSessionIdle: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
		t.Fatalf("returned after %v, expected to wait for active run to finish", elapsed)
	}
	<-released
}

func TestAdmitSyncWhenSessionIdle_RespectsContextCancel(t *testing.T) {
	h := newSteerTestHandler()
	h.mergeWindow = NewMergeWindowTracker()
	markActiveRun(h, "client:main")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := h.admitSyncWhenSessionIdle(ctx, "client:main")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("waited %v after cancel, expected fast return", elapsed)
	}
}

func TestRegisterAdmittedSyncWhenSessionIdleSerializesConcurrentIdleCallers(t *testing.T) {
	h := newSteerTestHandler()
	h.mergeWindow = NewMergeWindowTracker()
	for range 2 {
		if !h.abort.AcquireAdmission() {
			t.Fatal("failed to reserve sync admission")
		}
		defer h.abort.ReleaseAdmission()
	}

	type outcome struct {
		id  string
		ok  bool
		err error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for _, id := range []string{"sync-a", "sync-b"} {
		go func() {
			<-start
			ok, err := h.registerAdmittedSyncWhenSessionIdle(context.Background(), id, &AbortEntry{
				SessionKey: "client:main", ClientRun: id, CancelFn: func(error) {}, ExpiresAt: time.Now().Add(time.Hour),
			})
			results <- outcome{id: id, ok: ok, err: err}
		}()
	}
	close(start)

	first := <-results
	if first.err != nil || !first.ok {
		t.Fatalf("first registration = %+v", first)
	}
	select {
	case second := <-results:
		t.Fatalf("second same-session sync registered concurrently: first=%+v second=%+v", first, second)
	case <-time.After(75 * time.Millisecond):
	}

	sessLock := h.mergeWindow.SessionLock("client:main")
	sessLock.Lock()
	h.abort.Cleanup(first.id)
	sessLock.Unlock()
	second := <-results
	if second.err != nil || !second.ok || second.id == first.id {
		t.Fatalf("second registration after cleanup = %+v, first=%+v", second, first)
	}
	h.abort.Cleanup(second.id)
}

func TestSyncAdmissionTicketsPreserveIngressOrderBeforeRegistration(t *testing.T) {
	h := newSteerTestHandler()
	h.mergeWindow = NewMergeWindowTracker()
	for range 2 {
		if !h.abort.AcquireAdmission() {
			t.Fatal("failed to reserve sync admission")
		}
		defer h.abort.ReleaseAdmission()
	}

	firstTicket := h.reserveSyncAdmission("client:main")
	secondTicket := h.reserveSyncAdmission("client:main")
	t.Cleanup(func() {
		// Tests normally release both below. Keep cleanup safe for an early fatal.
		select {
		case <-firstTicket.complete:
		default:
			firstTicket.release()
		}
		select {
		case <-secondTicket.complete:
		default:
			secondTicket.release()
		}
	})

	type outcome struct {
		id  string
		ok  bool
		err error
	}
	results := make(chan outcome, 2)
	register := func(id string, ticket *syncAdmissionTicket) {
		if err := ticket.wait(context.Background()); err != nil {
			results <- outcome{id: id, err: err}
			return
		}
		ok, err := h.registerAdmittedSyncWhenSessionIdle(context.Background(), id, &AbortEntry{
			SessionKey: "client:main", ClientRun: id, CancelFn: func(error) {}, ExpiresAt: time.Now().Add(time.Hour),
		})
		results <- outcome{id: id, ok: ok, err: err}
	}

	// Let the later request reach its ticket first. It must stay behind the
	// earlier ticket instead of winning the final RegisterAdmitted race.
	go register("sync-b", secondTicket)
	select {
	case got := <-results:
		t.Fatalf("later ingress bypassed its predecessor: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	go register("sync-a", firstTicket)
	first := <-results
	if first.id != "sync-a" || first.err != nil || !first.ok {
		t.Fatalf("first registration = %+v, want sync-a", first)
	}

	sessLock := h.mergeWindow.SessionLock("client:main")
	sessLock.Lock()
	h.abort.Cleanup(first.id)
	sessLock.Unlock()
	firstTicket.release()
	second := <-results
	if second.id != "sync-b" || second.err != nil || !second.ok {
		t.Fatalf("second registration = %+v, want sync-b", second)
	}
	h.abort.Cleanup(second.id)
	secondTicket.release()
}

func TestSyncAdmissionCanceledMiddleTicketKeepsPredecessorBarrier(t *testing.T) {
	h := newSteerTestHandler()
	first := h.reserveSyncAdmission("client:main")
	middle := h.reserveSyncAdmission("client:main")
	last := h.reserveSyncAdmission("client:main")
	t.Cleanup(func() {
		first.release()
		middle.release()
		last.release()
	})

	middle.release() // models ticket.wait returning on cancellation
	lastReady := make(chan struct{})
	go func() {
		_ = last.wait(context.Background())
		close(lastReady)
	}()
	select {
	case <-lastReady:
		t.Fatal("canceled middle ticket opened a shortcut around its predecessor")
	case <-time.After(50 * time.Millisecond):
	}

	first.release()
	select {
	case <-lastReady:
	case <-time.After(time.Second):
		t.Fatal("last ticket did not advance after predecessor release")
	}
}

func TestSyncAdmissionTicketKeepsLaterSteerBehindEarlierOwnTurn(t *testing.T) {
	h := newSteerTestHandler()
	markActiveRun(h, "client:main")
	first := h.reserveSyncAdmission("client:main")
	second := h.reserveSyncAdmission("client:main")
	t.Cleanup(func() {
		first.release()
		second.release()
	})
	if err := first.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res, handled := h.trySteerIntoActiveRun("client:main", "아니, 짧게 답해줘", &SyncOptions{TrustedDirectUserInput: true}); handled || res != nil {
		t.Fatalf("earlier memory correction steered: handled=%v res=%+v", handled, res)
	}

	type outcome struct {
		res     *SyncResult
		handled bool
	}
	later := make(chan outcome, 1)
	go func() {
		if err := second.wait(context.Background()); err != nil {
			later <- outcome{}
			return
		}
		res, handled := h.trySteerIntoActiveRun("client:main", "그 부분만 다시 봐줘", &SyncOptions{TrustedDirectUserInput: true})
		later <- outcome{res: res, handled: handled}
	}()
	select {
	case got := <-later:
		t.Fatalf("later steer bypassed earlier own turn: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	// Model the active turn finishing and the earlier correction taking its own
	// ordered turn. Only after that predecessor releases may the later request
	// make its steer/own-turn decision; with no active run it becomes its own turn.
	h.abort.Cleanup("active-client:main")
	first.release()
	select {
	case got := <-later:
		if got.handled || got.res != nil {
			t.Fatalf("later request steered ahead of its own turn: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("later request did not advance after predecessor release")
	}
	if notes := h.steer.Drain("client:main"); len(notes) != 0 {
		t.Fatalf("later request leaked into steer queue: %v", notes)
	}
	second.release()
}

func TestSendSyncReleasesAdmissionTicketAfterRegistrationSoFollowUpCanSteer(t *testing.T) {
	firstRequestStarted := make(chan struct{})
	releaseFirstRequest := make(chan struct{})
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestCount.Add(1) == 1 {
			close(firstRequestStarted)
			<-releaseFirstRequest
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("sync reply", "end_turn"))
	}))
	defer server.Close()

	h := newSyncTestHandler(server, transcriptstore.NewMemoryTranscriptStore())
	defer h.Close()
	opts := &SyncOptions{TrustedDirectUserInput: true}
	firstDone := make(chan error, 1)
	go func() {
		_, err := h.SendSync(context.Background(), "client:main", "첫 요청을 처리해줘", "", opts)
		firstDone <- err
	}()
	select {
	case <-firstRequestStarted:
	case <-time.After(time.Second):
		t.Fatal("first sync request did not start")
	}

	secondDone := make(chan struct {
		res *SyncResult
		err error
	}, 1)
	go func() {
		res, err := h.SendSync(context.Background(), "client:main", "그 부분만 다시 봐줘", "", opts)
		secondDone <- struct {
			res *SyncResult
			err error
		}{res: res, err: err}
	}()
	select {
	case got := <-secondDone:
		if got.err != nil || got.res == nil || got.res.StopReason != "steered" {
			t.Fatalf("follow-up = res %+v err %v, want immediate steer", got.res, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("follow-up waited behind the whole active sync run")
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("LLM requests = %d, want only the active run", got)
	}
	close(releaseFirstRequest)
	if err := <-firstDone; err != nil {
		t.Fatalf("first SendSync: %v", err)
	}
}

// TestWithSyncRunLifecycle_RegistersDuringAndCleansUpAfter locks the invariant
// that makes native-chat auto-steer, /kill, and merge work: a synchronous run
// is registered in the abort tracker WHILE it executes (so HasActiveRun sees
// it) and cleaned up SYNCHRONOUSLY before the call returns (so a sequential
// next SendSync on the same session does not fold into a ghost run). Before
// this wiring, native (sync-only) runs were never registered and all three
// features were dead on the native surface.
func TestWithSyncRunLifecycle_RegistersDuringAndCleansUpAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	h := newSyncTestHandler(server, transcriptstore.NewMemoryTranscriptStore())
	const sessionKey = "client:main"

	if h.abort.HasActiveRun(sessionKey) {
		t.Fatalf("precondition: session already has an active run")
	}

	var activeDuringRun bool
	var sessionRunningDuringRun bool
	var gotCtx context.Context
	_, err := h.withSyncRunLifecycle(context.Background(), sessionKey, "run-1", false,
		func(ctx context.Context) (*SyncResult, error) {
			activeDuringRun = h.abort.HasActiveRun(sessionKey)
			sessionRunningDuringRun = h.sessions.Get(sessionKey).Status == session.StatusRunning
			gotCtx = ctx
			return &SyncResult{Text: "ok"}, nil
		})
	if err != nil {
		t.Fatalf("withSyncRunLifecycle: %v", err)
	}
	if !activeDuringRun {
		t.Error("HasActiveRun was false during the run — auto-steer/kill/merge cannot see a native turn")
	}
	if !sessionRunningDuringRun {
		t.Error("session lifecycle was not running during the sync turn — transcript recovery cannot distinguish an in-flight answer")
	}
	if gotCtx == nil {
		t.Error("run received a nil context — it must be the cancellable run ctx")
	}
	if h.abort.HasActiveRun(sessionKey) {
		t.Error("HasActiveRun still true after return — cleanup must be synchronous or the next SendSync folds into a ghost run")
	}
	if got := h.sessions.Get(sessionKey).Status; got != session.StatusDone {
		t.Errorf("session status after sync return = %q, want %q", got, session.StatusDone)
	}
}

func TestPersistSynthesizedSyncFallbackAppendsTerminalAssistantRow(t *testing.T) {
	store := transcriptstore.NewMemoryTranscriptStore()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	h := newSyncTestHandler(server, store)
	defer h.Close()

	res, err := h.buildSyncTimeoutFallback("test-model")
	testutil.NoError(t, err)
	if !res.synthesizedFallback {
		t.Fatal("timeout result was not marked as synthesized")
	}
	persistSynthesizedSyncFallback(
		RunParams{SessionKey: "client:timeout"},
		h.buildRunDeps(),
		res,
		h.logger,
	)

	messages, total, err := store.Load("client:timeout", 0)
	testutil.NoError(t, err)
	if total != 1 || len(messages) != 1 {
		t.Fatalf("persisted messages = %d/%d, want one terminal assistant row", len(messages), total)
	}
	if messages[0].Role != "assistant" || messages[0].TextContent() != fallbackForStopReason("timeout") {
		t.Fatalf("terminal row = role %q text %q", messages[0].Role, messages[0].TextContent())
	}
}

func TestPersistTimeoutRemnantKeepsToolWorkInTranscript(t *testing.T) {
	store := transcriptstore.NewMemoryTranscriptStore()
	activities := []agent.ToolActivity{{Name: "watch"}, {Name: "web"}}
	msg := enrichStopFallback(fallbackForStopReason("timeout"), activities)
	persistTimeoutRemnant(
		runDeps{transcript: store},
		"client:watch-timeout",
		&agent.AgentResult{StopReason: "timeout", ToolActivities: activities},
		msg,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	messages, total, err := store.Load("client:watch-timeout", 0)
	testutil.NoError(t, err)
	if total != 1 || len(messages) != 1 {
		t.Fatalf("persisted = %d/%d", len(messages), total)
	}
	text := messages[0].TextContent()
	if messages[0].Role != "user" || !strings.Contains(text, "[SYSTEM:") || !strings.Contains(text, "Tools used: watch, web") {
		t.Fatalf("remnant = role %q text %q", messages[0].Role, text)
	}
}
