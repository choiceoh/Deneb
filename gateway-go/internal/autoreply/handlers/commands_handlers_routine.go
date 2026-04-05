// commands_handlers_routine.go — Routine shortcut handlers.
// These commands collect data directly (bypassing LLM tool-call decisions)
// then pass the pre-collected data to the agent for formatting only.
package handlers

import (
	"context"
	"fmt"
	"time"
)

// handleMorningCommand collects morning letter data directly (weather,
// exchange, copper, calendar, email) then passes the pre-collected JSON
// to the agent for formatting per SKILL.md rules.
//
// Flow: /morning → direct data collection → agent formats → send.
// The LLM never decides whether to call a tool — data is already collected.
func handleMorningCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Deps == nil || ctx.Deps.MorningLetterDataFn == nil {
		return &CommandResult{
			Reply:     "⚠️ 모닝레터 서비스를 사용할 수 없습니다.",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	execCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := ctx.Deps.MorningLetterDataFn(execCtx)
	if err != nil {
		return &CommandResult{
			Reply:     "⚠️ 모닝레터 데이터 수집 실패: " + err.Error(),
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	// Inject pre-collected data into the agent message.
	// The agent receives structured JSON + explicit formatting instructions,
	// so it only needs to format — no tool-call decision needed.
	if ctx.Msg != nil {
		ctx.Msg.BodyForAgent = fmt.Sprintf(
			"[모닝레터 데이터 수집 완료 — morning_letter 도구를 호출하지 마세요]\n\n%s\n\n"+
				"위 데이터를 skills/morning-letter/SKILL.md의 2단계(레터 조율 및 작성) 절차에 따라 "+
				"모닝레터로 포맷하세요. 도구 호출 없이 바로 최종 레터만 출력하세요.",
			data,
		)
	}

	return &CommandResult{SkipAgent: false}, nil
}
