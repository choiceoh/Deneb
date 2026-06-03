package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// cronChatAdapter adapts chat.Handler to the cron.AgentRunner interface,
// allowing cron jobs to execute agent turns via the chat pipeline.
type cronChatAdapter struct {
	chat   *chat.Handler
	logger *slog.Logger
	// weeklyReportData collects the formal weekly-report data (wiki-based JSON
	// envelope) so a "/weekly" cron payload runs the LLM against pre-collected
	// data inside a fixed form, instead of freestyling. nil when wiki is
	// unwired; the command then falls through to a normal agent turn.
	weeklyReportData func(ctx context.Context) (string, error)
	// weeklyFormDeliver renders the formal 주간업무보고 form image and posts it to
	// the native 업무 chat, so a "/weekly" cron delivers both the text report and
	// the document form. Best-effort and nil-tolerant — a render failure just
	// leaves the text report.
	weeklyFormDeliver func(ctx context.Context) error
}

// isWeeklyReportCommand reports whether a cron payload is the weekly-report
// routine ("/weekly" or its Korean alias).
func isWeeklyReportCommand(command string) bool {
	switch strings.TrimSpace(command) {
	case "/weekly", "/주간보고":
		return true
	default:
		return false
	}
}

// weeklyReportPromptTmpl pins the 주간업무보고 FORM while leaving the writing to
// the LLM. The cron used to hand the agent a vague "topsolar-db 스킬로 weekly
// 정리해줘" prompt and the model freestyled the format (chatty markdown off a
// stale 3-row DB). Here the gateway pre-collects the wiki data deterministically
// and the LLM only fills this exact form — synthesising the 실시/예정 lines and
// 현안 from each project's timeline/next-action text, never inventing the layout.
// The single %s is the collected JSON envelope.
const weeklyReportPromptTmpl = `[주간업무보고 데이터 — 위키 프로젝트 기반, 이미 수집 완료. 도구로 다시 모으지 마세요.]

%s

위 JSON 데이터만으로 기획조정실 주간업무보고를 작성하세요. 아래 양식을 정확히 따르고, 형식·섹션·순서를 임의로 바꾸지 마세요.

[양식]
📋 주간업무보고 — 기획조정실 (보고자: 오선택 실장)
🗓 실시 {week_done} / 예정 {week_planned}

▢ <그룹 label>
  • <프로젝트 title>(<capacity 있으면 표기>)
     - 실시: <지난주 실제 진행 — 그 프로젝트의 timeline_raw·summary를 읽고 핵심 한 줄로>
     - 예정: <이번주 계획 — next_actions_raw를 읽고 핵심 한 줄로>
  (그룹 안 모든 프로젝트를 같은 형식으로)

⚠️ 현안
  - <issues 항목 그대로>

[규칙]
- 데이터에 있는 사실(프로젝트명·용량·날짜·상태)만 쓰고, 없는 내용(프로젝트·조직·인선)을 지어내지 마세요.
- 실시/예정 한 줄은 timeline_raw·next_actions_raw를 읽고 당신이 직접 핵심만 압축하세요(기계적 복붙 금지).
- 소관(그룹) 순서: 1팀 → 2팀 → 3팀 → 남도에코 → 개인. 빈 그룹·빈 현안 섹션은 생략.
- 보고할 프로젝트가 하나도 없으면 "이번 주 보고할 프로젝트 활동이 없습니다"만 출력.
- 인사·빈 서두("좋은 질문" 등)·내부 토큰(<thinking>, NO_REPLY)·채널 상태 추측 금지. 바로 양식부터.`

var _ cron.AgentRunner = (*cronChatAdapter)(nil)

