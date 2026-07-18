package observe

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	observecore "github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func newCapture(t *testing.T, size int) (*observecore.LogCapture, *slog.Logger) {
	t.Helper()
	ring := observecore.NewRing(size)
	delegate := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	capture := observecore.NewCapture(delegate, ring)
	return capture, slog.New(capture)
}

func decodeObservePayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	rpctest.MustOK(t, resp)
	var got T
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, resp.Payload)
	}
	return got
}

func TestDependencyAccessorsNilGetterAndNilResult(t *testing.T) {
	if got := (Deps{}).ring(); got != nil {
		t.Fatalf("nil capture ring = %#v", got)
	}
	if got := (Deps{}).alog(); got != nil {
		t.Fatalf("nil agentlog getter = %#v", got)
	}
	called := 0
	deps := Deps{AgentLog: func() *agentlog.Writer { called++; return nil }}
	if got := deps.alog(); got != nil || called != 1 {
		t.Fatalf("nil getter result = %#v calls=%d", got, called)
	}
	capture, _ := newCapture(t, 2)
	if got := (Deps{Capture: capture}).ring(); got != capture.Ring() {
		t.Fatal("capture ring identity lost")
	}
}

func TestMethodsReturnExactFourNamedLocalAndMiniappEndpoints(t *testing.T) {
	local := Methods(Deps{})
	mini := MiniappMethods(Deps{})
	if len(local) != 6 || len(mini) != 6 {
		t.Fatalf("method sizes local=%d mini=%d", len(local), len(mini))
	}
	localNames := make([]string, 0, len(local))
	miniNames := make([]string, 0, len(mini))
	for name := range local {
		localNames = append(localNames, name)
	}
	for name := range mini {
		miniNames = append(miniNames, name)
	}
	sort.Strings(localNames)
	sort.Strings(miniNames)
	if want := []string{"observe.behavior", "observe.health", "observe.logs", "observe.turn", "observe.workstation_feedback", "observe.workstation_usage"}; !reflect.DeepEqual(localNames, want) {
		t.Fatalf("local names = %#v", localNames)
	}
	if want := []string{"miniapp.observe.behavior", "miniapp.observe.health", "miniapp.observe.logs", "miniapp.observe.turn", "miniapp.observe.workstation_feedback", "miniapp.observe.workstation_usage"}; !reflect.DeepEqual(miniNames, want) {
		t.Fatalf("mini names = %#v", miniNames)
	}
}

func TestWorkstationUsageHandlerReadsLedgerAndDefaultsEmpty(t *testing.T) {
	// Empty deps → zeroed ledger, never an error (관찰 카드는 미기록도 정상 상태).
	empty := Methods(Deps{})["observe.workstation_usage"](context.Background(), &protocol.RequestFrame{ID: "u0"})
	rpctest.MustOK(t, empty)
	zero := rpctest.Result[struct {
		Total    int            `json:"total"`
		ByAction map[string]int `json:"byAction"`
	}](t, empty)
	if zero.Total != 0 || len(zero.ByAction) != 0 {
		t.Fatalf("empty ledger = %+v", zero)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := `{"total":3,"byAction":{"spotlight":2,"open":1},"lastAt":"2026-07-18T01:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "cache", "workstation_usage.json"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := Methods(Deps{StateDir: func() string { return dir }})["observe.workstation_usage"](
		context.Background(), &protocol.RequestFrame{ID: "u1"},
	)
	rpctest.MustOK(t, resp)
	got := rpctest.Result[struct {
		Total    int            `json:"total"`
		ByAction map[string]int `json:"byAction"`
		LastAt   string         `json:"lastAt"`
	}](t, resp)
	if got.Total != 3 || got.ByAction["spotlight"] != 2 || got.LastAt == "" {
		t.Fatalf("ledger read = %+v", got)
	}
}

func TestTurnHandlerValidationMalformedAndRingOnlyView(t *testing.T) {
	capture, logger := newCapture(t, 8)
	methods := Methods(Deps{Capture: capture})
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"runId":""}`), json.RawMessage(`{"runId":`)} {
		resp := methods["observe.turn"](context.Background(), &protocol.RequestFrame{ID: "turn", Params: raw})
		rpctest.MustErr(t, resp)
		if resp.Error.Code != protocol.ErrMissingParam || !strings.Contains(resp.Error.Message, "runId") {
			t.Errorf("params %q error = %#v", raw, resp.Error)
		}
	}

	logger.With("runId", "run-1", "session", "client:main", "tool", "read").Info("tool completed")
	logger.With("runId", "other", "session", "client:other").Warn("other warning")
	view := decodeObservePayload[map[string]json.RawMessage](t, rpctest.Call(methods, "observe.turn", map[string]any{"runId": "run-1"}))
	if len(view) == 0 {
		t.Fatal("ring-only turn view is empty")
	}
	encoded, _ := json.Marshal(view)
	text := string(encoded)
	if !strings.Contains(text, "run-1") || !strings.Contains(text, "tool completed") || strings.Contains(text, "other warning") {
		t.Fatalf("turn view join/filter = %s", text)
	}
}

