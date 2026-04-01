package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// cronChatAdapter adapts chat.Handler to the cron.AgentRunner interface,
// allowing cron jobs to execute agent turns via the chat pipeline.
type cronChatAdapter struct {
	chat *chat.Handler
}

var _ cron.AgentRunner = (*cronChatAdapter)(nil)

func (a *cronChatAdapter) RunAgentTurn(ctx context.Context, params cron.AgentTurnParams) (string, error) {
	result, err := a.chat.SendSync(ctx, params.SessionKey, params.Command, "", nil)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// cronTranscriptCloner adapts chat.FileTranscriptStore to the cron.TranscriptCloner
// interface for shadow session transcript operations.
type cronTranscriptCloner struct {
	store *chat.FileTranscriptStore
}

var _ cron.TranscriptCloner = (*cronTranscriptCloner)(nil)

func (c *cronTranscriptCloner) CloneRecent(srcKey, dstKey string, limit int) error {
	return c.store.CloneRecent(srcKey, dstKey, limit)
}

func (c *cronTranscriptCloner) DeleteTranscript(key string) error {
	return c.store.Delete(key)
}

func (c *cronTranscriptCloner) AppendSystemNote(sessionKey, text string) error {
	return c.store.AppendSystemNote(sessionKey, text)
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
