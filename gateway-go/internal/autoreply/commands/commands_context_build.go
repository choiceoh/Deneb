// commands_context_build.go — Build command context from message context.
// Mirrors src/auto-reply/reply/commands-context.ts (42 LOC).
package commands

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/inbound"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// InboundCommandContext holds the resolved command context for handler dispatch.
// This is the Go equivalent of the TS CommandContext from commands-types.ts.
type InboundCommandContext struct {
	Surface               string
	Channel               string
	ChannelID             string
	OwnerList             []string
	SenderIsOwner         bool
	IsAuthorizedSender    bool
	SenderID              string
	AbortKey              string
	RawBodyNormalized     string
	CommandBodyNormalized string
	From                  string
	To                    string
}

// BuildInboundCommandContext constructs an InboundCommandContext from message
// context, config, and normalization parameters.
func BuildInboundCommandContext(params struct {
	Msg                   *types.MsgContext
	Channel               *reply.ChannelContext
	AgentID               string
	SessionKey            string
	IsGroup               bool
	TriggerBodyNormalized string
	Registry              *CommandRegistry
}) InboundCommandContext {
	msg := params.Msg
	from := strings.TrimSpace(msg.From)
	to := strings.TrimSpace(msg.To)

	surface := ""
	if params.Channel != nil {
		surface = strings.ToLower(strings.TrimSpace(params.Channel.Channel))
	}
	channel := surface

	abortKey := params.SessionKey
	if abortKey == "" {
		abortKey = from
	}
	if abortKey == "" {
		abortKey = to
	}

	rawBodyNormalized := params.TriggerBodyNormalized

	// For group messages, strip mentions before normalizing the command body.
	bodyForNormalize := rawBodyNormalized
	if params.IsGroup {
		botUsername := ""
		if params.Channel != nil {
			botUsername = params.Channel.BotUsername
		}
		bodyForNormalize = inbound.StripMentions(rawBodyNormalized, botUsername)
	}

	commandBodyNormalized := bodyForNormalize
	if params.Registry != nil {
		botUsername := ""
		if params.Channel != nil {
			botUsername = params.Channel.BotUsername
		}
		commandBodyNormalized = params.Registry.NormalizeCommandBody(bodyForNormalize, botUsername)
	}

	return InboundCommandContext{
		Surface:               surface,
		Channel:               channel,
		OwnerList:             nil,
		SenderIsOwner:         true,
		IsAuthorizedSender:    true,
		AbortKey:              abortKey,
		RawBodyNormalized:     rawBodyNormalized,
		CommandBodyNormalized: commandBodyNormalized,
		From:                  from,
		To:                    to,
	}
}
