package autonomous

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// autonomousSessionKey is the fixed session key for autonomous cycles.
// Using a fixed key allows transcript accumulation across cycles for context continuity.
const autonomousSessionKey = "autonomous:cycle"

// buildDecisionPrompt constructs the LLM prompt for a decision cycle.
// Includes active goals, last cycle summary, current time, and structured output instructions.
func buildDecisionPrompt(goals []Goal, lastCycle *CycleState) string {
	var b strings.Builder

	// Header with timestamp for time-aware decisions.
	now := time.Now()
	b.WriteString("# 자율 실행 모드 — Decision Cycle\n\n")
	b.WriteString(fmt.Sprintf("현재 시각: %s\n\n", now.Format("2006-01-02 15:04 (Mon)")))

	// Last cycle summary for continuity.
	if lastCycle != nil && lastCycle.LastSummary != "" {
		b.WriteString("## 이전 사이클 결과\n\n")
		b.WriteString(lastCycle.LastSummary)
		b.WriteString("\n\n")
	}

	// Active goals.
	b.WriteString("## 활성 목표\n\n")
	if len(goals) == 0 {
		b.WriteString("활성 목표가 없습니다.\n\n")
		return b.String()
	}
	for i, g := range goals {
		b.WriteString(fmt.Sprintf("%d. **[%s]** %s (id: `%s`)\n",
			i+1, priorityLabel(g.Priority), g.Description, g.ID))
		if g.LastNote != "" {
			b.WriteString(fmt.Sprintf("   이전 진행: %s\n", g.LastNote))
		}
		age := time.Since(time.UnixMilli(g.CreatedAtMs))
		if age > 24*time.Hour {
			b.WriteString(fmt.Sprintf("   생성: %d일 전\n", int(age.Hours()/24)))
		}
	}

	// Instructions.
	b.WriteString("\n## 실행 지침\n\n")
	b.WriteString("1. 우선순위가 가장 높은 목표 중 지금 실행 가능한 것을 하나 선택하라.\n")
	b.WriteString("2. 사용 가능한 도구(exec, read, write, web_fetch 등)를 적극 활용해 실제 진행을 만들어라.\n")
	b.WriteString("3. 한 사이클에 하나의 목표에만 집중하라.\n")
	b.WriteString("4. 실행이 불가능하거나 외부 의존성으로 막힌 목표는 \"paused\"로 전환하고 이유를 설명하라.\n")
	b.WriteString("5. 목표가 완전히 달성되면 \"completed\"로 전환하라.\n")
	b.WriteString("6. 도구 실행 결과가 예상과 다르면 한 단계 더 시도하되, 같은 실패가 반복되면 paused로 전환하라.\n\n")

	// Output format.
	b.WriteString("## 출력 형식 (필수)\n\n")
	b.WriteString("작업 완료 후 반드시 응답 끝에 아래 형식의 목표 업데이트 블록을 포함하라:\n\n")
	b.WriteString("```goal_update\n")
	b.WriteString(`{"goalUpdates": [{"id": "GOAL_ID", "status": "active", "note": "이번 사이클에서 수행한 내용"}]}`)
	b.WriteString("\n```\n\n")
	b.WriteString("- status 값: `active` (진행 중), `completed` (완료), `paused` (중단)\n")
	b.WriteString("- note: 한국어로 간결하게 작성 (50자 이내 권장)\n")
	b.WriteString("- goal_update 블록이 없으면 진행 기록이 남지 않으니 반드시 포함할 것\n")

	return b.String()
}

func priorityLabel(p string) string {
	switch p {
	case PriorityHigh:
		return "높음"
	case PriorityMedium:
		return "보통"
	case PriorityLow:
		return "낮음"
	default:
		return p
	}
}

// goalUpdateBlockRegex matches a fenced ```goal_update ... ``` block.
var goalUpdateBlockRegex = regexp.MustCompile("(?s)```goal_update\\s*\n(.+?)\\s*```")

// goalUpdatePayload is the JSON envelope in a goal_update block.
type goalUpdatePayload struct {
	GoalUpdates []GoalUpdate `json:"goalUpdates"`
}

// parseGoalUpdates extracts GoalUpdate entries from the agent's output.
// If the structured block is missing or malformed, falls back to extracting
// a summary from the last paragraph of the output.
func parseGoalUpdates(output string, activeGoalIDs []string) []GoalUpdate {
	// Try structured parsing first.
	matches := goalUpdateBlockRegex.FindStringSubmatch(output)
	if len(matches) >= 2 {
		var payload goalUpdatePayload
		if err := json.Unmarshal([]byte(matches[1]), &payload); err == nil {
			valid := validateUpdates(payload.GoalUpdates)
			if len(valid) > 0 {
				return valid
			}
		}
	}

	// Fallback: LLM didn't produce structured output.
	// Save the tail of the output as a note on the first active goal
	// so progress isn't completely lost.
	if len(activeGoalIDs) > 0 && output != "" {
		note := extractFallbackNote(output)
		if note != "" {
			return []GoalUpdate{{
				ID:     activeGoalIDs[0],
				Status: StatusActive,
				Note:   note,
			}}
		}
	}

	return nil
}

func validateUpdates(updates []GoalUpdate) []GoalUpdate {
	valid := make([]GoalUpdate, 0, len(updates))
	for _, u := range updates {
		if u.ID == "" {
			continue
		}
		switch u.Status {
		case StatusActive, StatusCompleted, StatusPaused:
			// ok
		default:
			u.Status = StatusActive
		}
		// Truncate excessively long notes.
		if len(u.Note) > 500 {
			u.Note = u.Note[:497] + "..."
		}
		valid = append(valid, u)
	}
	return valid
}

// extractFallbackNote extracts the last meaningful paragraph from output
// as a fallback progress note (max 200 chars).
func extractFallbackNote(output string) string {
	// Take the last non-empty paragraph.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var lastPara string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "```") {
			if lastPara != "" {
				break
			}
			continue
		}
		if lastPara == "" {
			lastPara = line
		} else {
			lastPara = line + " " + lastPara
		}
		// Don't collect too much.
		if len(lastPara) > 200 {
			break
		}
	}
	if len(lastPara) > 200 {
		lastPara = lastPara[:197] + "..."
	}
	return lastPara
}
