package wiki

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestDetectUserModelContradictions_NoClientIsNoOp(t *testing.T) {
	s := missStore(t)
	wd := &WikiDreamer{store: s}
	if got := wd.detectUserModelContradictions(context.Background()); got != nil {
		t.Fatalf("nil client must yield no findings, got %+v", got)
	}
}

func TestDetectUserModelContradictions_FlagsCrossAxisConflict(t *testing.T) {
	s := missStore(t)
	if err := s.WritePage("사용자/소통.md", &Page{Meta: Frontmatter{Title: "소통"}, Body: "간결한 답변 선호"}); err != nil {
		t.Fatal(err)
	}
	if err := s.WritePage("사용자/업무리듬.md", &Page{Meta: Frontmatter{Title: "업무리듬"}, Body: "상세한 보고를 좋아함"}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[{\"pageA\":\"사용자/소통.md\",\"pageB\":\"사용자/업무리듬.md\",\"contradiction\":\"간결 vs 상세\"}]"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	wd := &WikiDreamer{
		store:  s,
		client: llm.NewClient(srv.URL, ""),
		model:  "deepseek-v4-flash",
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	findings := wd.detectUserModelContradictions(context.Background())
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want 1", findings)
	}
	if findings[0].Type != "user_model_contradiction" || findings[0].PageA != "사용자/소통.md" || findings[0].PageB != "사용자/업무리듬.md" {
		t.Errorf("finding wrong: %+v", findings[0])
	}
	if findings[0].Fix != nil {
		t.Errorf("user-model contradiction must be advisory (no Fix): %+v", findings[0])
	}
}

func TestDetectUserModelContradictions_IgnoresDerivedAndHistoricalPages(t *testing.T) {
	s := missStore(t)
	if err := s.WritePage("사용자/live.md", &Page{Meta: Frontmatter{Title: "현행"}, Body: "간결한 답변 선호"}); err != nil {
		t.Fatal(err)
	}
	if err := s.WritePage("사용자/archived.md", &Page{Meta: Frontmatter{Title: "보관", Archived: true}, Body: "상세한 답변 선호"}); err != nil {
		t.Fatal(err)
	}
	if err := s.WritePage("사용자/old.md", &Page{Meta: Frontmatter{Title: "구버전", SupersededBy: "사용자/new.md"}, Body: "상세한 답변 선호"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFact(FactInput{
		Subject: "self", Key: "communication.response_length", Value: "짧게",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	}); err != nil {
		t.Fatal(err)
	}

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[]"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	wd := &WikiDreamer{
		store: s, client: llm.NewClient(srv.URL, ""), model: "deepseek-v4-flash",
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if got := wd.detectUserModelContradictions(context.Background()); got != nil {
		t.Fatalf("one live ordinary page cannot form a cross-axis conflict: %+v", got)
	}
	if calls != 0 {
		t.Fatalf("derived/historical user pages triggered contradiction LLM call: %d", calls)
	}
}
