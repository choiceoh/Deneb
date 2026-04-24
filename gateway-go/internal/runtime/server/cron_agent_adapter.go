package server

import (
	"context"
	"fmt"
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
	chat *chat.Handler
}

var _ cron.AgentRunner = (*cronChatAdapter)(nil)

func (a *cronChatAdapter) RunAgentTurn(ctx context.Context, params cron.AgentTurnParams) (string, error) {
	// Inject delivery context so proactive tools (especially message.send) can
	// route back to the cron job's configured channel. Without this, the tool
	// returns "no active delivery target" and the agent tends to fabricate a
	// "channel not connected" follow-up that actually does reach the user,
	// producing the self-contradicting message we saw in production.
	opts := &chat.SyncOptions{}
	if params.Channel != "" && params.To != "" {
		opts.Delivery = &chat.DeliveryContext{
			Channel:   params.Channel,
			To:        params.To,
			AccountID: params.AccountID,
		}
	}
	result, err := a.chat.SendSync(ctx, params.SessionKey, params.Command, "", opts)
	if err != nil {
		return "", err
	}
	// Pick the deliverable:
	//   - Default: the final turn's text (result.Text). This is what a natural
	//     end_turn run is expected to produce.
	//   - Fallback A (empty final text): the agent ended with NO_REPLY or a bare
	//     acknowledgement. Use AllText so the earlier body survives.
	//   - Fallback B (truncated): stopReason != end_turn (max_turns, max_tokens)
	//     means the last turn is mid-stream planning like "이제 위키 업데이트하고
	//     텔레그램으로 전송할게", not the deliverable. Use AllText.
	//   - Fallback C (final text is much shorter than AllText): the agent
	//     composed the body in an earlier turn and closed with a short status
	//     line like "프로젝트 위키 4개 업데이트 완료" while turning an
	//     end_turn stop. We saw exactly this in production (19:35
	//     email-analysis-full delivered a 131-byte wiki-update status instead
	//     of the 922-char analysis). Prefer AllText whenever it is meaningfully
	//     larger — the 3× threshold keeps normal single-turn replies on Text.
	// NO_REPLY is stripped so the marker does not leak into Telegram.
	text := strings.TrimSpace(result.Text)
	allText := strings.TrimSpace(tokens.StripSilentToken(result.AllText, tokens.SilentReplyToken))
	truncated := result.StopReason != "" && result.StopReason != "end_turn"
	shortWrapUp := allText != "" && len(allText) >= 3*len(text)+200
	if text == "" || truncated || shortWrapUp {
		if allText != "" {
			return allText, nil
		}
	}
	return text, nil
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
