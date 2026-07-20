package genesis

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func judgeVerdictPayload(t *testing.T, pass bool, orig, cand float64, reason string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"pass": pass, "original_score": orig, "candidate_score": cand, "reason": reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

// newScriptedJudge serves payloads[i] on the i-th LLM call and reports the
// call count. An empty payload string yields an empty completion (the judge's
// empty-verdict error path) without engaging HTTP retry.
func newScriptedJudge(t *testing.T, payloads []string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(calls.Add(1)) - 1
		if i >= len(payloads) {
			t.Errorf("unexpected judge call %d (scripted %d)", i+1, len(payloads))
			i = len(payloads) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestSSEJSON(t, w, payloads[i])
	}))
}

func newSwapTestEvolver() *Evolver {
	return NewEvolver(nil, skills.NewCatalog(nil), nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestJudgeCandidateSwapProbe covers the order-swap consistency gate around an
// accepting forward verdict: a consistent judge (reversed pair rejected)
// commits, an order-inconsistent judge (both directions blessed) is refused
// fail-closed, and a swap-probe failure surfaces as an error so the caller
// keeps the original.
func TestJudgeCandidateSwapProbe(t *testing.T) {
	stats := &UsageStats{TotalUses: 5, SuccessCount: 2, FailureCount: 3, SuccessRate: 0.4}

	t.Run("consistent judge passes and records provenance", func(t *testing.T) {
		var calls atomic.Int32
		srv := newScriptedJudge(t, []string{
			judgeVerdictPayload(t, true, 70, 90, "forward improvement"),
			judgeVerdictPayload(t, false, 90, 70, "reversed pair is a regression"),
		}, &calls)
		defer srv.Close()

		e := newSwapTestEvolver()
		prov := &evolveProvenance{}
		pass, reason, err := e.judgeCandidate(context.Background(), "foo", llm.NewClient(srv.URL, "test-key"), "judge", "original body", "candidate body", stats, prov)
		if err != nil {
			t.Fatalf("judgeCandidate: %v", err)
		}
		if !pass {
			t.Fatalf("consistent judge must pass, got reject: %s", reason)
		}
		if calls.Load() != 2 {
			t.Fatalf("want 2 judge calls (forward+swap), got %d", calls.Load())
		}
		if prov.JudgeSwapConsistent == nil || !*prov.JudgeSwapConsistent {
			t.Fatalf("provenance must record a consistent swap probe, got %v", prov.JudgeSwapConsistent)
		}
	})

	t.Run("order-inconsistent judge is refused fail-closed", func(t *testing.T) {
		var calls atomic.Int32
		srv := newScriptedJudge(t, []string{
			judgeVerdictPayload(t, true, 70, 90, "forward improvement"),
			judgeVerdictPayload(t, true, 70, 90, "reversed pair also 'improves'"),
		}, &calls)
		defer srv.Close()

		e := newSwapTestEvolver()
		prov := &evolveProvenance{}
		pass, reason, err := e.judgeCandidate(context.Background(), "foo", llm.NewClient(srv.URL, "test-key"), "judge", "original body", "candidate body", stats, prov)
		if err != nil {
			t.Fatalf("judgeCandidate: %v", err)
		}
		if pass {
			t.Fatal("order-inconsistent judge must not pass")
		}
		if !strings.Contains(reason, "order-swap inconsistency") {
			t.Fatalf("reject reason must name the swap inconsistency, got: %s", reason)
		}
		if prov.JudgeSwapConsistent == nil || *prov.JudgeSwapConsistent {
			t.Fatalf("provenance must record the inconsistent swap probe, got %v", prov.JudgeSwapConsistent)
		}
	})

	t.Run("swap probe failure is a fail-closed error", func(t *testing.T) {
		var calls atomic.Int32
		srv := newScriptedJudge(t, []string{
			judgeVerdictPayload(t, true, 70, 90, "forward improvement"),
			"", // empty completion → judge: empty verdict
		}, &calls)
		defer srv.Close()

		e := newSwapTestEvolver()
		_, _, err := e.judgeCandidate(context.Background(), "foo", llm.NewClient(srv.URL, "test-key"), "judge", "original body", "candidate body", stats, nil)
		if err == nil || !strings.Contains(err.Error(), "judge swap probe") {
			t.Fatalf("swap probe failure must surface as an error, got: %v", err)
		}
	})

	t.Run("kill switch skips the probe", func(t *testing.T) {
		t.Setenv("DENEB_JUDGE_SWAP_CHECK", "0")
		var calls atomic.Int32
		srv := newScriptedJudge(t, []string{
			judgeVerdictPayload(t, true, 70, 90, "forward improvement"),
		}, &calls)
		defer srv.Close()

		e := newSwapTestEvolver()
		prov := &evolveProvenance{}
		pass, reason, err := e.judgeCandidate(context.Background(), "foo", llm.NewClient(srv.URL, "test-key"), "judge", "original body", "candidate body", stats, prov)
		if err != nil || !pass {
			t.Fatalf("disabled probe must keep the forward verdict, got pass=%v reason=%q err=%v", pass, reason, err)
		}
		if calls.Load() != 1 {
			t.Fatalf("disabled probe must not issue a second call, got %d", calls.Load())
		}
		if prov.JudgeSwapConsistent != nil {
			t.Fatalf("disabled probe must leave provenance unset, got %v", *prov.JudgeSwapConsistent)
		}
	})

	t.Run("forward reject never reaches the probe", func(t *testing.T) {
		var calls atomic.Int32
		srv := newScriptedJudge(t, []string{
			judgeVerdictPayload(t, false, 80, 70, "not an improvement"),
		}, &calls)
		defer srv.Close()

		e := newSwapTestEvolver()
		pass, _, err := e.judgeCandidate(context.Background(), "foo", llm.NewClient(srv.URL, "test-key"), "judge", "original body", "candidate body", stats, nil)
		if err != nil || pass {
			t.Fatalf("forward reject expected, got pass=%v err=%v", pass, err)
		}
		if calls.Load() != 1 {
			t.Fatalf("forward reject must not issue a swap call, got %d", calls.Load())
		}
	})
}

// TestJudgeSwapInconsistent pins the pure decision rule: only a swapped
// verdict that itself clears the strict-improvement rule marks the judge
// order-inconsistent.
func TestJudgeSwapInconsistent(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name    string
		swapped judgeVerdict
		covered bool
		want    bool
	}{
		{"swapped accept at margin", judgeVerdict{Pass: true, OriginalScore: f(70), CandidateScore: f(90)}, false, true},
		{"swapped explicit reject", judgeVerdict{Pass: false, OriginalScore: f(90), CandidateScore: f(70)}, false, false},
		{"swapped pass without scores fails closed as reject", judgeVerdict{Pass: true}, false, false},
		{"swapped pass below margin", judgeVerdict{Pass: true, OriginalScore: f(70), CandidateScore: f(71)}, false, false},
		{"covered margin applies", judgeVerdict{Pass: true, OriginalScore: f(70), CandidateScore: f(71.5)}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := judgeSwapInconsistent(tc.swapped, tc.covered); got != tc.want {
				t.Fatalf("judgeSwapInconsistent(%+v, covered=%v) = %v, want %v", tc.swapped, tc.covered, got, tc.want)
			}
		})
	}
}