func appendRecord(t *testing.T, capture *observecore.LogCapture, at time.Time, level slog.Level, msg string, attrs ...slog.Attr) {
	t.Helper()
	record := slog.NewRecord(at, level, msg, 0)
	record.AddAttrs(attrs...)
	if err := capture.Handle(context.Background(), record); err != nil {
		t.Fatalf("capture Handle: %v", err)
	}
}

func TestLogsHandlerFilteringOrderingLimitsAndMalformedFallback(t *testing.T) {
	capture, _ := newCapture(t, 16)
	now := time.Now()
	appendRecord(t, capture, now.Add(-48*time.Hour), slog.LevelError, "old failure",
		slog.String("runId", "r1"), slog.String("session", "s1"))
	appendRecord(t, capture, now.Add(-2*time.Hour), slog.LevelDebug, "debug lookup",
		slog.String("runId", "r1"), slog.String("session", "s1"))
	appendRecord(t, capture, now.Add(-time.Hour), slog.LevelInfo, "request finished",
		slog.String("runId", "r1"), slog.String("sessionKey", "s1"))
	appendRecord(t, capture, now.Add(-30*time.Minute), slog.LevelWarn, "request slow",
		slog.String("runId", "r2"), slog.String("session", "s1"))
	appendRecord(t, capture, now, slog.LevelError, "request failed",
		slog.String("runId", "r1"), slog.String("session", "s2"))
	methods := Methods(Deps{Capture: capture})
	type payload struct {
		Lines []observecore.LogLine `json:"lines"`
		Count int                   `json:"count"`
	}

	got := decodeObservePayload[payload](t, rpctest.Call(methods, "observe.logs", map[string]any{
		"runId": "r1", "level": "info", "contains": "request", "limit": 2,
	}))
	if got.Count != 2 || len(got.Lines) != 2 || got.Lines[0].Msg != "request failed" || got.Lines[1].Msg != "request finished" {
		t.Fatalf("filtered logs = %#v", got)
	}

	got = decodeObservePayload[payload](t, rpctest.Call(methods, "observe.logs", map[string]any{
		"session": "s1", "days": 1,
	}))
	if got.Count != 3 {
		t.Fatalf("one-day session logs = %#v", got)
	}

	// Explicit sinceMs takes precedence over days, even when days is huge.
	got = decodeObservePayload[payload](t, rpctest.Call(methods, "observe.logs", map[string]any{
		"days": 99, "sinceMs": now.Add(-45 * time.Minute).UnixMilli(),
	}))
	if got.Count != 2 {
		t.Fatalf("explicit since logs = %#v", got)
	}

	resp := methods["observe.logs"](context.Background(), &protocol.RequestFrame{ID: "bad", Params: json.RawMessage(`{"runId":`)})
	got = decodeObservePayload[payload](t, resp)
	if got.Count != 5 {
		t.Fatalf("malformed logs fallback = %#v", got)
	}
}

func TestLogsHandlerCaptureDisabledUsesNonNilEmptySlice(t *testing.T) {
	type payload struct {
		Lines           []observecore.LogLine `json:"lines"`
		Count           int                   `json:"count"`
		CaptureDisabled bool                  `json:"captureDisabled"`
	}
	got := decodeObservePayload[payload](t, rpctest.Call(Methods(Deps{}), "observe.logs", map[string]any{
		"runId": "ignored", "limit": 100,
	}))
	if !got.CaptureDisabled || got.Count != 0 || got.Lines == nil || len(got.Lines) != 0 {
		t.Fatalf("disabled capture payload = %#v", got)
	}
}

