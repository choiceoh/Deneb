// Package modeltuner is the background per-model optimization loop:
// measure (agent-log scorecard) → threshold (rules.go) → adjust/advise.
//
// Every cycle it aggregates the last 24h of agent logs by model, evaluates
// the tuning rules, auto-applies the one safe adjustment (output-token floor
// for models that keep hitting the ceiling), persists a scorecard to
// ~/.deneb/model-stats.json, and notifies the operator in Korean — but only
// when the recommendation set actually changed. New locally-served (vLLM)
// models get a one-shot calibration probe so their baseline latency and
// Korean-output sanity are on record before they take real traffic.
package modeltuner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

const (
	taskInterval = 6 * time.Hour
	statsWindow  = 24 * time.Hour

	calibrationTimeout   = 60 * time.Second
	calibrationMaxTokens = 256
	calibrationPrompt    = "안녕하세요. 한 문장으로 자기소개해 주세요."
)

// statsSource abstracts agentlog.Writer for tests.
type statsSource interface {
	AggregateByModel(sinceMs int64) []agentlog.ModelStat
}

// Calibration records the one-shot probe result for a locally served model.
type Calibration struct {
	Model     string `json:"model"`
	Provider  string `json:"provider"`
	LatencyMs int64  `json:"latencyMs"`
	OutputLen int    `json:"outputLen"`
	KoreanOK  bool   `json:"koreanOk"`
	Ts        int64  `json:"ts"` //nolint:staticcheck // ST1003 — JSON field name
}

// Scorecard is the persisted output of one tuner cycle.
type Scorecard struct {
	GeneratedAtMs   int64                  `json:"generatedAtMs"`
	WindowHours     int                    `json:"windowHours"`
	Models          []agentlog.ModelStat   `json:"models"`
	Recommendations []Recommendation       `json:"recommendations"`
	Calibrations    map[string]Calibration `json:"calibrations,omitempty"`
}

// Deps wires the tuner task into the gateway.
type Deps struct {
	Logs      statsSource
	Registry  *modelrole.Registry
	StatePath string                                      // scorecard JSON path (e.g. ~/.deneb/model-stats.json)
	Notify    func(ctx context.Context, msg string) error // optional operator delivery
	Logger    *slog.Logger
}

// Task implements autonomous.PeriodicTask (structurally — no import needed).
type Task struct {
	deps Deps
}

// NewTask builds the periodic model-tuner task.
func NewTask(deps Deps) *Task {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Task{deps: deps}
}

func (t *Task) Name() string            { return "model-tuner" }
func (t *Task) Interval() time.Duration { return taskInterval }

// Run executes one measure → threshold → adjust cycle.
func (t *Task) Run(ctx context.Context) error {
	since := time.Now().Add(-statsWindow).UnixMilli()
	stats := t.deps.Logs.AggregateByModel(since)
	recs := Analyze(stats)
	prev := loadScorecard(t.deps.StatePath)

	// Auto-apply: the max-tokens floor is the only adjustment safe enough to
	// act on without a human (bounded, raise-only, request-level). Models
	// whose recommendation disappeared get their floor cleared so a fixed
	// model returns to the default budget.
	if t.deps.Registry != nil {
		applied := map[string]bool{}
		for _, r := range recs {
			if r.TunedMaxTokens > 0 {
				t.deps.Registry.SetTunedMaxTokens(r.Model, r.TunedMaxTokens)
				applied[r.Model] = true
			}
		}
		for _, r := range prev.Recommendations {
			if r.TunedMaxTokens > 0 && !applied[r.Model] {
				t.deps.Registry.SetTunedMaxTokens(r.Model, 0)
			}
		}
	}

	calibs := t.calibrate(ctx, prev.Calibrations)

	sc := Scorecard{
		GeneratedAtMs:   time.Now().UnixMilli(),
		WindowHours:     int(statsWindow.Hours()),
		Models:          stats,
		Recommendations: recs,
		Calibrations:    calibs,
	}
	if err := saveScorecard(t.deps.StatePath, sc); err != nil {
		return fmt.Errorf("save scorecard: %w", err)
	}
	t.deps.Logger.Info("modeltuner: cycle complete",
		"models", len(stats), "recommendations", len(recs), "calibrations", len(calibs))

	// Notify only when the recommendation set changed and is non-empty —
	// a resolved situation goes quiet instead of pinging "all clear".
	if t.deps.Notify != nil && len(recs) > 0 && Fingerprint(recs) != Fingerprint(prev.Recommendations) {
		if err := t.deps.Notify(ctx, formatNotification(recs)); err != nil {
			t.deps.Logger.Warn("modeltuner: notify failed", "error", err)
		}
	}
	return nil
}

