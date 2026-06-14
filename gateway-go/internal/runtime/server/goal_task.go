// goal_task.go — the Ralph loop driver. A PeriodicTask that advances active
// standing goals (set via /goal) one run at a time while the user is idle,
// judges completion with the cheap lightweight model, and enforces a
// per-goal idempotency ledger so a re-driven run never repeats a destructive
// action (double-send, re-exec).
//
// It reuses the same server-driven-run machinery as heartbeat: SendSync with
// AutoDeliveredOutput so each step's reply is delivered to the user by the
// run-completion relay and the agent does not self-send via the message tool.
// Goal state + ledger live in goals.Store (goals.json), so a standing goal
// survives the SIGUSR1 deploy restarts. Inspired by Hermes Agent's /goal loop.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*goalTask)(nil)

const (
	// goalTickInterval is how often the loop checks for an active goal to
	// advance. One step per tick paces progress (a 20-step goal takes ~40 min
	// of idle time) and bounds tick wall-clock to one run.
	goalTickInterval = 2 * time.Minute
	// goalIdleThreshold mirrors heartbeat: skip a step while the user is
	// mid-conversation so the loop never races a turn in flight or interrupts.
	goalIdleThreshold = 1 * time.Minute
	// goalStepTimeout bounds one goal run (same as the interactive turn deadline).
	goalStepTimeout = 5 * time.Minute
	// goalJudgeTimeout bounds the cheap completion judge.
	goalJudgeTimeout = 45 * time.Second
)

// goalTask implements autonomous.PeriodicTask: it advances standing goals.
type goalTask struct {
	chatHandler *chat.Handler
	store       *goals.Store
	activity    *monitoring.ActivityTracker
	logger      *slog.Logger

	// notify delivers a short terminal notice (done / paused) to the goal's
	// session. nil-safe: when unwired, transitions are logged only and the user
	// learns the outcome from the step output or /goal status.
	notify func(ctx context.Context, sessionKey, msg string) error
}

func (t *goalTask) Name() string            { return "goal-loop" }
func (t *goalTask) Interval() time.Duration { return goalTickInterval }

func (t *goalTask) Run(ctx context.Context) error {
	if t.chatHandler == nil || t.store == nil {
		return nil
	}
	// User-active gate: do not advance a goal while the user is actively using
	// the system (mirrors heartbeat). Their messages preempt the loop naturally.
	if t.userActive() {
		t.logger.Debug("goal-loop: skipped, user active")
		return nil
	}
	active := t.store.ListActive()
	if len(active) == 0 {
		return nil
	}
	// Advance at most one goal per tick to pace progress and bound tick time.
	// Single-user deployments have 0-1 standing goals in practice.
	return t.driveOne(ctx, active[0])
}

// userActive reports whether the user interacted within the idle threshold.
func (t *goalTask) userActive() bool {
	if t.activity == nil {
		return false
	}
	idle := time.Duration(time.Now().UnixMilli()-t.activity.LastActivityAt()) * time.Millisecond
	return idle < goalIdleThreshold
}