func appendAgentLog(t *testing.T, w *agentlog.Writer, ts int64, session, typ string, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal agent log data: %v", err)
	}
	if err := w.Append(agentlog.LogEntry{Ts: ts, Type: typ, Session: session, RunID: "run", Data: raw}); err != nil {
		t.Fatalf("append agent log: %v", err)
	}
}

func TestBehaviorHandlerAggregatesWindowAndMalformedFallback(t *testing.T) {
	w := agentlog.NewWriter(t.TempDir())
	now := time.Now()
	appendAgentLog(t, w, now.Add(-48*time.Hour).UnixMilli(), agentlog.SessionBackground, agentlog.TypeBackgroundJob,
		agentlog.BackgroundJobData{Name: "old", Outcome: "error"})
	appendAgentLog(t, w, now.Add(-time.Hour).UnixMilli(), agentlog.SessionBackground, agentlog.TypeBackgroundJob,
		agentlog.BackgroundJobData{Name: "gmail", Outcome: "ok"})
	appendAgentLog(t, w, now.Add(-30*time.Minute).UnixMilli(), agentlog.SessionBackground, agentlog.TypeBackgroundJob,
		agentlog.BackgroundJobData{Name: "gmail", Outcome: "error"})
	appendAgentLog(t, w, now.Add(-20*time.Minute).UnixMilli(), agentlog.SessionProactive, agentlog.TypeProactiveRelay,
		agentlog.ProactiveRelayData{Decision: "suppressed", Reason: "contentless"})
	deps := Deps{AgentLog: func() *agentlog.Writer { return w }}
	methods := Methods(deps)

	got := decodeObservePayload[agentlog.AggregateResult](t, rpctest.Call(methods, "observe.behavior", map[string]any{"days": 1}))
	if got.BackgroundJobs["gmail"] != 2 || got.BackgroundErrors["gmail"] != 1 || got.BackgroundJobs["old"] != 0 ||
		got.ProactiveDecisions["suppressed:contentless"] != 1 {
		t.Fatalf("one-day aggregate = %#v", got)
	}
	got = decodeObservePayload[agentlog.AggregateResult](t, rpctest.Call(methods, "observe.behavior", map[string]any{"sinceMs": now.Add(-40 * time.Minute).UnixMilli(), "days": 99}))
	if got.BackgroundJobs["gmail"] != 1 || got.ProactiveDecisions["suppressed:contentless"] != 1 {
		t.Fatalf("explicit since aggregate = %#v", got)
	}

	resp := methods["observe.behavior"](context.Background(), &protocol.RequestFrame{ID: "bad", Params: json.RawMessage(`{`)})
	got = decodeObservePayload[agentlog.AggregateResult](t, resp)
	if got.BackgroundJobs["old"] != 1 || got.BackgroundJobs["gmail"] != 2 {
		t.Fatalf("malformed behavior fallback = %#v", got)
	}
}

func TestBehaviorHandlerNilGetterResultReturnsInitializedMaps(t *testing.T) {
	for _, deps := range []Deps{
		{},
		{AgentLog: func() *agentlog.Writer { return nil }},
	} {
		got := decodeObservePayload[agentlog.AggregateResult](t, rpctest.Call(Methods(deps), "observe.behavior", nil))
		if got.ProactiveDecisions == nil || got.BackgroundJobs == nil || got.BackgroundErrors == nil ||
			len(got.ProactiveDecisions) != 0 || len(got.BackgroundJobs) != 0 || len(got.BackgroundErrors) != 0 {
			t.Fatalf("nil behavior aggregate = %#v", got)
		}
	}
}

