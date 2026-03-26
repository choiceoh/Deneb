// Package server — inbound message preprocessing via the autoreply pipeline.
//
// Bridges the autoreply command/directive system into the Telegram → chat.send
// flow so that slash commands (/new, /model, /think, etc.), inline directives
// (!model, !think), and inbound normalization are processed before the message
// reaches the LLM agent.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// InboundProcessor preprocesses incoming Telegram messages through the
// autoreply pipeline before dispatching to the chat handler.
type InboundProcessor struct {
	cmdRegistry *autoreply.CommandRegistry
	cmdRouter   *autoreply.CommandRouter
	chatHandler *chat.Handler
	server      *Server
	logger      *slog.Logger
}

// NewInboundProcessor creates a processor with the full autoreply command set.
func NewInboundProcessor(s *Server) *InboundProcessor {
	registry := autoreply.NewCommandRegistry(autoreply.BuiltinChatCommands())
	router := autoreply.NewCommandRouter(registry)

	return &InboundProcessor{
		cmdRegistry: registry,
		cmdRouter:   router,
		chatHandler: s.chatHandler,
		server:      s,
		logger:      s.logger.With("pkg", "inbound"),
	}
}

// HandleTelegramUpdate processes an incoming Telegram update through the
// autoreply pipeline: inbound normalization → command detection → directive
// parsing → link enrichment → chat.send dispatch.
func (p *InboundProcessor) HandleTelegramUpdate(update *telegram.Update) {
	// Handle callback queries (inline button clicks).
	if update.CallbackQuery != nil {
		p.handleCallbackQuery(update.CallbackQuery)
		return
	}

	msg := update.Message
	if msg == nil || msg.Text == "" {
		return
	}

	chatID := fmt.Sprintf("%d", msg.Chat.ID)
	sessionKey := "telegram:" + chatID

	// Build autoreply MsgContext from the Telegram message.
	var senderID string
	var senderName string
	if msg.From != nil {
		senderID = fmt.Sprintf("%d", msg.From.ID)
		senderName = buildSenderName(msg.From)
	}

	msgCtx := &autoreply.MsgContext{
		Body:              msg.Text,
		RawBody:           msg.Text,
		From:              chatID,
		To:                chatID,
		SessionKey:        sessionKey,
		MessageSid:        fmt.Sprintf("tg-%s-%d", chatID, msg.MessageID),
		Channel:           "telegram",
		SenderID:          senderID,
		SenderName:        senderName,
		IsGroup:           isGroupChat(msg.Chat),
		ChatType:          msg.Chat.Type,
		CommandAuthorized: true, // single-user deployment
	}

	// Normalize inbound context (defaults for CommandBody, BodyForAgent, etc.).
	autoreply.FinalizeInboundContext(msgCtx)

	// Strip bot mentions in group chats.
	if msgCtx.IsGroup {
		msgCtx.BodyForAgent = autoreply.StripMentions(msgCtx.BodyForAgent, "")
		msgCtx.BodyForCommands = autoreply.StripMentions(msgCtx.BodyForCommands, "")
	}

	// Try slash command dispatch.
	trimmed := strings.TrimSpace(msgCtx.BodyForCommands)
	if strings.HasPrefix(trimmed, "/") {
		cmdKey := extractCommandKey(trimmed)
		if cmdKey != "" && p.cmdRouter.HasHandler(cmdKey) {
			result, err := p.cmdRouter.Dispatch(autoreply.CommandContext{
				Command:    cmdKey,
				Body:       msgCtx.Body,
				SessionKey: sessionKey,
				Channel:    "telegram",
				IsGroup:    msgCtx.IsGroup,
				Msg:        msgCtx,
				Session: &autoreply.SessionState{
					SessionKey: sessionKey,
					Channel:    "telegram",
					IsGroup:    msgCtx.IsGroup,
				},
			})
			if err == nil && result != nil && result.SkipAgent {
				// Command handled; send reply back to Telegram.
				p.sendCommandReply(chatID, result)
				return
			}
			// Command processed but agent should continue (e.g., /btw).
			if err == nil && result != nil && result.Reply != "" {
				p.sendCommandReply(chatID, result)
			}
		}
	}

	// Parse inline directives (!model, !think, etc.) and clean the message body.
	directives := autoreply.ParseInlineDirectives(msgCtx.BodyForAgent, nil)
	agentMessage := directives.Cleaned
	if agentMessage == "" {
		agentMessage = msgCtx.BodyForAgent
	}

	// Interactive replies: extract reply context when user replies to a message.
	if rc := ExtractReplyContext(msg, p.server.telegramPlug.BotUserID()); rc != nil {
		msgCtx.ReplyToID = rc.ReplyToID
		if prefix := FormatReplyPrefix(rc); prefix != "" {
			agentMessage = prefix + "\n" + agentMessage
		}
	}

	// Enrich message with fetched link content.
	if linkSummary := EnrichMessageWithLinks(
		context.Background(), agentMessage, defaultLinkFetcher, p.logger,
	); linkSummary != "" {
		agentMessage = agentMessage + "\n\n" + linkSummary
	}

	// Dispatch to chat.send with the preprocessed message.
	delivery := map[string]any{
		"channel": "telegram",
		"to":      chatID,
	}
	// Pass the triggering message ID for [[reply_to_current]] support.
	if msg.MessageID != 0 {
		delivery["messageId"] = strconv.FormatInt(msg.MessageID, 10)
	}

	req, err := protocol.NewRequestFrame(
		"tg-"+chatID+"-"+strconv.FormatInt(msg.MessageID, 10),
		"chat.send",
		map[string]any{
			"sessionKey": sessionKey,
			"message":    agentMessage,
			"delivery":   delivery,
		},
	)
	if err != nil {
		p.logger.Error("failed to build chat.send request", "error", err)
		return
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer sendCancel()
	resp := p.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		p.logger.Warn("chat.send failed for telegram message",
			"chatId", chatID,
			"error", resp.Error,
		)
	}
}

