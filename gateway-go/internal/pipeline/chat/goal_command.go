// goal_command.go — the /goal slash command surface for the Ralph loop. It
// only mutates goal STATE (goals.Store); the loop that advances goals lives in
// the server layer (goal_task.go). Subcommands: bare "/goal <text>" sets a new
// standing goal, "status" reports, "pause"/"resume"/"stop" manage it.
package chat

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/goals"
)

// handleGoalCommand processes "/goal …" synchronously against the process goal
// store and replies via respond. Dormant-safe: if no store is wired (goals not
// enabled) it says so rather than panicking.
func (h *Handler) handleGoalCommand(sessionKey, args string, respond func(text string)) {
	store := goals.Default()
	if store == nil {
		respond("표준 목표 기능이 아직 활성화되지 않았습니다.")
		return
	}

	sub, _ := splitFirstWord(args)
	switch strings.ToLower(sub) {
	case "", "status", "상태":
		respond(formatGoalStatus(store.Get(sessionKey)))

	case "stop", "clear", "done", "중단", "정지":
		st := store.Clear(sessionKey)
		if st == nil {
			respond("진행 중인 표준 목표가 없습니다.")
			return
		}
		respond("표준 목표를 중단했습니다: " + st.Goal)

	case "pause", "일시중지":
		st := store.Pause(sessionKey, "user-paused")
		if st == nil {
			respond("진행 중인 표준 목표가 없습니다.")
			return
		}
		respond("표준 목표를 일시중지했습니다. 이어서 진행하려면 `/goal resume`.")

	case "resume", "재개":
		st := store.Resume(sessionKey)
		if st == nil || st.Status != goals.StatusActive {
			respond("재개할 일시중지된 목표가 없습니다.")
			return
		}
		respond("표준 목표를 재개했습니다 (예산 초기화): " + st.Goal)

	default:
		// Anything else is treated as a new goal: "/goal <text>".
		goalText := strings.TrimSpace(args)
		if goalText == "" {
			respond("목표 내용을 입력하세요. 예: `/goal 탑솔라 6월 견적 정리해서 초안까지`")
			return
		}
		st := store.Set(sessionKey, goalText, 0)
		respond(fmt.Sprintf(
			"🎯 표준 목표를 설정했습니다 (최대 %d단계). 자리를 비우면(유휴) 자동으로 한 단계씩 진행합니다.\n목표: %s\n상태 확인 `/goal status` · 중단 `/goal stop`",
			st.MaxTurns, st.Goal,
		))
	}
}

// formatGoalStatus renders a session's standing goal for /goal status.
func formatGoalStatus(st *goals.State) string {
	if st == nil {
		return "진행 중인 표준 목표가 없습니다. 설정하려면 `/goal <목표>`."
	}
	return st.Summary()
}

// splitFirstWord splits s into its first whitespace-delimited word and the rest.
func splitFirstWord(s string) (first, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}
