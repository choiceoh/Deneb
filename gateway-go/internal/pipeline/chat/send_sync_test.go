package chat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newSyncTestHandler(server *httptest.Server, transcript TranscriptStore) *Handler {
	sm := session.NewManager()
	broadcast := func(event string, payload any) (int, []error) { return 1, nil }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = llm.NewClient(server.URL, "test-key")
	cfg.Transcript = transcript
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."
	cfg.MaxTokens = 1024
	return NewHandler(sm, broadcast, logger, cfg)
}

func TestSendSync_UninitializedHandler(t *testing.T) {
	h := &Handler{}
	_, err := h.SendSync(context.Background(), "sess-1", "hello", "", nil)
	if err == nil || err.Error() != "chat handler not initialized" {
		t.Fatalf("expected initialization error, got: %v", err)
	}
}

func TestSendSync_UsesDefaultModelWhenRequestModelEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("sync reply", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
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

func TestSendSyncStream_StreamsDeltaAndPreservesExplicitModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("stream reply", "end_turn"))
	}))
	defer server.Close()

	h := newSyncTestHandler(server, NewMemoryTranscriptStore())
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

func TestBestText(t *testing.T) {
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
