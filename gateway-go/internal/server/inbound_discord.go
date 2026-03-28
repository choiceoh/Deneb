// Package server — Discord inbound message preprocessing via the autoreply pipeline.
//
// Bridges the autoreply command/directive system into the Discord → chat.send
// flow so that slash commands (/new, /model, /think, etc.) and inline directives
// are processed before the message reaches the LLM agent.
package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandleDiscordMessage processes an incoming Discord message through the
// autoreply pipeline: command detection → directive parsing → chat.send dispatch.
func (p *InboundProcessor) HandleDiscordMessage(msg *discord.Message) {
	if msg == nil || msg.Content == "" {
		return
	}

	channelID := msg.ChannelID
	sessionKey := "discord:" + channelID

	// Build autoreply MsgContext from the Discord message.
	var senderID, senderName string
	if msg.Author != nil {
		senderID = msg.Author.ID
		senderName = msg.Author.Username
		if msg.Author.GlobalName != "" {
			senderName = msg.Author.GlobalName
		}
	}

	msgCtx := &types.MsgContext{
		Body:              msg.Content,
		RawBody:           msg.Content,
		From:              channelID,
		To:                channelID,
		SessionKey:        sessionKey,
		MessageSid:        "dc-" + channelID + "-" + msg.ID,
		Channel:           "discord",
		SenderID:          senderID,
		SenderName:        senderName,
		IsGroup:           msg.GuildID != "", // guild messages are "group" context
		CommandAuthorized: true,              // single-user deployment
	}

	// Normalize inbound context.
	autoreply.FinalizeInboundContext(msgCtx)

	// Try slash command dispatch.
	trimmed := strings.TrimSpace(msgCtx.BodyForCommands)
	if strings.HasPrefix(trimmed, "/") {
		cmdKey := extractCommandKey(trimmed)
		if cmdKey != "" && p.cmdRouter.HasHandler(cmdKey) {
			result, err := p.cmdRouter.Dispatch(autoreply.CommandContext{
				Command:    cmdKey,
				Body:       msgCtx.Body,
				SessionKey: sessionKey,
				Channel:    "discord",
				IsGroup:    msgCtx.IsGroup,
				Msg:        msgCtx,
				Session: &types.SessionState{
					SessionKey: sessionKey,
					Channel:    "discord",
					IsGroup:    msgCtx.IsGroup,
				},
				Deps: p.buildCommandDeps(),
			})
			if err == nil && result != nil && result.SkipAgent {
				p.sendDiscordCommandReply(channelID, result)
				return
			}
			if err == nil && result != nil && result.Reply != "" {
				p.sendDiscordCommandReply(channelID, result)
			}
		}
	}

	// Parse inline directives (/model, /think, etc.) and clean the message body.
	agentMessage := msgCtx.BodyForAgent
	if agentMessage != "" {
		directives := autoreply.ParseInlineDirectives(agentMessage, nil)
		if directives.Cleaned != "" {
			agentMessage = directives.Cleaned
		}
	}

	if agentMessage == "" {
		return
	}

	// Build delivery context.
	delivery := map[string]any{
		"channel":   "discord",
		"to":        channelID,
		"messageId": msg.ID,
	}
	if msg.Author != nil {
		delivery["accountId"] = msg.Author.ID
	}

	// Build chat.send params.
	sendParams := map[string]any{
		"sessionKey": sessionKey,
		"message":    agentMessage,
		"delivery":   delivery,
	}

	req, err := protocol.NewRequestFrame(
		"dc-"+channelID+"-"+msg.ID,
		"chat.send",
		sendParams,
	)
	if err != nil {
		p.logger.Error("failed to build chat.send request for discord", "error", err)
		return
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer sendCancel()
	resp := p.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		p.logger.Warn("chat.send failed for discord message",
			"channelId", channelID,
			"error", resp.Error,
		)
	}
}

// sendDiscordCommandReply delivers a command result back to the Discord channel.
func (p *InboundProcessor) sendDiscordCommandReply(channelID string, result *autoreply.CommandResult) {
	replyText := result.Reply
	if replyText == "" && len(result.Payloads) > 0 {
		replyText = result.Payloads[0].Text
	}
	if replyText == "" {
		return
	}

	client := p.server.discordPlug.Client()
	if client == nil {
		p.logger.Warn("discord client not available for command reply")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := discord.SendText(ctx, client, channelID, replyText, ""); err != nil {
		p.logger.Warn("failed to send discord command reply", "channelId", channelID, "error", err)
	}
}
