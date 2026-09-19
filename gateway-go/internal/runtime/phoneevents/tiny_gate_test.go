package phoneevents

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// TestMain keeps every test in the package off real models: the default gate
// call fails at once, as it does when no model is reachable. Without it a
// notification-type processJudgment dialed the default vLLM address and sat
// through the LLM client's retry backoff — about 70s per call.
func TestMain(m *testing.M) {
	tinyYesNo = func(context.Context, string, string) (pilot.YesNo, error) {
		return pilot.YesNo{}, errors.New("no model in tests")
	}
	os.Exit(m.Run())
}

// stubTinyGate answers every gate call with res and records the user message.
func stubTinyGate(t *testing.T, res pilot.YesNo, err error) *string {
	t.Helper()
	var seen string
	prev := tinyYesNo
	tinyYesNo = func(_ context.Context, system, user string) (pilot.YesNo, error) {
		if !strings.Contains(system, "YES 또는 NO 한 단어만") {
			t.Errorf("gate system prompt lost its one-word instruction: %q", system)
		}
		seen = user
		return res, err
	}
	t.Cleanup(func() { tinyYesNo = prev })
	return &seen
}

func TestWorthFullJudgmentComparesProbabilityWithThreshold(t *testing.T) {
	seen := stubTinyGate(t, pilot.YesNo{PYes: 0.4, HasProb: true, Model: "tiny"}, nil)
	if v := worthFullJudgment(context.Background(), "카카오톡", "내일 9시 실사", 0.5); v.pass || v.mode != "prob" || v.pYes != 0.4 {
		t.Fatalf("P(YES)=0.4 at threshold 0.5 = %#v, want a drop decided by probability", v)
	}
	if v := worthFullJudgment(context.Background(), "카카오톡", "내일 9시 실사", 0.3); !v.pass {
		t.Fatalf("P(YES)=0.4 at threshold 0.3 = %#v, want a pass", v)
	}
	if *seen != "앱: 카카오톡\n알림 내용:\n내일 9시 실사" {
		t.Fatalf("gate user message = %q", *seen)
	}
}

// Backends without logprobs (the OpenRouter rungs) leave only the token; the
// gate reads it as the text gate always did — only an explicit NO drops.
func TestWorthFullJudgmentReadsTokenWithoutProbability(t *testing.T) {
	for _, tc := range []struct {
		token string
		pass  bool
	}{
		{"NO", false},
		{"no", false},
		{"YES", true},
		{"**", true},
	} {
		stubTinyGate(t, pilot.YesNo{Token: tc.token, Model: "nemotron"}, nil)
		if v := worthFullJudgment(context.Background(), "메시지", "x", 0.5); v.pass != tc.pass || v.mode != "token" {
			t.Errorf("token %q = %#v, want pass=%v by token", tc.token, v, tc.pass)
		}
	}
}

func TestWorthFullJudgmentFailsOpenWhenNoModelAnswers(t *testing.T) {
	stubTinyGate(t, pilot.YesNo{}, errors.New("all models failed"))
	if v := worthFullJudgment(context.Background(), "메시지", "x", 0.5); !v.pass || v.mode != "error" || v.err == nil {
		t.Fatalf("gate error = %#v, want fail-open", v)
	}
}

func TestTinyGateThresholdIgnoresOutOfRangeOverrides(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want float64
	}{
		{"", defaultTinyGateThreshold},
		{"0.3", 0.3},
		{" 0.35 ", 0.35},
		{"0", defaultTinyGateThreshold},
		{"1", defaultTinyGateThreshold},
		{"1.5", defaultTinyGateThreshold},
		{"high", defaultTinyGateThreshold},
	} {
		t.Setenv(tinyGateThresholdEnv, tc.env)
		if got := tinyGateThreshold(); got != tc.want {
			t.Errorf("%s=%q → %v, want %v", tinyGateThresholdEnv, tc.env, got, tc.want)
		}
	}
}

// A drop must reach the journal: it used to be Debug-only, so the gate's
// recall could not be measured without replaying the ledger.
func TestProcessJudgmentLogsGateDropAtInfo(t *testing.T) {
	stubTinyGate(t, pilot.YesNo{PYes: 0.02, HasProb: true, Model: "glm"}, nil)
	var logs bytes.Buffer
	probe := &recallProbeRunner{}
	h := &Handler{chatHandler: probe, logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	delivered, err := h.processJudgment(context.Background(), "notification", "AliExpress", "(광고) 오늘만 70% 할인")
	if delivered || err != nil {
		t.Fatalf("processJudgment = %v/%v, want a quiet drop", delivered, err)
	}
	if probe.got.SessionKey != "" {
		t.Fatalf("judgment turn ran for a dropped notification: %#v", probe.got)
	}
	line := logs.String()
	for _, want := range []string{"level=INFO", `msg="phone-event tiny-gate"`, "pass=false", "mode=prob", "p=0.02", "source=AliExpress"} {
		if !strings.Contains(line, want) {
			t.Errorf("gate log %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "70% 할인") {
		t.Errorf("gate log leaks the notification text: %q", line)
	}
}