// driveOne advances a single goal by one run: re-inject the continuation, run
// the agent under the idempotency guard, judge completion, and book the result.
func (t *goalTask) driveOne(ctx context.Context, g *goals.State) error {
	sessionKey := g.SessionKey
	if sessionKey == "" {
		sessionKey = "client:main"
	}

	// Idempotency guard: block destructive actions already committed by a prior
	// run of this goal, and record newly-committed ones (correlated by tool-call
	// ID so OnToolResult — which lacks the input — can look up the key the
	// before-hook computed). Tool calls within a run may execute in parallel, so
	// the maps are mutex-guarded.
	var mu sync.Mutex
	pending := make(map[string]string) // toolUseID -> ledger key (destructive, allowed)
	var committed []string

	before := func(name, id string, input []byte) (bool, string) {
		key, destructive := goals.DestructiveActionKey(name, input)
		if !destructive {
			return false, ""
		}
		if t.store.SeenAction(sessionKey, key) {
			return true, "이미 이 목표의 이전 단계에서 실행한 작업이라 중복 실행을 막았습니다. 다음 단계로 진행하거나, 추가 작업이 없으면 목표 완료를 선언하세요."
		}
		mu.Lock()
		pending[id] = key
		mu.Unlock()
		return false, ""
	}
	onResult := func(_ /*name*/, id, _ /*result*/ string, isErr bool) {
		mu.Lock()
		key := pending[id]
		delete(pending, id)
		if key != "" && !isErr {
			committed = append(committed, key)
		}
		mu.Unlock()
	}

	opts := &chat.SyncOptions{
		AutoDeliveredOutput: true, // run-completion relay delivers; agent must not self-send
		BeforeToolCall:      before,
		OnToolResult:        onResult,
	}

	runCtx, cancel := context.WithTimeout(ctx, goalStepTimeout)
	res, err := t.chatHandler.SendSync(runCtx, sessionKey, composeGoalContinuation(g.Goal, g.Subgoals), "", opts)
	cancel()

	// Commit successfully-executed destructive actions regardless of the judge —
	// they happened, so future runs must not repeat them. (SendSync is
	// synchronous, so all onResult calls have fired by now.)
	t.store.CommitActions(sessionKey, committed)

	if err != nil {
		// Fail-soft: count the run so a persistently failing goal still drains
		// its budget into a pause instead of looping forever. Not propagated as
		// a task error (that only bumps the error counter).
		t.logger.Warn("goal-loop: step failed", "session", sessionKey, "error", err)
		t.store.RecordRun(sessionKey, "continue", "run error: "+truncateRunes(err.Error(), 200), false)
		return nil
	}

	verdict, reason, parseFailed := t.judge(ctx, g.Goal, g.Subgoals, res.BestText())
	updated := t.store.RecordRun(sessionKey, verdict, reason, parseFailed)
	t.logger.Info("goal-loop: step done",
		"session", sessionKey, "verdict", verdict,
		"turnsUsed", updated.TurnsUsed, "maxTurns", updated.MaxTurns, "status", updated.Status)
	t.surfaceTransition(ctx, sessionKey, updated)
	return nil
}

// surfaceTransition tells the user when a goal reaches a terminal/paused state
// that the step output alone would not convey (budget pause especially). The
// "done" case is usually already implied by the final step's reply, but a
// one-line confirmation is still sent for clarity.
func (t *goalTask) surfaceTransition(ctx context.Context, sessionKey string, st *goals.State) {
	if st == nil || t.notify == nil {
		return
	}
	var msg string
	switch st.Status {
	case goals.StatusDone:
		msg = "✅ 표준 목표 완료: " + st.Goal
		if st.LastReason != "" {
			msg += "\n— " + st.LastReason
		}
	case goals.StatusPaused:
		msg = "⏸️ 표준 목표 일시중지: " + st.Goal
		if st.PausedReason != "" {
			msg += "\n— " + st.PausedReason
		}
	default:
		return // still active — the step output already informed the user
	}
	if err := t.notify(ctx, sessionKey, msg); err != nil {
		t.logger.Warn("goal-loop: transition notice failed", "session", sessionKey, "error", err)
	}
}

// ── Completion judge ────────────────────────────────────────────────────────

const goalJudgeSystem = `You are a strict judge deciding whether an autonomous agent has achieved a user's stated goal. You get the goal and the agent's most recent response. Decide ONLY from that response.

The goal is DONE when:
- the response confirms the goal was completed, OR
- the response clearly shows the final deliverable was produced, OR
- the response explains the goal is blocked / unachievable / needs user input (treat as DONE, with the reason describing the block).

Otherwise it is NOT done — CONTINUE.

Reply with ONE JSON object on a single line and nothing else:
{"done": <true|false>, "reason": "<one short sentence>"}`

// goalJudgeSystemWithSubgoals is the stricter judge used when the goal carries
// explicit completion criteria: it demands specific per-criterion evidence and
// rejects generic "all done" phrasing.
const goalJudgeSystemWithSubgoals = `You are a strict judge deciding whether an autonomous agent achieved a user's goal AND every listed completion criterion. You get the goal (with numbered criteria) and the agent's most recent response. Decide ONLY from that response.

The goal is DONE only when the response shows SPECIFIC evidence satisfying EACH numbered criterion — OR explains the goal is blocked / needs user input (treat that as DONE). Generic claims like "everything is finished" without per-criterion evidence are NOT done — CONTINUE.

Reply with ONE JSON object on a single line and nothing else:
{"done": <true|false>, "reason": "<one short sentence>"}`

