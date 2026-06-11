// run_delivery_failure.go — delivery-failure transcript notes, stop-reason
// fallback messages, and external-delivery/sub-agent guards used by the run
// lifecycle handlers. Split from run_lifecycle.go (pure move, ~700-LOC rule).

package chat

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

const externalDeliveryFailureNotice = "외부 채널 전송이 실패했습니다. 전달이 확인되지 않았습니다. 현재 채팅에 보인다고 가정하지 말고 채널 연결을 확인한 뒤 다시 시도해 주세요."

// persistReplyDeliveryFailure records a synthetic system note in the
// transcript when the primary channel reply callback fails permanently.
// Without this, the next turn's agent only sees its own (successful-looking)
// assistant output and will hallucinate reasons to the user ("channel was
// disconnected", "I already replied") when asked why they didn't see it.
// The note gives the agent ground truth to work with.
func persistReplyDeliveryFailure(deps runDeps, sessionKey, channel string, deliverErr error, logger *slog.Logger) {
	if deps.transcript == nil {
		return
	}
	text := "[SYSTEM: 직전 어시스턴트 응답은 " + channel + " 채널로 **전송이 확인되지 않았습니다**. " +
		"유저가 그 응답을 봤다고 가정하지 마세요. 유저가 이유를 물으면 상황을 모른다고 말하고, " +
		"채널 연결 같은 추측성 설명을 지어내지 마세요."
	if deliverErr != nil {
		text += " (원인 힌트: " + deliverErr.Error() + ")"
	}
	text += "]"
	msg := NewTextChatMessage("user", text, time.Now().UnixMilli())
	if err := deps.transcript.Append(sessionKey, msg); err != nil {
		logger.Warn("failed to persist delivery-failure note", "error", err)
	}
}

// persistMediaDeliveryFailure records a synthetic system note when one or
// more media attachments the agent sent failed to reach the channel.
// Without this, the agent believes the image/audio was delivered and may
// reference it in subsequent turns ("did you see the image I just sent?")
// even though the user only saw the text.
func persistMediaDeliveryFailure(deps runDeps, sessionKey, channel string, failedURLs []string, logger *slog.Logger) {
	if deps.transcript == nil || len(failedURLs) == 0 {
		return
	}
	text := "[SYSTEM: 직전 턴에서 시도한 미디어 첨부 " +
		strconv.Itoa(len(failedURLs)) + "건이 " + channel + " 채널로 **전달되지 않았습니다**. " +
		"유저는 그 미디어를 보지 못했습니다. 다음 턴에서 '아까 보낸 이미지/오디오'라고 언급하지 마세요."
	text += " 실패한 URL: " + strings.Join(failedURLs, ", ") + "]"
	msg := NewTextChatMessage("user", text, time.Now().UnixMilli())
	if err := deps.transcript.Append(sessionKey, msg); err != nil {
		logger.Warn("failed to persist media-failure note", "error", err)
	}
}

// fallbackForStopReason returns a user-visible Korean message for abnormal
// terminations where the agent produced no text output. Empty string means
// no fallback needed (e.g., end_turn is a normal termination — tool-only
// turns legitimately produce no text and the caller already logged it).
func fallbackForStopReason(stopReason string) string {
	switch stopReason {
	case "max_turns":
		return "응답 생성이 반복 한도에 도달해 마무리되지 않았어요. 다시 한 번 말해 주세요 — 더 짧게 끊어서 요청하면 잘 끝납니다."
	case "max_turns_graceful":
		// The grace call iteration normally produces a wrap-up text, so this
		// fallback is only reached when even that turn yielded no output.
		return "응답이 턴 예산 한도에 도달했지만 마무리 답변을 받지 못했어요. 다시 요청해 주세요."
	case "timeout":
		return "응답 생성이 시간 초과로 중단됐어요. 잠시 후에 다시 시도해 주세요."
	case "aborted":
		return "작업이 중단됐어요. 이어서 진행하려면 다시 요청해 주세요."
	case "error":
		return "응답 생성 중 오류가 발생했어요. 다시 시도해 주세요."
	case stopReasonCompressionStuck:
		// Mid-loop compaction can no longer reduce context (either the
		// protected head+tail already exceed budget, or two consecutive
		// retries produced byte-identical inputs). The session is
		// unrecoverable without starting fresh.
		return compactionKoreanError
	default:
		// end_turn, empty, or unknown — empty text is legitimate (tool-only turn).
		return ""
	}
}

// persistInterruptedContext saves a context note to the transcript when a run
// is aborted while tools were executing. This ensures the next run has context
// about what was being done, preventing the "amnesia" bug where the assistant
// forgets its in-progress work when the user sends a message mid-execution.
func persistInterruptedContext(deps runDeps, sessionKey string, result *agent.AgentResult, logger *slog.Logger) {
	if deps.transcript == nil || len(result.InterruptedToolNames) == 0 {
		return
	}

	// Build a concise note listing the tools that were running and any
	// partial text the assistant had produced before interruption.
	var sb strings.Builder
	sb.WriteString("**System:** the previous assistant turn was interrupted by the user while executing tools: ")
	for i, name := range result.InterruptedToolNames {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(name)
	}
	sb.WriteString(".")
	if result.Text != "" {
		sb.WriteString(" Partial response before interruption: ")
		// Truncate to avoid bloating the transcript.
		partial := result.Text
		if len(partial) > 500 {
			partial = partial[:500] + "..."
		}
		sb.WriteString(partial)
	}
	sb.WriteString(" Continue or adjust based on the user's new message.")

	msg := NewTextChatMessage("user", sb.String(), time.Now().UnixMilli())
	if err := deps.transcript.Append(sessionKey, msg); err != nil {
		logger.Warn("failed to persist interrupted context", "error", err)
	} else {
		logger.Info("persisted interrupted context",
			"tools", result.InterruptedToolNames,
			"turns", result.Turns)
	}

}

func hasFailedExternalDeliveryTool(toolActivities []agent.ToolActivity) bool {
	for _, ta := range toolActivities {
		if !ta.IsError {
			continue
		}
		if ta.Name == "message" || ta.Name == "send_file" {
			return true
		}
	}
	return false
}

func shouldForceExternalDeliveryFailureNotice(delivery *DeliveryContext, toolActivities []agent.ToolActivity, text string, isSilent bool) bool {
	if delivery == nil || !hasFailedExternalDeliveryTool(toolActivities) {
		return false
	}
	if isSilent {
		return true
	}
	return strings.TrimSpace(text) == ""
}

// isSubagentSession reports whether the run's session was spawned as a
// sub-agent (child) of another session. Sub-agents have no user-facing
// channel: their final output is read by the parent via session.LastOutput
// (see ToolSessionsSpawn / the subagents tool), so a nil channel replyFunc at
// completion is the normal, expected state rather than a delivery wiring bug.
// Callers use this to avoid raising false delivery-failure alarms for children.
func isSubagentSession(deps runDeps, sessionKey string) bool {
	if deps.sessions == nil {
		return false
	}
	sess := deps.sessions.Get(sessionKey)
	return sess != nil && sess.SpawnedBy != ""
}
