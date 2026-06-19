package genesis

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// toolPlanCompletion wraps a tool-call plan body in an OpenAI chat-completion
// envelope so the fake executor server can return it verbatim.
func toolPlanCompletion(t *testing.T, content string) string {
	t.Helper()
	b, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	return `{"choices":[{"message":{"content":` + string(b) + `}}]}`
}

// newPlanExecutorServer returns a fake executor whose emitted tool-call plan is
// chosen by a marker in the request (the skill body). One server can thus return
// a correct plan for the "original" body and a degraded plan for the "candidate"
// body, letting a behavioral regression be simulated without a GPU.
func newPlanExecutorServer(t *testing.T) *httptest.Server {
	t.Helper()
	const fullPlan = `{"tool_calls":[{"name":"exec","args":"python3 topsolar.py dashboard"}]}`
	const emptyPlan = `{"tool_calls":[]}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		plan := fullPlan
		if strings.Contains(string(body), "PLAN_EMPTY") {
			plan = emptyPlan
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, toolPlanCompletion(t, plan))
	}))
}

// behaviorTestCase is a replay case asserting the skill drives an exec call that
// runs `topsolar.py dashboard` — the proven tool-call behavior a rewrite must
// not regress.
func behaviorTestCase() SkillValidationCaseRecord {
	return SkillValidationCaseRecord{
		SkillName: "topsolar-db",
		Source:    "test-manual",
		Replay: SkillReplayCaseRecord{
			Input: "프로젝트 현황 알려줘",
			ExpectedToolCalls: []SkillReplayToolCallRecord{
				{Name: "exec", InputIncludes: []string{"topsolar.py", "dashboard"}},
			},
		},
	}
}

func newBehaviorEngine(t *testing.T, serverURL string) (*SkillValidationEngine, *Tracker) {
	t.Helper()
	tr := newTestTracker(t)
	engine := NewSkillValidationEngine(tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if serverURL != "" {
		// Disable client retries so the executor-error case fails fast instead of
		// burning ~70s on exponential backoff against the 500 server.
		engine.SetExecutor(llm.NewClient(serverURL, "test-key", llm.WithRetry(0, time.Millisecond, time.Millisecond)), "test-model")
	}
	return engine, tr
}

// TestEvaluateBehavior_RejectsToolCallRegression is the core guarantee: a
// candidate whose simulated plan drops the proven exec/topsolar.py/dashboard
// call is rejected even though it parses fine. This is the false-accept the
// substring-only gate could not catch.
func TestEvaluateBehavior_RejectsToolCallRegression(t *testing.T) {
	srv := newPlanExecutorServer(t)
	defer srv.Close()
	engine, tr := newBehaviorEngine(t, srv.URL)
	if err := tr.RecordSkillValidationCase(behaviorTestCase()); err != nil {
		t.Fatalf("record case: %v", err)
	}
	orig := "# Skill\n\nRun `python3 topsolar.py dashboard`. PLAN_FULL"
	cand := "# Skill\n\nJust describe the project status. PLAN_EMPTY"
	res, err := engine.EvaluateBehavior(context.Background(), "topsolar-db", orig, cand)
	if err != nil {
		t.Fatalf("EvaluateBehavior: %v", err)
	}
	if !res.Evaluated {
		t.Fatalf("expected Evaluated=true, got %+v", res)
	}
	if res.Pass {
		t.Fatalf("expected behavioral regression to fail the gate, got pass: %+v", res)
	}
}

// TestEvaluateBehavior_PreservedToolCallPasses confirms the gate is a
// regression-only safety net: a rewrite that keeps the proven tool plan passes
// (it must not block non-behavioral improvements).
func TestEvaluateBehavior_PreservedToolCallPasses(t *testing.T) {
	srv := newPlanExecutorServer(t)
	defer srv.Close()
	engine, tr := newBehaviorEngine(t, srv.URL)
	if err := tr.RecordSkillValidationCase(behaviorTestCase()); err != nil {
		t.Fatalf("record case: %v", err)
	}
	orig := "# Skill\n\nRun `python3 topsolar.py dashboard`. PLAN_FULL (v1)"
	cand := "# Skill\n\nFirst note the caveat, then run `python3 topsolar.py dashboard`. PLAN_FULL (v2)"
	res, err := engine.EvaluateBehavior(context.Background(), "topsolar-db", orig, cand)
	if err != nil {
		t.Fatalf("EvaluateBehavior: %v", err)
	}
	if !res.Evaluated {
		t.Fatalf("expected Evaluated=true, got %+v", res)
	}
	if !res.Pass {
		t.Fatalf("expected preserved behavior to pass, got: %+v", res)
	}
}

// TestEvaluateBehavior_NoExecutorFailsOpen: with no executor wired the gate is
// inert (Evaluated=false → caller treats as pass).
func TestEvaluateBehavior_NoExecutorFailsOpen(t *testing.T) {
	engine, tr := newBehaviorEngine(t, "")
	if err := tr.RecordSkillValidationCase(behaviorTestCase()); err != nil {
		t.Fatalf("record case: %v", err)
	}
	res, err := engine.EvaluateBehavior(context.Background(), "topsolar-db", "a PLAN_FULL", "b PLAN_EMPTY")
	if err != nil {
		t.Fatalf("EvaluateBehavior: %v", err)
	}
	if res.Evaluated {
		t.Fatalf("expected fail-open (Evaluated=false) with no executor, got %+v", res)
	}
}

// TestEvaluateBehavior_NoCasesFailsOpen: an executor with no behavior-evaluable
// cases must not block (the starved-corpus reality today).
func TestEvaluateBehavior_NoCasesFailsOpen(t *testing.T) {
	srv := newPlanExecutorServer(t)
	defer srv.Close()
	engine, _ := newBehaviorEngine(t, srv.URL)
	res, err := engine.EvaluateBehavior(context.Background(), "topsolar-db", "a PLAN_FULL", "b PLAN_EMPTY")
	if err != nil {
		t.Fatalf("EvaluateBehavior: %v", err)
	}
	if res.Evaluated {
		t.Fatalf("expected fail-open (Evaluated=false) with no cases, got %+v", res)
	}
}

// TestEvaluateBehavior_ExecutorErrorFailsOpen: a flaky executor (HTTP 500) must
// yield an un-evaluated pass with a nil error, never a block.
func TestEvaluateBehavior_ExecutorErrorFailsOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	engine, tr := newBehaviorEngine(t, srv.URL)
	if err := tr.RecordSkillValidationCase(behaviorTestCase()); err != nil {
		t.Fatalf("record case: %v", err)
	}
	res, err := engine.EvaluateBehavior(context.Background(), "topsolar-db", "a PLAN_FULL", "b PLAN_EMPTY")
	if err != nil {
		t.Fatalf("expected fail-open with nil error on executor failure, got err=%v", err)
	}
	if res.Evaluated {
		t.Fatalf("expected fail-open (Evaluated=false) on executor error, got %+v", res)
	}
}
