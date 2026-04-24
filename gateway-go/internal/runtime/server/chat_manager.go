package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// ChatManager groups the chat pipeline and its channel delivery backends.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type ChatManager struct {
	chatHandler     *chat.Handler
	toolDeps        *chat.CoreToolDeps
	telegramPlug    *telegram.Plugin
	modelRegistry   *modelrole.Registry
	localAIHub      *localai.Hub
	embeddingClient *embedding.Client

	// proactiveRelay delivers agent-initiated messages (cron results,
	// gmail poll summaries, wiki dreaming notifications) to the user's
	// channel without routing through the LLM. The body is sent verbatim
	// and mirrored into the session transcript so follow-up user turns
	// retain context. Set in registerSessionRPCMethods once both the
	// telegram plugin and transcript store are available.
	proactiveRelay proactiveRelayDeps
}