// sendCommandReply delivers a command result back to the Telegram chat.
func (p *InboundProcessor) sendCommandReply(chatID string, result *autoreply.CommandResult) {
	replyText := result.Reply
	if replyText == "" && len(result.Payloads) > 0 {
		replyText = result.Payloads[0].Text
	}
	if replyText == "" {
		return
	}

	client := p.server.telegramPlug.Client()
	if client == nil {
		p.logger.Warn("telegram client not available for command reply")
		return
	}

	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	html := telegram.FormatHTML(replyText)
	if _, err := telegram.SendText(ctx, client, id, html, telegram.SendOptions{ParseMode: "HTML"}); err != nil {
		p.logger.Warn("failed to send command reply", "chatId", chatID, "error", err)
	}
}

// extractCommandKey pulls the command name from a slash-prefixed message.
// "/model gpt-4" → "model", "/new" → "new".
func extractCommandKey(text string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(text), "/")
	if trimmed == "" {
		return ""
	}
	// Take first word.
	if idx := strings.IndexAny(trimmed, " \t\n"); idx > 0 {
		trimmed = trimmed[:idx]
	}
	return strings.ToLower(trimmed)
}

// buildSenderName constructs a display name from a Telegram user.
func buildSenderName(from *telegram.User) string {
	if from == nil {
		return ""
	}
	name := from.FirstName
	if from.LastName != "" {
		name += " " + from.LastName
	}
	return name
}

// isGroupChat checks if a Telegram chat is a group/supergroup.
func isGroupChat(chat telegram.Chat) bool {
	return chat.Type == "group" || chat.Type == "supergroup"
}

// handleCallbackQuery processes an inline keyboard button click.
// Acknowledges the query to Telegram and routes the callback data as a text
// message to the agent session.
func (p *InboundProcessor) handleCallbackQuery(cb *telegram.CallbackQuery) {
	if cb.Message == nil || cb.Data == "" {
		return
	}

	chatID := fmt.Sprintf("%d", cb.Message.Chat.ID)
	sessionKey := "telegram:" + chatID

	// Acknowledge to Telegram (stops the loading spinner on the button).
	client := p.server.telegramPlug.Client()
	if client != nil {
		ackCtx, ackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer ackCancel()
		if err := telegram.AnswerCallbackQuery(ackCtx, client, cb.ID, ""); err != nil {
			p.logger.Warn("failed to answer callback query", "error", err)
		}
	}

	// Route callback data as a text message to the agent.
	agentMessage := fmt.Sprintf("[Button: %s]", cb.Data)

	req, err := protocol.NewRequestFrame(
		fmt.Sprintf("tg-%s-cb-%s", chatID, cb.ID),
		"chat.send",
		map[string]any{
			"sessionKey": sessionKey,
			"message":    agentMessage,
			"delivery": map[string]any{
				"channel": "telegram",
				"to":      chatID,
			},
		},
	)
	if err != nil {
		p.logger.Error("failed to build chat.send request for callback", "error", err)
		return
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer sendCancel()
	resp := p.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		p.logger.Warn("chat.send failed for callback query",
			"chatId", chatID,
			"error", resp.Error,
		)
	}
}
