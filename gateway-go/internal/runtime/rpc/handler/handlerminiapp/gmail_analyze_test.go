package handlerminiapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeAnalyzePipeline struct {
	analyzeFn func(ctx context.Context, msg *gmail.MessageDetail) (string, error)
}

func (f *fakeAnalyzePipeline) Analyze(ctx context.Context, msg *gmail.MessageDetail) (string, error) {
	if f.analyzeFn == nil {
		return "", errors.New("Analyze not stubbed")
	}
	return f.analyzeFn(ctx, msg)
}

func analyzeDeps(client GmailClient, pipeline AnalyzePipeline) GmailAnalyzeDeps {
	return GmailAnalyzeDeps{
		Client:   func() (GmailClient, error) { return client, nil },
		Pipeline: func() (AnalyzePipeline, error) { return pipeline, nil },
	}
}

func TestGmailAnalyze_HappyPath(t *testing.T) {
	var seenID string
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, id string) (*gmail.MessageDetail, error) {
			seenID = id
			return &gmail.MessageDetail{
				ID:      id,
				From:    "Alice <alice@example.com>",
				Subject: "Meeting tomorrow",
				Date:    "Mon, 26 May 2026 14:30:00 +0900",
				Body:    "Hi, can we sync at 2pm?",
			}, nil
		},
	}
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, msg *gmail.MessageDetail) (string, error) {
			if msg.ID != "m1" {
				t.Errorf("pipeline got id=%q, want m1", msg.ID)
			}
			return "## 핵심 요약\n회의 일정 조율 요청.\n", nil
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, pipeline))

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	var got map[string]any
	decode(t, resp, &got)

	if seenID != "m1" {
		t.Errorf("client.GetMessage id = %q, want m1", seenID)
	}
	if got["id"] != "m1" || got["subject"] != "Meeting tomorrow" {
		t.Errorf("payload fields wrong: %+v", got)
	}
	if !strings.Contains(got["analysis"].(string), "핵심 요약") {
		t.Errorf("analysis missing expected content: %v", got["analysis"])
	}
	if _, ok := got["durationMs"].(float64); !ok {
		t.Errorf("durationMs missing/wrong type: %+v", got["durationMs"])
	}
}

func TestGmailAnalyze_MissingID(t *testing.T) {
	h := gmailAnalyze(analyzeDeps(&fakeGmailClient{}, &fakeAnalyzePipeline{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestGmailAnalyze_RequiresAuth(t *testing.T) {
	h := gmailAnalyze(analyzeDeps(&fakeGmailClient{}, &fakeAnalyzePipeline{}))
	resp := h(context.Background(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected unauthorized, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestGmailAnalyze_GmailGetNotFound(t *testing.T) {
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return nil, errors.New("HTTP 404: Not Found")
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, &fakeAnalyzePipeline{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "missing"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want NOT_FOUND", resp.Error.Code)
	}
}

func TestGmailAnalyze_PipelineFailure(t *testing.T) {
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1"}, nil
		},
	}
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, _ *gmail.MessageDetail) (string, error) {
			return "", errors.New("LLM call timed out")
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, pipeline))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

func TestGmailAnalyze_EmptyAnalysisIsRejected(t *testing.T) {
	gmailClient := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1"}, nil
		},
	}
	pipeline := &fakeAnalyzePipeline{
		analyzeFn: func(_ context.Context, _ *gmail.MessageDetail) (string, error) {
			return "   \n  ", nil // whitespace only
		},
	}
	h := gmailAnalyze(analyzeDeps(gmailClient, pipeline))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error for empty analysis")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

func TestGmailAnalyze_PipelineFactoryError(t *testing.T) {
	deps := GmailAnalyzeDeps{
		Client: func() (GmailClient, error) { return &fakeGmailClient{}, nil },
		Pipeline: func() (AnalyzePipeline, error) {
			return nil, ErrAnalyzeNoLLM
		},
	}
	h := gmailAnalyze(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.analyze", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

func TestGmailAnalyzeMethods_MissingDepsReturnsNil(t *testing.T) {
	if got := GmailAnalyzeMethods(GmailAnalyzeDeps{}); got != nil {
		t.Errorf("expected nil when no deps wired, got %v", got)
	}
	if got := GmailAnalyzeMethods(GmailAnalyzeDeps{
		Client: func() (GmailClient, error) { return nil, nil },
	}); got != nil {
		t.Errorf("expected nil with only client, got %v", got)
	}
}

func TestPipelineFromGmailpoll_NoLLM(t *testing.T) {
	_, err := PipelineFromGmailpoll(nil, nil, "")
	if !errors.Is(err, ErrAnalyzeNoLLM) {
		t.Errorf("err = %v, want ErrAnalyzeNoLLM", err)
	}
	_, err = PipelineFromGmailpoll(nil, nil, "claude-opus")
	if !errors.Is(err, ErrAnalyzeNoLLM) {
		t.Errorf("nil LLMClient should still return ErrAnalyzeNoLLM, got %v", err)
	}
}