func TestHealthHandlerReturnsRingAndAgentLogMetrics(t *testing.T) {
	capture, _ := newCapture(t, 4)
	now := time.Now()
	appendRecord(t, capture, now, slog.LevelInfo, "ok", slog.String("runId", "r"))
	appendRecord(t, capture, now, slog.LevelError, "error one", slog.String("runId", "r"))
	appendRecord(t, capture, now, slog.LevelError, "error two", slog.String("runId", "r"))
	w := agentlog.NewWriter(t.TempDir())
	appendAgentLog(t, w, now.UnixMilli(), agentlog.SessionBackground, agentlog.TypeBackgroundJob,
		agentlog.BackgroundJobData{Name: "cron", Outcome: "error"})
	appendAgentLog(t, w, now.UnixMilli(), "client:main", agentlog.TypeRunEnd,
		agentlog.RunEndData{InputTokens: 10, OutputTokens: 2, Proactive: true, Compacted: true})
	var vllmCalls atomic.Int32
	deps := Deps{
		Capture:  capture,
		AgentLog: func() *agentlog.Writer { return w },
		VllmBases: func() []string {
			vllmCalls.Add(1)
			return nil
		},
	}
	got := decodeObservePayload[map[string]any](t, rpctest.Call(Methods(deps), "observe.health", nil))
	want := map[string]any{
		"captureEnabled":      true,
		"agentLogEnabled":     true,
		"ringCapacity":        float64(4),
		"ringUsed":            float64(3),
		"recentErrors":        float64(2),
		"runs24h":             float64(1),
		"proactiveRuns24h":    float64(1),
		"compactedRuns24h":    float64(1),
		"backgroundErrors24h": float64(1),
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("health[%s] = %#v, want %#v; all=%#v", key, got[key], value, got)
		}
	}
	if _, ok := got["vllmPrefixCache"]; ok || vllmCalls.Load() != 1 {
		t.Fatalf("empty vLLM stats = %#v calls=%d", got["vllmPrefixCache"], vllmCalls.Load())
	}
}

func TestHealthHandlerVllmMetricsAndCancellationFallback(t *testing.T) {
	metrics := `
vllm:prefix_cache_queries_total{model_name="model-a",engine="0"} 10
vllm:prefix_cache_hits_total{model_name="model-a",engine="0"} 4
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			t.Errorf("metrics path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, metrics)
	}))
	defer server.Close()
	deps := Deps{VllmBases: func() []string { return []string{server.URL + "/v1"} }}
	got := decodeObservePayload[map[string]json.RawMessage](t, rpctest.Call(Methods(deps), "observe.health", nil))
	var stats []observecore.VllmPrefixCache
	if err := json.Unmarshal(got["vllmPrefixCache"], &stats); err != nil {
		t.Fatalf("decode vLLM stats: %v raw=%s", err, got["vllmPrefixCache"])
	}
	if len(stats) != 1 || stats[0].Model != "model-a" || stats[0].Queries != 10 || stats[0].Hits != 4 || stats[0].HitRatePct != 40 {
		t.Fatalf("vLLM stats = %#v", stats)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rawReq := &protocol.RequestFrame{ID: "cancel", Method: "observe.health"}
	resp := Methods(deps)["observe.health"](ctx, rawReq)
	cancelled := decodeObservePayload[map[string]any](t, resp)
	if cancelled["captureEnabled"] != false || cancelled["agentLogEnabled"] != false {
		t.Fatalf("cancelled health fallback = %#v", cancelled)
	}
	if _, ok := cancelled["vllmPrefixCache"]; ok {
		t.Fatalf("cancelled health retained vLLM stats: %#v", cancelled)
	}
}

func TestConcurrentLogsAndHealthReadsStayConsistent(t *testing.T) {
	capture, logger := newCapture(t, 32)
	methods := Methods(Deps{Capture: capture})
	const workers = 40
	start := make(chan struct{})
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			<-start
			logger.With("runId", "shared", "session", "s", "i", i).Info("event")
			if i%3 == 0 {
				resp := rpctest.Call(methods, "observe.logs", map[string]any{"runId": "shared", "limit": 10})
				if resp == nil || resp.Error != nil {
					t.Errorf("logs read %d = %#v", i, resp)
				}
			} else {
				resp := rpctest.Call(methods, "observe.health", nil)
				if resp == nil || resp.Error != nil {
					t.Errorf("health read %d = %#v", i, resp)
				}
			}
			done <- struct{}{}
		}(i)
	}
	close(start)
	for i := 0; i < workers; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent observe operation timed out")
		}
	}
	got := decodeObservePayload[map[string]any](t, rpctest.Call(methods, "observe.health", nil))
	if got["ringUsed"] != float64(32) || got["ringCapacity"] != float64(32) {
		t.Fatalf("bounded ring after concurrent writes = %#v", got)
	}
}
