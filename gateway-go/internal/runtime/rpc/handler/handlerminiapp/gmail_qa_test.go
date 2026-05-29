package handlerminiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestGmailAsk_HappyPath(t *testing.T) {
	cache := NewAnalysisStore(t.TempDir())
	if err := cache.save(&analysisRecord{
		MsgID:           "m1",
		Analysis:        "핵심: 결제 지연 위험",
		RelatedProjects: []string{"프로젝트/topsolar.md"},
		PromptVersion:   AnalysisPromptVersion,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	var gotCtx, gotQ string
	var gotHist []QATurn
	deps := GmailAnalyzeDeps{
		Client: func() (GmailClient, error) {
			return &fakeGmailClient{getMessageFn: func(_ context.Context, id string) (*gmail.MessageDetail, error) {
				return &gmail.MessageDetail{ID: id, Subject: "결제 안내", From: "a@b.com", Body: "송장 첨부합니다"}, nil
			}}, nil
		},
		Pipeline: func() (AnalyzePipeline, error) { return &fakeAnalyzePipeline{}, nil },
		Cache:    cache,
		Ask: func(_ context.Context, mailContext string, history []QATurn, question string) (string, error) {
			gotCtx, gotHist, gotQ = mailContext, history, question
			return "결제 기한은 다음 주입니다.", nil
		},
	}
	h := GmailAnalyzeMethods(deps)["miniapp.gmail.ask"]
	if h == nil {
		t.Fatal("ask handler not registered when Ask is wired")
	}

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.ask", map[string]any{
		"id":       "m1",
		"question": "기한 언제야?",
		"history":  []map[string]any{{"q": "금액?", "a": "백만원"}},
	}))
	if !resp.OK {
		t.Fatalf("expected OK, got %+v", resp.Error)
	}
	var got map[string]any
	decode(t, resp, &got)
	if got["answer"] != "결제 기한은 다음 주입니다." {
		t.Errorf("answer = %v", got["answer"])
	}
	if gotQ != "기한 언제야?" {
		t.Errorf("forwarded question = %q", gotQ)
	}
	if len(gotHist) != 1 || gotHist[0].Q != "금액?" || gotHist[0].A != "백만원" {
		t.Errorf("forwarded history = %+v", gotHist)
	}
	// Grounding context must carry body + analysis + related project.
	for _, want := range []string{"송장 첨부합니다", "결제 지연 위험", "프로젝트/topsolar.md"} {
		if !strings.Contains(gotCtx, want) {
			t.Errorf("mailContext missing %q:\n%s", want, gotCtx)
		}
	}
}

func TestGmailAsk_MissingQuestion(t *testing.T) {
	deps := GmailAnalyzeDeps{
		Client:   func() (GmailClient, error) { return &fakeGmailClient{}, nil },
		Pipeline: func() (AnalyzePipeline, error) { return &fakeAnalyzePipeline{}, nil },
		Ask:      func(context.Context, string, []QATurn, string) (string, error) { return "x", nil },
	}
	h := GmailAnalyzeMethods(deps)["miniapp.gmail.ask"]
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.ask", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error for missing question")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want MISSING_PARAM", resp.Error.Code)
	}
}

func TestGmailAsk_NotRegisteredWithoutAskCallback(t *testing.T) {
	deps := GmailAnalyzeDeps{
		Client:   func() (GmailClient, error) { return &fakeGmailClient{}, nil },
		Pipeline: func() (AnalyzePipeline, error) { return &fakeAnalyzePipeline{}, nil },
		// Ask is nil → ask must not be registered.
	}
	if _, ok := GmailAnalyzeMethods(deps)["miniapp.gmail.ask"]; ok {
		t.Error("ask must not be registered when Ask callback is nil")
	}
}

func TestBuildMailQAContext_NoCachedAnalysis(t *testing.T) {
	deps := GmailAnalyzeDeps{Cache: NewAnalysisStore(t.TempDir())}
	msg := &gmail.MessageDetail{ID: "x", Subject: "S", From: "f@x.com", Body: "본문내용"}
	out := buildMailQAContext(msg, deps)
	if !strings.Contains(out, "본문내용") {
		t.Errorf("context missing body: %s", out)
	}
	if strings.Contains(out, "## 분석") {
		t.Errorf("should have no analysis section on cache miss:\n%s", out)
	}
}
