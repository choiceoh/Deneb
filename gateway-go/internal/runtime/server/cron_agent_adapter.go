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
}

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
	result, err := a.chat.SendSync(ctx, params.SessionKey, params.Command, "", opts)
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