func (a *cronChatAdapter) RunAgentTurn(ctx context.Context, params cron.AgentTurnParams) (string, error) {
	// Inject delivery context so proactive tools (especially message.send) can
	// route back to the cron job's configured channel. Without this, the tool
	// returns "no active delivery target" and the agent tends to fabricate a
	// "channel not connected" follow-up that actually does reach the user,
	// producing the self-contradicting message we saw in production.
	//
	// AutoDeliveredOutput marks every cron run: the agent's final text is
	// delivered to the user's channel by the cron delivery layer (proactive
	// relay / main-session handoff / DeliverCronOutput), so the agent must
	// not deliver it via the message tool, and an in-loop send-guard failure
	// is a benign no-op rather than an outage to report. This stops the LLM
	// from translating a tool error into a "텔레그램 채널이 연결되지 않았다"
	// apology that itself gets delivered through that very channel.
	opts := &chat.SyncOptions{AutoDeliveredOutput: true}
	if params.Channel != "" && params.To != "" {
		opts.Delivery = &chat.DeliveryContext{
			Channel:   params.Channel,
			To:        params.To,
			AccountID: params.AccountID,
			ThreadID:  params.ThreadID,
		}
	}
	// Routine "/weekly" cron payloads get the report data pre-collected and a
	// fixed form injected, so the LLM writes inside the 양식 instead of
	// freestyling (see weeklyReportPromptTmpl). Other commands pass through.
	command := a.resolveCronCommand(ctx, params.Command)
	// Also post the formal form image to the 업무 chat (best-effort) so the user
	// gets both the document form and the text report. Runs before the text turn
	// so the form lands first in the transcript.
	if a.weeklyFormDeliver != nil && isWeeklyReportCommand(params.Command) {
		if err := a.weeklyFormDeliver(ctx); err != nil && a.logger != nil {
			a.logger.Warn("weekly form image delivery failed", "error", err)
		}
	}
	result, err := a.chat.SendSync(ctx, params.SessionKey, command, "", opts)
	if err != nil {
		return "", err
	}
	// Pick the deliverable. Prefer DeliverableText: it accumulates every
	// substantial answer turn (a detailed report plus its wrap-up) while
	// dropping the short "이제 위키 검색부터 할게요" progress narration the model
	// emits before tool calls. That working narration was leaking into cron
	// reports — the final turn alone is often a short status ("위키 업데이트 완료")
	// so the old heuristic fell back to the full AllText, narration and all.
	// Fall back to the final turn, then the raw accumulation, only when no
	// deliverable survived (e.g. a run aborted after emitting only narration).
	// NO_REPLY is stripped so the marker does not leak into Telegram.
	text := strings.TrimSpace(result.Text)
	deliverable := strings.TrimSpace(tokens.StripSilentToken(result.DeliverableText, tokens.SilentReplyToken))
	allText := strings.TrimSpace(tokens.StripSilentToken(result.AllText, tokens.SilentReplyToken))
	source := "deliverable"
	output := deliverable
	switch {
	case output != "":
		// keep the narration-free deliverable
	case text != "":
		source = "text"
		output = text
	case allText != "":
		source = "allText"
		output = allText
	default:
		source = "empty"
	}
	// Log the delivery choice so postmortems can see which bucket the run landed
	// in. Without this, diagnosing "why did the user get a short wrap-up instead
	// of the body" requires reconstructing from per-turn tokens alone.
	logger := a.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("cron agent output chosen",
		"jobId", params.AgentID,
		"sessionKey", params.SessionKey,
		"source", source,
		"textLen", len(text),
		"deliverableLen", len(deliverable),
		"allTextLen", len(allText),
		"chosenLen", len(output),
		"stopReason", result.StopReason)
	return output, nil
}

// resolveCronCommand rewrites recognised routine slash-command payloads into a
// fully-specified prompt. For "/weekly" (or "/주간보고") it pre-collects the
// wiki-based report data and wraps it in the fixed 양식 template so the LLM
// fills a locked form rather than inventing one. Unknown commands (and the
// case where wiki/data is unavailable) pass through unchanged.
func (a *cronChatAdapter) resolveCronCommand(ctx context.Context, command string) string {
	if !isWeeklyReportCommand(command) {
		return command
	}
	if a.weeklyReportData == nil {
		return command
	}
	data, err := a.weeklyReportData(ctx)
	if err != nil || strings.TrimSpace(data) == "" {
		if a.logger != nil {
			a.logger.Warn("weekly report data collection failed; using raw cron command", "error", err)
		}
		return command
	}
	return fmt.Sprintf(weeklyReportPromptTmpl, data)
}

// acpSubagentPoller implements cron.SubagentPoller using the ACP registry
// and session manager to detect and collect descendant subagent outputs.
type acpSubagentPoller struct {
	registry *acp.ACPRegistry
	sessions *session.Manager
}

var _ cron.SubagentPoller = (*acpSubagentPoller)(nil)

func (p *acpSubagentPoller) HasActiveDescendants(sessionKey string) bool {
	if p.registry == nil {
		return false
	}
	// Check all agents — those whose session key starts with the parent prefix
	// or whose ParentID matches are considered descendants.
	agents := p.registry.List("")
	for _, a := range agents {
		if a.Status == "running" || a.Status == "idle" {
			if strings.HasPrefix(a.SessionKey, "acp:"+sessionKey+":") || a.ParentID == sessionKey {
				return true
			}
		}
	}
	return false
}

func (p *acpSubagentPoller) CollectDescendantOutputs(sessionKey string) string {
	if p.registry == nil || p.sessions == nil {
		return ""
	}
	agents := p.registry.List("")
	var parts []string
	for _, a := range agents {
		if !strings.HasPrefix(a.SessionKey, "acp:"+sessionKey+":") && a.ParentID != sessionKey {
			continue
		}
		if a.Status != "done" {
			continue
		}
		sess := p.sessions.Get(a.SessionKey)
		if sess == nil || sess.LastOutput == "" {
			continue
		}
		role := a.Role
		if role == "" {
			role = a.ID
		}
		parts = append(parts, fmt.Sprintf("[%s] %s", role, sess.LastOutput))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}
