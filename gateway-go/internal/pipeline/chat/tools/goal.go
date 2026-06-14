// goal.go — the `goal` agent tool: lets the model set and manage a standing
// goal (Ralph loop) that Deneb keeps advancing across turns while the user is
// idle. The goal state + idempotency ledger live in goals.Store and the loop is
// driven by the server's goalTask; this tool is the agent's surface onto it.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// ToolGoal returns the `goal` tool. It resolves the store at call time
// (goals.Default, installed at server startup) and operates on the current
// session, so the agent can set/extend/inspect a standing goal mid-turn.
func ToolGoal() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		store := goals.Default()
		if store == nil {
			return "표준 목표 기능이 비활성화되어 있습니다.", nil
		}
		sessionKey := toolctx.SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "", fmt.Errorf("goal: 현재 세션을 확인할 수 없습니다")
		}
		var p struct {
			Action   string `json:"action"`
			Goal     string `json:"goal"`
			Subgoal  string `json:"subgoal"`
			MaxTurns int    `json:"max_turns"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("goal: 입력 파싱 실패: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(p.Action)) {
		case "set":
			goal := strings.TrimSpace(p.Goal)
			if goal == "" {
				return "", fmt.Errorf("goal: action=set 에는 goal 텍스트가 필요합니다")
			}
			st := store.Set(sessionKey, goal, p.MaxTurns)
			return fmt.Sprintf("🎯 표준 목표 설정 완료 (최대 %d단계). 사용자가 자리를 비우면 자동으로 한 단계씩 진행합니다.\n목표: %s", st.MaxTurns, st.Goal), nil

		case "subgoal":
			sub := strings.TrimSpace(p.Subgoal)
			if sub == "" {
				return "", fmt.Errorf("goal: action=subgoal 에는 subgoal 텍스트가 필요합니다")
			}
			st := store.AddSubgoal(sessionKey, sub)
			if st == nil {
				return "진행 중인 표준 목표가 없습니다. 먼저 action=set 으로 목표를 설정하세요.", nil
			}
			return fmt.Sprintf("서브골 추가됨 (총 %d개). 완료 판정 시 각 항목의 구체적 근거를 요구합니다.", len(st.Subgoals)), nil

		case "status", "":
			return store.Get(sessionKey).Summary(), nil

		case "pause":
			if store.Pause(sessionKey, "agent-paused") == nil {
				return "진행 중인 표준 목표가 없습니다.", nil
			}
			return "표준 목표를 일시중지했습니다.", nil

		case "resume":
			st := store.Resume(sessionKey)
			if st == nil || st.Status != goals.StatusActive {
				return "재개할 일시중지된 목표가 없습니다.", nil
			}
			return "표준 목표를 재개했습니다 (예산 초기화).", nil

		case "stop", "clear", "done":
			if store.Clear(sessionKey) == nil {
				return "진행 중인 표준 목표가 없습니다.", nil
			}
			return "표준 목표를 중단했습니다.", nil

		default:
			return fmt.Sprintf("알 수 없는 goal 액션: %q. 지원: set, subgoal, status, pause, resume, stop", p.Action), nil
		}
	}
}