// calibrate runs the one-shot probe for locally served vLLM models that have
// no calibration on record. Prior results are carried forward; failures are
// not recorded so the next cycle retries (a vLLM restart window shouldn't
// permanently mark a model uncalibrated).
func (t *Task) calibrate(ctx context.Context, prior map[string]Calibration) map[string]Calibration {
	out := make(map[string]Calibration, len(prior))
	for k, v := range prior {
		out[k] = v
	}
	if t.deps.Registry == nil {
		return out
	}
	for role, cfg := range t.deps.Registry.ConfiguredModels() {
		if cfg.ProviderID != "vllm" || cfg.Model == "" {
			continue
		}
		if _, done := out[cfg.Model]; done {
			continue
		}
		client := t.deps.Registry.Client(role)
		if client == nil {
			continue
		}
		cal, err := probeModel(ctx, client, cfg.Model)
		if err != nil {
			t.deps.Logger.Warn("modeltuner: calibration probe failed",
				"model", cfg.Model, "error", err)
			continue
		}
		cal.Provider = cfg.ProviderID
		out[cfg.Model] = cal
		t.deps.Logger.Info("modeltuner: model calibrated",
			"model", cfg.Model, "latencyMs", cal.LatencyMs, "koreanOk", cal.KoreanOK)
	}
	return out
}

// probeModel sends one short Korean prompt and measures wall time + output.
func probeModel(ctx context.Context, client *llm.Client, model string) (Calibration, error) {
	probeCtx, cancel := context.WithTimeout(ctx, calibrationTimeout)
	defer cancel()

	start := time.Now()
	events, err := client.StreamChat(probeCtx, llm.ChatRequest{
		Model:     model,
		MaxTokens: calibrationMaxTokens,
		Messages:  []llm.Message{llm.NewTextMessage("user", calibrationPrompt)},
	})
	if err != nil {
		return Calibration{}, err
	}
	var text strings.Builder
	for ev := range events {
		switch ev.Type {
		case "content_block_delta":
			var d llm.ContentBlockDelta
			if json.Unmarshal(ev.Payload, &d) == nil {
				text.WriteString(d.Delta.Text)
			}
		case "error":
			return Calibration{}, fmt.Errorf("stream error: %s", string(ev.Payload))
		}
	}
	if probeCtx.Err() != nil {
		return Calibration{}, probeCtx.Err()
	}
	output := strings.TrimSpace(text.String())
	if output == "" {
		return Calibration{}, fmt.Errorf("empty probe output")
	}
	return Calibration{
		Model:     model,
		LatencyMs: time.Since(start).Milliseconds(),
		OutputLen: len(output),
		KoreanOK:  koreanRatio(output) > 0.3,
		Ts:        time.Now().UnixMilli(),
	}, nil
}

// koreanRatio returns the Hangul fraction among letters in s.
func koreanRatio(s string) float64 {
	var hangul, letters int
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Hangul, r) {
			hangul++
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(hangul) / float64(letters)
}

// formatNotification builds the Korean operator message.
func formatNotification(recs []Recommendation) string {
	var sb strings.Builder
	sb.WriteString("📊 모델 튜너: 최근 24시간 분석에서 조치/점검 항목이 갱신되었습니다.\n")
	for _, r := range recs {
		fmt.Fprintf(&sb, "- %s/%s: %s\n", r.Provider, r.Model, r.Message)
	}
	sb.WriteString("상세: ~/.deneb/model-stats.json")
	return sb.String()
}

// loadScorecard reads the prior scorecard; a missing or corrupt file yields
// the zero Scorecard (first run, or start over).
func loadScorecard(path string) Scorecard {
	var sc Scorecard
	if path == "" {
		return sc
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return sc
	}
	_ = json.Unmarshal(raw, &sc)
	return sc
}

// saveScorecard atomically persists the scorecard.
func saveScorecard(path string, sc Scorecard) error {
	if path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
