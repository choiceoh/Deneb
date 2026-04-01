package server

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
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
