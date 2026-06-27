package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/goals"
)

// GoalGlanceFunc returns a compact, pre-formatted snapshot of the session's
// active standing goal for the dynamic system-prompt block, or "" when there is
// no active goal. It mirrors CalendarGlanceFunc: ambient background context
// injected into the per-request (uncached) block, so it never perturbs the
// static prompt-cache prefix.
type GoalGlanceFunc func(ctx context.Context, sessionKey string) string

// NewGoalGlanceFunc builds the ambient goal glance from the process goal store
// (goals.Default(), installed at startup). The store is read lazily per turn so
// wiring order does not matter, and a nil store (goals not wired) yields "".
//
// This closes the read-side loop on goals.Store: until now the active goal was
// only consumed by the autonomous goal-driver task, so a normal chat turn in a
// session with a standing goal had no awareness of it. Now the agent can answer
// "어떻게 돼가" without a tool round-trip and notice when a fresh request
// conflicts with the goal it is supposed to be driving.
func NewGoalGlanceFunc() GoalGlanceFunc {
	return func(_ context.Context, sessionKey string) string {
		store := goals.Default()
		if store == nil || strings.TrimSpace(sessionKey) == "" {
			return ""
		}
		st := store.Get(sessionKey)
		if st == nil || !st.Active() {
			return ""
		}
		return formatGoalGlance(st)
	}
}

// formatGoalGlance renders an active goal as a short Korean glance. Kept pure
// (no store, no clock) so it is unit-testable in isolation.
func formatGoalGlance(st *goals.State) string {
	goal := strings.TrimSpace(st.Goal)
	if goal == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("- 목표: ")
	b.WriteString(goal)
	b.WriteString("\n")
	if st.MaxTurns > 0 {
		fmt.Fprintf(&b, "- 진행: %d/%d턴 사용", st.TurnsUsed, st.MaxTurns)
	} else {
		fmt.Fprintf(&b, "- 진행: %d턴 사용", st.TurnsUsed)
	}
	if r := strings.TrimSpace(st.LastReason); r != "" {
		b.WriteString(" · 최근 판정: ")
		b.WriteString(r)
	}
	b.WriteString("\n")
	if len(st.Subgoals) > 0 {
		b.WriteString("- 완료 기준: ")
		b.WriteString(strings.Join(st.Subgoals, "; "))
		b.WriteString("\n")
	}
	return b.String()
}
