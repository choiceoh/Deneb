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
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/inbound"
	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// mediaDownloadTimeout bounds how long we wait for Telegram CDN downloads.
const mediaDownloadTimeout = 30 * time.Second

// InboundProcessor preprocesses incoming Telegram messages through the
// autoreply pipeline before dispatching to the chat handler.
type InboundProcessor struct {
	cmdRegistry      *autoreply.CommandRegistry
	cmdRouter        *autoreply.CommandRouter
	chatHandler      *chat.Handler
	server           *Server
	logger           *slog.Logger
	mediaGroupBatch  *MediaGroupBatcher
}

// NewInboundProcessor creates a processor with the full autoreply command set.
func NewInboundProcessor(s *Server) *InboundProcessor {
	registry := autoreply.NewCommandRegistry(autoreply.BuiltinChatCommands())
	router := autoreply.NewCommandRouter(registry)

	p := &InboundProcessor{
		cmdRegistry: registry,
		cmdRouter:   router,
		chatHandler: s.chatHandler,
		server:      s,
		logger:      s.logger.With("pkg", "inbound"),
	}

	// Media group batcher: collects multiple photos sent together and
	// processes them as a single message with all images attached.
	p.mediaGroupBatch = NewMediaGroupBatcher(func(messages []*telegram.Message) {
		p.handleMediaGroup(messages)
	})

	return p
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
	if msg == nil {
		return
	}

	// Determine the text body: Text for text messages, Caption for media messages.
	msgText := media.MessageText(msg)
	hasMedia := media.HasMedia(msg)

	// Skip messages with neither text nor processable media.
	if msgText == "" && !hasMedia {
		return
	}

	// Media group batching: when user sends multiple photos at once, Telegram
	// delivers each as a separate update with the same media_group_id. Buffer
	// them and process together so the agent sees all images in one run.
	if hasMedia && msg.MediaGroupID != "" {
		if p.mediaGroupBatch.Add(msg) {
			return // buffered; will be dispatched after batch delay
		}
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

	msgCtx := &types.MsgContext{
		Body:              msgText,
		RawBody:           msgText,
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
	inbound.FinalizeInboundContext(msgCtx)

	// Strip bot mentions in group chats.
	if msgCtx.IsGroup {
		msgCtx.BodyForAgent = inbound.StripMentions(msgCtx.BodyForAgent, "")
		msgCtx.BodyForCommands = inbound.StripMentions(msgCtx.BodyForCommands, "")
	}

	// Try slash command dispatch (only for text messages with commands).
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
				Session: &types.SessionState{
					SessionKey: sessionKey,
					Channel:    "telegram",
					IsGroup:    msgCtx.IsGroup,
				},
				Deps: p.buildCommandDeps(sessionKey),
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
	agentMessage := msgCtx.BodyForAgent
	if agentMessage != "" {
		directives := autoreply.ParseInlineDirectives(agentMessage, nil)
		if directives.Cleaned != "" {
			agentMessage = directives.Cleaned
		}
	}

	// Interactive replies: extract reply context when user replies to a message.
	if rc := ExtractReplyContext(msg, p.server.telegramPlug.BotUserID()); rc != nil {
		msgCtx.ReplyToID = rc.ReplyToID
		if prefix := FormatReplyPrefix(rc); prefix != "" {
			agentMessage = prefix + "\n" + agentMessage
		}
	}

	// Extract media attachments (download + base64-encode).
	var attachments []chat.ChatAttachment
	if hasMedia {
		attachments = p.extractAttachments(msg)

		// If no text was provided with media, use a default analysis prompt.
		if agentMessage == "" && len(attachments) > 0 {
			agentMessage = "이 미디어를 분석해 주세요."
		}
		// If media download failed entirely (no attachments extracted) but the
		// user sent media with no caption, fall back to a notice so the agent
		// run still fires instead of silently dropping the message.
		if agentMessage == "" && len(attachments) == 0 {
			agentMessage = "[이미지 다운로드 실패 — 다시 보내 주세요]"
		}
	}

	// Auto-detect YouTube URLs and extract transcript as context.
	if agentMessage != "" && media.IsYouTubeURL(agentMessage) {
		ytURLs := media.ExtractYouTubeURLs(agentMessage)
		for _, ytURL := range ytURLs {
			ytCtx, ytCancel := context.WithTimeout(context.Background(), 15*time.Second)
			ytResult, err := media.ExtractYouTubeTranscript(ytCtx, ytURL)
			ytCancel()
			if err != nil {
				p.logger.Warn("youtube transcript extraction failed", "url", ytURL, "error", err)
				continue
			}
			agentMessage = agentMessage + "\n\n" + media.FormatYouTubeResult(ytResult)
		}
	}

	// Enrich message with fetched link content (bounded to avoid blocking inbound).
	linkCtx, linkCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer linkCancel()
	if linkSummary := EnrichMessageWithLinks(
		linkCtx, agentMessage, defaultLinkFetcher, p.logger,
	); linkSummary != "" {
		agentMessage = agentMessage + "\n\n" + linkSummary
	}

	// Build delivery context with triggering message ID for reply threading.
	delivery := map[string]any{
		"channel": "telegram",
		"to":      chatID,
	}
	if msg.MessageID != 0 {
		delivery["messageId"] = strconv.FormatInt(msg.MessageID, 10)
	}

	// Build chat.send params.
	sendParams := map[string]any{
		"sessionKey": sessionKey,
		"message":    agentMessage,
		"delivery":   delivery,
	}
	if len(attachments) > 0 {
		sendParams["attachments"] = attachments
	}

	// Dispatch to chat.send with the preprocessed message and media attachments.
	req, err := protocol.NewRequestFrame(
		"tg-"+chatID+"-"+strconv.FormatInt(msg.MessageID, 10),
		"chat.send",
		sendParams,
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

// extractAttachments downloads media from a Telegram message with a bounded timeout.
func (p *InboundProcessor) extractAttachments(msg *telegram.Message) []chat.ChatAttachment {
	tgClient := p.server.telegramPlug.Client()
	if tgClient == nil {
		return nil
	}

	dlCtx, dlCancel := context.WithTimeout(context.Background(), mediaDownloadTimeout)
	defer dlCancel()

	mediaAtts := media.ExtractAttachments(dlCtx, tgClient, msg, p.logger)

	var attachments []chat.ChatAttachment
	for _, ma := range mediaAtts {
		attachments = append(attachments, chat.ChatAttachment{
			Type:     ma.Type,
			MimeType: ma.MimeType,
			Data:     ma.Data,
			Name:     ma.Name,
			Size:     ma.Size,
		})
	}
	return attachments
}

// handleMediaGroup processes a batch of messages from the same Telegram media group.
// All photos are extracted and sent as a single chat.send with multiple image attachments.
func (p *InboundProcessor) handleMediaGroup(messages []*telegram.Message) {
	if len(messages) == 0 {
		return
	}

	// Use the first message for metadata (chat, sender, caption).
	first := messages[0]
	chatID := fmt.Sprintf("%d", first.Chat.ID)
	sessionKey := "telegram:" + chatID

	// Collect caption from whichever message has one (Telegram puts the caption
	// on only one of the media group messages, usually the first).
	var caption string
	for _, msg := range messages {
		if c := media.MessageText(msg); c != "" {
			caption = c
			break
		}
	}

	// Extract attachments from all messages in the group.
	var allAttachments []chat.ChatAttachment
	for _, msg := range messages {
		atts := p.extractAttachments(msg)
		allAttachments = append(allAttachments, atts...)
	}

	agentMessage := caption
	if agentMessage == "" && len(allAttachments) > 0 {
		agentMessage = "이 미디어를 분석해 주세요."
	}
	if agentMessage == "" && len(allAttachments) == 0 {
		agentMessage = "[이미지 다운로드 실패 — 다시 보내 주세요]"
	}

	// Build delivery context from the first message.
	delivery := map[string]any{
		"channel": "telegram",
		"to":      chatID,
	}
	if first.MessageID != 0 {
		delivery["messageId"] = strconv.FormatInt(first.MessageID, 10)
	}

	sendParams := map[string]any{
		"sessionKey": sessionKey,
		"message":    agentMessage,
		"delivery":   delivery,
	}
	if len(allAttachments) > 0 {
		sendParams["attachments"] = allAttachments
	}

	req, err := protocol.NewRequestFrame(
		"tg-"+chatID+"-mg-"+first.MediaGroupID,
		"chat.send",
		sendParams,
	)
	if err != nil {
		p.logger.Error("failed to build chat.send for media group", "error", err)
		return
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer sendCancel()
	resp := p.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		p.logger.Warn("chat.send failed for media group",
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
	html := telegram.MarkdownToTelegramHTML(replyText)
	if _, err := telegram.SendText(ctx, client, id, html, telegram.SendOptions{ParseMode: "HTML"}); err != nil {
		p.logger.Warn("failed to send command reply", "chatId", chatID, "error", err)
	}
}

// buildCommandDeps creates a CommandDeps populated with server-level status data.
// sessionKey is used to look up the current session's last failure reason for /status.
func (p *InboundProcessor) buildCommandDeps(sessionKey string) *autoreply.CommandDeps {
	sd := &autoreply.StatusDeps{
		Version:   p.server.version,
		StartedAt: p.server.startedAt,
		RustFFI:   p.server.rustFFI,
	}
	if p.server.sessions != nil {
		sd.SessionCount = p.server.sessions.Count()
	}
	sd.WSConnections = p.server.clientCnt.Load()

	// Per-provider usage stats.
	if p.server.usageTracker != nil {
		report := p.server.usageTracker.Status()
		if report != nil && len(report.Providers) > 0 {
			sd.ProviderUsage = make(map[string]*autoreply.ProviderUsageStats, len(report.Providers))
			for name, ps := range report.Providers {
				sd.ProviderUsage[name] = &autoreply.ProviderUsageStats{
					Calls:  ps.Calls,
					Input:  ps.Tokens.Input,
					Output: ps.Tokens.Output,
				}
			}
		}
	}

	// Channel health.
	if p.server.channelHealth != nil {
		snapshot := p.server.channelHealth.HealthSnapshot()
		if len(snapshot) > 0 {
			sd.ChannelHealth = make([]autoreply.ChannelHealthEntry, len(snapshot))
			for i, ch := range snapshot {
				sd.ChannelHealth[i] = autoreply.ChannelHealthEntry{
					ID:      ch.ChannelID,
					Healthy: ch.Healthy,
					Reason:  ch.Reason,
				}
			}
		}
	}

	// Session-specific failure reason for /status.
	if sessionKey != "" && p.server.sessions != nil {
		if sess := p.server.sessions.Get(sessionKey); sess != nil {
			sd.LastFailureReason = sess.FailureReason
		}
	}

	var subagentRunsFn func() []subagentpkg.SubagentRunRecord
	if p.server.acpDeps != nil && p.server.acpDeps.Registry != nil {
		reg := p.server.acpDeps.Registry
		key := sessionKey
		subagentRunsFn = func() []subagentpkg.SubagentRunRecord {
			agents := reg.List(key)
			runs := make([]subagentpkg.SubagentRunRecord, len(agents))
			for i, a := range agents {
				runs[i] = subagentpkg.SubagentRunRecord{
					RunID:           a.ID,
					ChildSessionKey: a.SessionKey,
					RequesterKey:    key,
					SpawnDepth:      a.Depth,
					WorkspaceDir:    a.WorkspaceDir,
					CreatedAt:       a.SpawnedAt,
					StartedAt:       a.SpawnedAt,
					EndedAt:         a.EndedAt,
					OutcomeStatus:   a.Status,
				}
			}
			return runs
		}
	}

	return &autoreply.CommandDeps{Status: sd, SubagentRuns: subagentRunsFn}
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
