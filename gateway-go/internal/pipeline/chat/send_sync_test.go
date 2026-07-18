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
	"testing"

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
	var gotCtx context.Context
	_, err := h.withSyncRunLifecycle(context.Background(), sessionKey, "run-1", false,
		func(ctx context.Context) (*SyncResult, error) {
			activeDuringRun = h.abort.HasActiveRun(sessionKey)
			gotCtx = ctx
			return &SyncResult{Text: "ok"}, nil
		})
	if err != nil {
		t.Fatalf("withSyncRunLifecycle: %v", err)
	}
	if !activeDuringRun {
		t.Error("HasActiveRun was false during the run — auto-steer/kill/merge cannot see a native turn")
	}
	if gotCtx == nil {
		t.Error("run received a nil context — it must be the cancellable run ctx")
	}
	if h.abort.HasActiveRun(sessionKey) {
		t.Error("HasActiveRun still true after return — cleanup must be synchronous or the next SendSync folds into a ghost run")
	}
}
