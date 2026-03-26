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
//
// Design principles:
//   - System prompt already provides tools, time, workspace, safety — don't repeat.
//   - This prompt is the "user message" in the autonomous session. It must clearly
//     establish the autonomous context so the agent doesn't think it's in a normal conversation.
//   - Guide strategic thinking, not just task selection.
//   - Warn about common autonomous execution anti-patterns.
//   - Make the output format robust against LLM formatting quirks.
func buildDecisionPrompt(goals []Goal, lastCycle *CycleState, recentlyChanged ...Goal) string {
	var b strings.Builder

	// ── Identity & context ──────────────────────────────────────────────
	b.WriteString("[자율 실행 사이클]\n\n")
	b.WriteString("너는 지금 자율 실행 모드로 동작 중이다. 사용자가 직접 메시지를 보낸 것이 아니라, 시스템 타이머가 이 사이클을 트리거했다. ")
	b.WriteString("사용자에게 질문하거나 확인을 요청할 수 없다 — 스스로 판단하고 실행해야 한다.\n\n")

	// ── Last cycle continuity ───────────────────────────────────────────
	if lastCycle != nil && lastCycle.LastSummary != "" {
		b.WriteString("### 이전 사이클\n")
		b.WriteString(lastCycle.LastSummary)
		b.WriteString("\n\n")
	}

	// ── Recently completed/paused for context ───────────────────────────
	if len(recentlyChanged) > 0 {
		b.WriteString("### 최근 변경된 목표\n")
		for _, g := range recentlyChanged {
			label := "✓ 완료"
			if g.Status == StatusPaused {
				label = "⏸ 중단"
			}
			b.WriteString(fmt.Sprintf("- %s: %s", label, g.Description))
			if g.LastNote != "" {
				b.WriteString(fmt.Sprintf(" — %s", g.LastNote))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// ── Active goals ────────────────────────────────────────────────────
	b.WriteString("### 활성 목표\n\n")
	if len(goals) == 0 {
		b.WriteString("활성 목표 없음. 이 사이클은 건너뛴다.\n")
		return b.String()
	}

	for i, g := range goals {
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, strings.ToUpper(g.Priority), g.Description))
		b.WriteString(fmt.Sprintf("   id: `%s`", g.ID))

		// Show cycle count and age for stuck detection.
		var meta []string
		if g.CycleCount > 0 {
			meta = append(meta, fmt.Sprintf("%d회 작업", g.CycleCount))
		}
		age := time.Since(time.UnixMilli(g.CreatedAtMs))
		if age > 24*time.Hour {
			meta = append(meta, fmt.Sprintf("%d일 경과", int(age.Hours()/24)))
		}
		if len(meta) > 0 {
			b.WriteString(fmt.Sprintf(" · %s", strings.Join(meta, ", ")))
		}
		b.WriteString("\n")

		if g.LastNote != "" {
			b.WriteString(fmt.Sprintf("   마지막 진행: %s\n", g.LastNote))
		}
		if g.PausedReason != "" && g.Status == StatusActive {
			// Was paused before, reactivated — show previous block reason.
			b.WriteString(fmt.Sprintf("   (이전 중단 이유: %s)\n", g.PausedReason))
		}
	}

	// ── Execution strategy ──────────────────────────────────────────────
	b.WriteString("\n### 실행 전략\n\n")
	b.WriteString("1. **목표 선택**: 우선순위가 가장 높고 지금 진행 가능한 목표 하나를 선택하라.\n")
	b.WriteString("2. **접근 방식 결정**:\n")
	b.WriteString("   - 처음 작업하는 목표 → 먼저 현재 상태를 파악하라 (파일 읽기, 명령 실행 등)\n")
	b.WriteString("   - 이전 진행이 있는 목표 → 마지막 진행 내용을 이어서 다음 단계를 실행하라\n")
	b.WriteString("   - 5회 이상 작업했는데 진전이 없는 목표 → paused로 전환하라\n")
	b.WriteString("3. **실행**: 도구를 사용해 실제로 변화를 만들어라. 계획만 세우지 말 것.\n")
	b.WriteString("4. **판단**:\n")
	b.WriteString("   - 완전히 달성됨 → completed\n")
	b.WriteString("   - 진행했지만 아직 미완 → active (note에 다음 단계 기록)\n")
	b.WriteString("   - 외부 의존성/권한 부족/불가능 → paused (note에 사유 기록)\n\n")

	// ── Anti-patterns ───────────────────────────────────────────────────
	b.WriteString("### 하지 말 것\n\n")
	b.WriteString("- 사용자에게 질문하거나 확인을 요청하지 마라 (자율 모드에서는 응답받을 수 없다)\n")
	b.WriteString("- 실제 실행 없이 \"계획을 세웠습니다\" 같은 보고만 하지 마라\n")
	b.WriteString("- 이미 완료된 작업을 다시 하지 마라 (이전 진행 내용을 확인하라)\n")
	b.WriteString("- 검증 없이 \"완료\"로 표시하지 마라 (결과를 확인한 후 completed로 전환하라)\n\n")

	// ── Output format ───────────────────────────────────────────────────
	b.WriteString("### 출력 형식\n\n")
	b.WriteString("작업 후 반드시 응답 마지막에 아래 블록을 포함하라:\n\n")
	b.WriteString("```goal_update\n")
	b.WriteString("{\"goalUpdates\": [{\"id\": \"ID\", \"status\": \"STATUS\", \"note\": \"NOTE\"}]}\n")
	b.WriteString("```\n\n")
	b.WriteString("- STATUS: `active` | `completed` | `paused`\n")
	b.WriteString("- NOTE: 한국어, 이번 사이클에서 한 일 또는 중단 사유. 다음 사이클의 나 자신이 읽는다.\n")
	b.WriteString("- 이 블록이 없으면 진행 기록이 유실된다.\n")

	return b.String()
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
		if len(lastPara) > 200 {
			break
		}
	}
	if len(lastPara) > 200 {
		lastPara = lastPara[:197] + "..."
	}
	return lastPara
}
