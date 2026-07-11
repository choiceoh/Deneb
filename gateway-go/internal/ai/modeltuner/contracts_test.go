package modeltuner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

func TestAdaptiveEffortEnabledContract(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{value: "1", want: true},
		{value: "true", want: true},
		{value: "TRUE", want: true},
		{value: " on ", want: true},
		{value: "yes", want: true},
		{value: ""},
		{value: "0"},
		{value: "false"},
		{value: "off"},
		{value: "no"},
		{value: "enabled"},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv(adaptiveEffortTuneEnv, tt.value)
			if got := adaptiveEffortEnabled(); got != tt.want {
				t.Fatalf("adaptiveEffortEnabled(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestEffortSignalContract(t *testing.T) {
	stat := agentlog.EffortStat{RoutedRuns: 8, KeptRuns: 2, EscalatedRuns: 2}
	got := effortSignal(stat)
	if got.Sample != 10 || got.RoutedRuns != 8 || got.EscalationRate != .25 || got.RoutedShare != .8 {
		t.Fatalf("signal = %+v", got)
	}
}

func TestAnalyzeThresholdBoundariesAndStableOrdering(t *testing.T) {
	stats := []agentlog.ModelStat{
		{Provider: "p", Model: "z", Runs: 4, MaxTokensRecoveries: 4, TimeoutRuns: 4, P95Ms: 999_999, ToolCalls: 100, ToolErrors: 100},
		{Provider: "p", Model: "b", Runs: 10, MaxTokensRecoveries: 2, TimeoutRuns: 2, CacheCreationTokens: 1, CacheReadTokens: 1, InputTokens: 9, P95Ms: 120_001, ThinkingRuns: 3, FallbackRuns: 3, ToolCalls: 20, ToolErrors: 3},
		{Provider: "p", Model: "a", Runs: 5, MaxTokensRecoveries: 1, TimeoutRuns: 1, CacheCreationTokens: 1, CacheReadTokens: 2, InputTokens: 8, P95Ms: 120_000, FallbackRuns: 1, ToolCalls: 19, ToolErrors: 19},
	}
	got := Analyze(stats)
	var keys []string
	for _, rec := range got {
		keys = append(keys, rec.Model+":"+rec.Rule)
		if rec.Model == "z" {
			t.Errorf("low sample recommendation: %+v", rec)
		}
	}
	want := []string{
		"a:max_tokens", "a:stall",
		"b:cache_break", "b:fallback", "b:latency", "b:max_tokens", "b:stall", "b:tool_errors",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	for _, rec := range got {
		if rec.Rule == "max_tokens" && rec.TunedMaxTokens != tunedMaxTokensFloor {
			t.Errorf("floor = %+v", rec)
		}
		if rec.Message == "" || rec.Provider != "p" {
			t.Errorf("recommendation = %+v", rec)
		}
	}
}

func TestAnalyzeDoesNotJudgeInactiveCacheOrLowToolSamples(t *testing.T) {
	got := Analyze([]agentlog.ModelStat{{Model: "m", Runs: 100, CacheCreationTokens: 0, CacheReadTokens: 0, InputTokens: 100, ToolCalls: 19, ToolErrors: 19}})
	if len(got) != 0 {
		t.Fatalf("recommendations = %+v", got)
	}
	got = Analyze([]agentlog.ModelStat{{Model: "m", Runs: 100, CacheCreationTokens: 1, CacheReadTokens: 0, InputTokens: 0}})
	if len(got) != 0 {
		t.Fatalf("zero denominator recommendations = %+v", got)
	}
}

func TestFingerprintContractAdditional(t *testing.T) {
	if got := Fingerprint(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	recs := []Recommendation{{Provider: "z", Model: "m", Rule: "stall", Message: "one"}, {Provider: "a", Model: "m", Rule: "latency"}, {Provider: "z", Model: "m", Rule: "stall", Message: "two"}}
	if got := Fingerprint(recs); got != "a/m:latency,z/m:stall,z/m:stall" {
		t.Fatalf("fingerprint = %q", got)
	}
	if recs[0].Provider != "z" {
		t.Fatal("input mutated")
	}
}

func TestKoreanRatioAdditional(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  float64
	}{
		{input: ""},
		{input: "123 !"},
		{input: "abc", want: 0},
		{input: "가나다", want: 1},
		{input: "가a나b", want: .5},
		{input: "漢字가", want: 1.0 / 3.0},
		{input: "가-나 123", want: 1},
	} {
		if got := koreanRatio(tt.input); got != tt.want {
			t.Errorf("koreanRatio(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestScorecardNoteForContractAdditional(t *testing.T) {
	sc := Scorecard{
		WindowHours: 24,
		Models: []agentlog.ModelStat{
			{Model: "empty", Runs: 0},
			{Model: "m", Runs: 10, P95Ms: 12_500, CacheCreationTokens: 1, CacheReadTokens: 80, InputTokens: 20, FallbackRuns: 2, TimeoutRuns: 1},
		},
		Calibrations: map[string]Calibration{"m": {LatencyMs: 1500, KoreanOK: false}},
	}
	got := sc.NoteFor("m", 16_384)
	for _, want := range []string{"24h 10런", "p95 12초", "캐시 80%", "폴백 2", "스톨 1", "프로브 1.5초⚠한글", "출력 floor 16384"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	if got := sc.NoteFor("missing", 0); got != "" {
		t.Fatalf("missing = %q", got)
	}
	if got := (Scorecard{Calibrations: map[string]Calibration{"m": {LatencyMs: 1000, KoreanOK: true}}}).NoteFor("m", 0); got != "프로브 1.0초✓" {
		t.Fatalf("calibration-only = %q", got)
	}
}

func TestAdvisoryLinesContractAdditional(t *testing.T) {
	sc := Scorecard{Recommendations: []Recommendation{{Provider: "p", Model: "m", Message: "message"}, {Model: "bare", Message: "check"}}}
	want := []string{"p/m: message", "/bare: check"}
	if got := sc.AdvisoryLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %v", got)
	}
	if got := (Scorecard{}).AdvisoryLines(); got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

func TestFormatNotificationContractAdditional(t *testing.T) {
	got := formatNotification([]Recommendation{{Provider: "p", Model: "m", Message: "check"}})
	for _, want := range []string{"📊 모델 튜너", "p/m: check", "~/.deneb/model-stats.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func newProbeServer(t *testing.T, chunks ...map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("request: %v", err)
		}
		if req["model"] != "probe-model" || req["max_tokens"] != float64(calibrationMaxTokens) {
			t.Errorf("request = %#v", req)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			data, _ := json.Marshal(map[string]any{"id": "c", "model": "probe-model", "choices": []any{map[string]any{"index": 0, "delta": chunk}}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestProbeModelFiltersThinkingAndMeasuresAnswer(t *testing.T) {
	srv := newProbeServer(
		t,
		map[string]any{"role": "assistant", "reasoning_content": "내부 추론"},
		map[string]any{"content": "안녕하세요 반갑습니다"},
	)
	defer srv.Close()
	got, err := probeModel(context.Background(), llm.NewClient(srv.URL, "key"), "probe-model")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "probe-model" || got.OutputLen != len("안녕하세요 반갑습니다") || !got.KoreanOK || got.Ts <= 0 || got.LatencyMs < 0 {
		t.Fatalf("calibration = %+v", got)
	}
}

func TestProbeModelFailureContracts(t *testing.T) {
	empty := newProbeServer(t)
	defer empty.Close()
	if _, err := probeModel(context.Background(), llm.NewClient(empty.URL, "key"), "probe-model"); err == nil || !strings.Contains(err.Error(), "empty probe") {
		t.Fatalf("empty error = %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "offline", http.StatusServiceUnavailable) }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := probeModel(ctx, llm.NewClient(srv.URL, "key"), "probe-model"); err == nil {
		t.Fatal("HTTP failure accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := probeModel(canceled, llm.NewClient(empty.URL, "key"), "probe-model"); err == nil {
		t.Fatal("cancel accepted")
	}
}

type contractStats struct {
	stats  []agentlog.ModelStat
	effort map[string]agentlog.EffortStat
	since  []int64
}

func (s *contractStats) AggregateByModel(since int64) []agentlog.ModelStat {
	s.since = append(s.since, since)
	return s.stats
}

func (s *contractStats) AggregateEffortByModel(since int64) map[string]agentlog.EffortStat {
	s.since = append(s.since, since)
	return s.effort
}

func TestTaskMetadataAndNilLogsRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scorecard.json")
	task := NewTask(Deps{StatePath: path, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if task.Name() != "model-tuner" || task.Interval() != 6*time.Hour {
		t.Fatalf("metadata = %s/%s", task.Name(), task.Interval())
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := LoadScorecard(path)
	if got.WindowHours != 24 || got.Models != nil || got.Recommendations != nil {
		t.Fatalf("scorecard = %+v", got)
	}
}

func TestTaskRunPersistsNotifiesOnChangeAndSuppressesSameSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scorecard.json")
	logs := &contractStats{stats: []agentlog.ModelStat{{Provider: "p", Model: "m", Runs: 10, TimeoutRuns: 3}}}
	var notices []string
	fail := false
	task := NewTask(Deps{Logs: logs, StatePath: path, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Notify: func(_ context.Context, msg string) error {
		if fail {
			return errors.New("offline")
		}
		notices = append(notices, msg)
		return nil
	}})
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || len(logs.since) != 2 {
		t.Fatalf("notices/since = %d/%v", len(notices), logs.since)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 {
		t.Fatalf("same set renotified: %d", len(notices))
	}
	logs.stats = append(logs.stats, agentlog.ModelStat{Provider: "p", Model: "z", Runs: 10, TimeoutRuns: 3})
	fail = true
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 {
		t.Fatal("failed notify appended")
	}
	if got := LoadScorecard(path); len(got.Recommendations) != 2 {
		t.Fatalf("scorecard = %+v", got)
	}
}

func TestScorecardLoadSaveContracts(t *testing.T) {
	if got := LoadScorecard(""); !reflect.DeepEqual(got, Scorecard{}) {
		t.Fatalf("empty = %+v", got)
	}
	if err := saveScorecard("", Scorecard{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scorecard.json")
	want := Scorecard{GeneratedAtMs: 1, WindowHours: 24, Models: []agentlog.ModelStat{{Model: "m"}}, Recommendations: []Recommendation{{Model: "m", Rule: "stall"}}, Calibrations: map[string]Calibration{"m": {Model: "m"}}}
	if err := saveScorecard(path, want); err != nil {
		t.Fatal(err)
	}
	if got := LoadScorecard(path); !reflect.DeepEqual(got, want) {
		t.Fatalf("roundtrip = %+v", got)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v/%v", info, err)
	}
	bad := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadScorecard(bad); !reflect.DeepEqual(got, Scorecard{}) {
		t.Fatalf("bad = %+v", got)
	}
	if got := LoadScorecard(filepath.Join(t.TempDir(), "missing")); !reflect.DeepEqual(got, Scorecard{}) {
		t.Fatalf("missing = %+v", got)
	}
	parentFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveScorecard(filepath.Join(parentFile, "x"), Scorecard{}); err == nil {
		t.Fatal("save failure swallowed")
	}
}

func TestDefaultStatePathContract(t *testing.T) {
	path := DefaultStatePath()
	if filepath.Base(path) != "model-stats.json" || filepath.Dir(path) == "." {
		t.Fatalf("path = %q", path)
	}
}
