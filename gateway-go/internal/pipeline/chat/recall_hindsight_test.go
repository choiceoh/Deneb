package chat

import (
	"context"
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
	out := buildRecallPreflight(context.Background(),
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

func TestBuildRecallPreflightSkipsHindsightWithoutCue(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"id":"m1","text":"unused"}]}`)
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.Config{BaseURL: srv.URL, BankID: "deneb"})
	// No cue → hindsight must not be called (cost outweighs benefit on no-recall
	// turns). The hybrid semantic search is preserved for cue-bearing turns.
	out := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "telegram:1", Message: "오늘 저녁 뭐 먹지?"},
		runDeps{hindsightClient: client},
		nil,
	)
	if out != "" {
		t.Fatalf("no-cue turn must inject nothing, got %q", out)
	}
	if called {
		t.Fatal("hindsight must not be called on a no-cue turn")
	}
}

func TestBuildRecallPreflightSurvivesHindsightFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.Config{BaseURL: srv.URL, BankID: "deneb"})
	out := buildRecallPreflight(context.Background(),
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