const goalJudgeUserTmpl = "Goal:\n%s\n\nAgent's most recent response:\n%s\n\nIs the goal satisfied?"

// judge asks the lightweight model whether the goal is satisfied. It is
// FAIL-OPEN: any judge error or empty answer returns "continue" (the turn
// budget is the hard backstop). A non-parseable judge output returns
// parseFailed=true so the store's consecutive-parse-failure cap can auto-pause.
func (t *goalTask) judge(ctx context.Context, goal string, subgoals []string, answer string) (verdict, reason string, parseFailed bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "continue", "빈 응답", false // fail-open, not a parse failure
	}
	jctx, cancel := context.WithTimeout(ctx, goalJudgeTimeout)
	defer cancel()

	system := goalJudgeSystem
	goalBlock := truncateRunes(goal, 2000)
	if len(subgoals) > 0 {
		system = goalJudgeSystemWithSubgoals
		var b strings.Builder
		b.WriteString(goalBlock)
		b.WriteString("\n\n완료 기준(각 항목마다 응답에 구체적 근거가 있어야 DONE):")
		for i, sg := range subgoals {
			fmt.Fprintf(&b, "\n  %d. %s", i+1, sg)
		}
		goalBlock = b.String()
	}
	user := fmt.Sprintf(goalJudgeUserTmpl, goalBlock, truncateRunes(answer, 4000))
	out, err := pilot.CallLocalLLM(jctx, system, user, 512, map[string]any{"temperature": 0})
	if err != nil {
		return "continue", "judge 오류(계속): " + truncateRunes(err.Error(), 120), false // fail-open
	}
	done, jreason, ok := parseJudgeVerdict(out)
	if !ok {
		return "continue", "judge 출력 해석 실패", true
	}
	if done {
		return "done", jreason, false
	}
	return "continue", jreason, false
}

// parseJudgeVerdict extracts {"done":bool,"reason":string} from judge output,
// tolerating markdown fences and surrounding prose. ok=false when no JSON
// object with a "done" field is found.
func parseJudgeVerdict(out string) (done bool, reason string, ok bool) {
	s := strings.TrimSpace(out)
	// Strip a leading ```json / ``` fence if present.
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var v struct {
		Done   any    `json:"done"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return false, "", false
	}
	switch d := v.Done.(type) {
	case bool:
		return d, v.Reason, true
	case string:
		return strings.EqualFold(strings.TrimSpace(d), "true"), v.Reason, true
	default:
		return false, "", false
	}
}

// ── Continuation prompt ─────────────────────────────────────────────────────

const goalContinuationTmpl = `[표준 목표 진행 — 자동 단계]
목표: %s

이 목표를 향해 다음 구체적 단계 하나를 실행하세요.
- 목표가 완료되었다고 판단되면 명확히 "완료"라고 밝히고 멈추세요.
- 막혀서 사용자 입력이 필요하면 무엇이 필요한지 분명히 말하고 멈추세요.
- 이미 끝낸 작업(이전 단계에서 보낸 메일·변경 등)은 절대 반복하지 마세요. 같은 대화의 이전 단계 결과를 반영하세요.
- 보고할 새 진전이 없으면 본문에 정확히 ` + "`NO_REPLY`" + ` 한 단어만 출력하세요.`

func composeGoalContinuation(goal string, subgoals []string) string {
	out := fmt.Sprintf(goalContinuationTmpl, strings.TrimSpace(goal))
	if len(subgoals) > 0 {
		var b strings.Builder
		b.WriteString(out)
		b.WriteString("\n\n추가 완료 기준(아래를 모두 충족해야 목표 완료):")
		for i, sg := range subgoals {
			fmt.Fprintf(&b, "\n  %d. %s", i+1, sg)
		}
		out = b.String()
	}
	return out
}
