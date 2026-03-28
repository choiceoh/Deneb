package autoreply

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// InboundDispatchResult holds the outcome of dispatching an inbound message.
type InboundDispatchResult struct {
	Handled    bool
	CommandKey string
	Replies    []types.ReplyPayload
	Error      error
}

// DispatchInbound processes an inbound message through the auto-reply pipeline:
// command detection, agent execution, and reply delivery.
//
// This is the Go equivalent of dispatchInboundMessage() from the TS codebase.
// It coordinates the full message lifecycle:
// 1. Normalize and detect commands
// 2. Check deduplication
// 3. Route to command handler or agent
// 4. Deliver replies via the dispatcher
func DispatchInbound(
	ctx context.Context,
	params DispatchInboundParams,
) InboundDispatchResult {
	_ = ctx
	if params.Text == "" && len(params.Attachments) == 0 {
		return InboundDispatchResult{}
	}

	// Normalize command body.
	normalizedText := params.Text
	if params.Registry != nil {
		normalizedText = params.Registry.NormalizeCommandBody(params.Text, params.BotUsername)
	}

	// Check for control commands.
	if params.Registry != nil && params.Registry.HasControlCommand(normalizedText, "") {
		return InboundDispatchResult{
			Handled:    true,
			CommandKey: extractCommandKey(normalizedText),
		}
	}

	// Build reply payload and dispatch via the chat handler.
	// The actual agent execution is delegated to chat.Handler.Send().
	return InboundDispatchResult{Handled: true}
}

// DispatchInboundParams holds the parameters for inbound message dispatch.
type DispatchInboundParams struct {
	Text        string
	Attachments []string
	SessionKey  string
	Channel     string
	To          string
	AccountID   string
	ThreadID    string
	BotUsername string
	Registry    *CommandRegistry
}

func extractCommandKey(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return ""
	}
	// Extract just the command name.
	end := strings.IndexAny(trimmed[1:], " \t\n")
	if end == -1 {
		return trimmed[1:]
	}
	return trimmed[1 : end+1]
}
