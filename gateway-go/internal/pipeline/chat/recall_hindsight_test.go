package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/hindsight"
)

func TestRecallHindsightScoreDecays(t *testing.T) {
	if recallHindsightScore(0) <= recallHindsightScore(1) {
		t.Fatal("score should decay with rank")
	}
	if got := recallHindsightScore(100); got != 0.60 {
		t.Fatalf("score should floor at 0.60, got %v", got)
	}
}

func TestParseRecallTimestamp(t *testing.T) {
	if got := parseRecallTimestamp("", "2026-05-01T09:00:00Z"); got <= 0 {
		t.Fatalf("expected a parsed timestamp, got %d", got)
	}
	if got := parseRecallTimestamp("", "not-a-date"); got != 0 {
		t.Fatalf("expected 0 for unparseable input, got %d", got)
	}
}

func TestRecallHindsightEvidenceNilClient(t *testing.T) {
	if ev := recallHindsightEvidence(context.Background(), nil, "anything", nil); ev != nil {
		t.Fatalf("nil client should yield no evidence, got %v", ev)
	}
}

func TestBuildRecallPreflightInjectsHindsightEvidence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/default/banks/deneb/memories/recall" {
			t.Errorf("unexpected recall path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[
			{"id":"m1","text":"Deneb 회상 개선은 서버 preflight 방식으로 결정했다","type":"experience","context":"project decision","mentioned_at":"2026-05-10T12:00:00Z"}
		]}`)
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.Config{BaseURL: srv.URL, BankID: "deneb"})
	out, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "전에 회상 개선 얘기했던 거 기억나?"},
		runDeps{hindsightClient: client},
		nil,
	)
	if !strings.Contains(out, "source=hindsight") {
		t.Fatalf("expected hindsight evidence row, got %q", out)
	}
	if !strings.Contains(out, "서버 preflight 방식") {
		t.Fatalf("expected hindsight memory text in evidence, got %q", out)
	}
	if !strings.Contains(out, "confidence=high") {
		t.Fatalf("expected top hindsight hit to be high confidence, got %q", out)
	}
}

func TestBuildRecallPreflightSearchesHindsightEveryTurn(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"id":"m1","text":"unused"}]}`)
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.Config{BaseURL: srv.URL, BankID: "deneb"})
	// Hermes-style auto_recall: every source (incl. hindsight) is searched on EVERY turn,
	// not just cue turns. A no-cue turn with a matching hit injects evidence silently.
	out, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "오늘 저녁 뭐 먹지?"},
		runDeps{hindsightClient: client},
		nil,
	)
	if !called {
		t.Fatal("hindsight must be searched on every turn (auto_recall)")
	}
	if out == "" {
		t.Fatal("a no-cue turn with a hindsight hit must inject evidence silently, got empty")
	}
}

func TestBuildRecallPreflightSurvivesHindsightFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.Config{BaseURL: srv.URL, BankID: "deneb"})
	out, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "전에 얘기했던 거 기억나?"},
		runDeps{hindsightClient: client},
		nil,
	)
	// No other evidence sources are wired, so the preflight degrades to the
	// no-evidence stub rather than crashing on the Hindsight error.
	if !strings.Contains(out, "source=none") {
		t.Fatalf("expected graceful no-evidence fallback, got %q", out)
	}
}

func TestClipRunesAtBoundary(t *testing.T) {
	// Word-boundary clip: never cut mid-word.
	got := clipRunesAtBoundary("현대차 울산공장 모듈 납품 결제기한 확인 요청", 12)
	if strings.HasSuffix(got, "납") || len([]rune(got)) > 12 {
		t.Errorf("bad clip: %q", got)
	}
	// Short input untouched.
	if got := clipRunesAtBoundary("짧은 질문", 400); got != "짧은 질문" {
		t.Errorf("short input modified: %q", got)
	}
	// No whitespace in range → hard cut at the cap, still valid runes.
	long := strings.Repeat("가", 50)
	if got := clipRunesAtBoundary(long, 10); len([]rune(got)) != 10 {
		t.Errorf("hard cut failed: %d runes", len([]rune(got)))
	}
}

// TestRecallHindsightEvidence_QueryTooLongRetry: the bank enforces a
// server-side TOKEN limit; on its "Query too long" 400 the source must halve
// the query and retry instead of contributing nothing on long Korean turns
// (the exact failure observed in production logs on 2026-06-10).
func TestRecallHindsightEvidence_QueryTooLongRetry(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		queries = append(queries, req.Query)
		if len([]rune(req.Query)) > 250 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"Query too long: 682 tokens exceeds maximum of 500."}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"id":"m1","text":"현대차 울산 결제기한은 6월 말로 합의","type":"fact"}]}`))
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.Config{BaseURL: srv.URL, BankID: "deneb"})
	message := strings.Repeat("업무 맥락 설명 ", 60) // ~420 runes → first call rejected
	ev := recallHindsightEvidence(context.Background(), client, message, nil)

	if len(queries) != 2 {
		t.Fatalf("want reject+retry (2 calls), got %d: %v", len(queries), queries)
	}
	if len([]rune(queries[1])) >= len([]rune(queries[0])) {
		t.Errorf("retry query not shortened: %d vs %d runes", len([]rune(queries[1])), len([]rune(queries[0])))
	}
	if len(ev) != 1 || !strings.Contains(ev[0].Note, "결제기한") {
		t.Errorf("retry evidence lost: %+v", ev)
	}
}
