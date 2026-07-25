package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// readTurnLLM returns the turn.llm rows the runner wrote under dir.
func readTurnLLM(t *testing.T, dir, session string) []agentlog.TurnLLMData {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, session+".jsonl"))
	if err != nil {
		t.Fatalf("read agent log: %v", err)
	}
	var out []agentlog.TurnLLMData
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var e struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(line), &e) != nil || e.Type != agentlog.TypeTurnLLM {
			continue
		}
		var d agentlog.TurnLLMData
		if json.Unmarshal(e.Data, &d) == nil {
			out = append(out, d)
		}
	}
	return out
}

// The whole point of the collector is that the failure mix reaches the LEDGER —
// a counter that stays inside the process answers nothing later.
func TestLogTurnDetailStampsRetryTelemetry(t *testing.T) {
	dir := t.TempDir()
	writer := agentlog.NewWriter(dir)
	runner := &agentRunner{
		runLog: agentlog.NewRunLogger(writer, "client:test", "run-1"),
		state:  &agentRunState{},
	}
	// Populated through the REAL retry loop rather than a test backdoor: the
	// claim under test is that a provider 429 travels all the way from the
	// transport layer into the ledger row.
	collector := absorbRateLimits(t, 2)
	runner.turnRetries = collector

	runner.logTurnDetail(
		preparedAgentTurn{index: 0, request: preparedTurnRequest{}},
		&turnResult{stopReason: "end_turn", providerModel: "kimi-k3-fallback"},
	)

	rows := readTurnLLM(t, dir, "client:test")
	if len(rows) != 1 {
		t.Fatalf("turn.llm rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Retries != 2 {
		t.Errorf("Retries = %d, want 2", got.Retries)
	}
	if got.RetryMix != "429x2" {
		t.Errorf("RetryMix = %q, want %q", got.RetryMix, "429x2")
	}
	if !got.RateLimited {
		t.Error("RateLimited must be set when a 429 was absorbed")
	}
	if got.ProviderModel != "kimi-k3-fallback" {
		t.Errorf("ProviderModel = %q — fallback must be visible in the ledger", got.ProviderModel)
	}
}

// A clean turn must stay byte-clean: every new field is omitempty, so an
// unaffected run's rows do not change shape at all.
func TestLogTurnDetailOmitsRetryTelemetryWhenClean(t *testing.T) {
	dir := t.TempDir()
	writer := agentlog.NewWriter(dir)
	runner := &agentRunner{
		runLog: agentlog.NewRunLogger(writer, "client:clean", "run-2"),
		state:  &agentRunState{},
	}
	_, collector := llm.WithRetryCollector(context.Background())
	runner.turnRetries = collector

	runner.logTurnDetail(
		preparedAgentTurn{index: 0, request: preparedTurnRequest{}},
		&turnResult{stopReason: "end_turn"},
	)

	raw, err := os.ReadFile(filepath.Join(dir, "client:clean.jsonl"))
	if err != nil {
		t.Fatalf("read agent log: %v", err)
	}
	for _, field := range []string{"retries", "retryMix", "rateLimited", "providerModel"} {
		if strings.Contains(string(raw), field) {
			t.Errorf("clean turn must omit %q, got: %s", field, raw)
		}
	}
}

// A nil collector (a turn that never reached streamTurn) must not panic — the
// telemetry path has to be strictly weaker than the run path.
func TestLogTurnDetailSurvivesNilCollector(t *testing.T) {
	dir := t.TempDir()
	runner := &agentRunner{
		runLog: agentlog.NewRunLogger(agentlog.NewWriter(dir), "client:nil", "run-3"),
		state:  &agentRunState{},
	}
	runner.logTurnDetail(
		preparedAgentTurn{index: 0, request: preparedTurnRequest{}},
		&turnResult{stopReason: "end_turn"},
	)
	if rows := readTurnLLM(t, dir, "client:nil"); len(rows) != 1 {
		t.Fatalf("turn.llm rows = %d, want 1", len(rows))
	}
}

// absorbRateLimits drives the real llm retry loop against a server that answers
// n rate limits before succeeding, and returns the collector it filled.
func absorbRateLimits(t *testing.T, n int) *llm.RetryCollector {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls <= n {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, "slow down")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(server.Close)

	client := llm.NewClient(server.URL, "test-key",
		llm.WithRetry(n+1, 5*time.Millisecond, 20*time.Millisecond))
	ctx, collector := llm.WithRetryCollector(context.Background())
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	body, err := client.DoStream(ctx, req)
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	body.Close()
	return collector
}
