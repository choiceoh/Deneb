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
				"위 데이터를 skills/productivity/morning-letter/SKILL.md의 2단계(레터 조율 및 작성) 절차에 따라 "+
				"모닝레터로 포맷하세요. 도구 호출 없이 바로 최종 레터만 출력하세요.",
			data,
		)
	}

	return &CommandResult{SkipAgent: false}, nil
}

// handleWeeklyCommand builds the weekly business report (주간업무보고) directly:
// scans the project wiki, composes the form, and renders a PDF — with a text
// fallback when Chromium can't run under memory pressure. The agent then only
// delivers: it sends the PDF via send_file, or relays the text version.
//
// Flow: /weekly → direct build (collect + render) → agent delivers → done.
func handleWeeklyCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Deps == nil || ctx.Deps.WeeklyReportFn == nil {
		return &CommandResult{
			Reply:     "⚠️ 주간업무보고 서비스를 사용할 수 없습니다.",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	execCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pdfPath, text, rendered := ctx.Deps.WeeklyReportFn(execCtx)

	if ctx.Msg != nil {
		if rendered && pdfPath != "" {
			ctx.Msg.BodyForAgent = fmt.Sprintf(
				"[주간업무보고 PDF 생성 완료 — 위키 데이터 기반, 도구로 재수집하지 마세요]\n\n"+
					"PDF 경로: %s\n\n"+
					"이 PDF를 send_file 도구(type: document)로 사용자에게 보내세요. 그리고 소관별 "+
					"건수와 임박 마감 등 핵심을 2~3줄로 요약해 덧붙이세요. 내부 토큰 노출 금지.",
				pdfPath,
			)
		} else {
			ctx.Msg.BodyForAgent = fmt.Sprintf(
				"[주간업무보고 — PDF 렌더 불가(메모리 여유 부족), 텍스트로 전달]\n\n%s\n\n"+
					"위 보고서를 사용자에게 전달하세요. 규칙: ① 사실(프로젝트·용량·날짜·상태)은 "+
					"재작성·요약하지 말고 그대로 둔다. ② 보고서에 없는 항목(조직 구성·인선 등)을 "+
					"새로 추가하지 마라. ③ 보고할 프로젝트가 하나도 없으면 '이번 주 보고할 프로젝트 "+
					"활동이 없습니다'라고만 한다. ④ 도구 호출·내부 토큰 없이 최종 텍스트만.",
				text,
			)
		}
	}

	return &CommandResult{SkipAgent: false}, nil
}
