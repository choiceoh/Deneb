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
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/inbound"
	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// mediaDownloadTimeout bounds how long we wait for Telegram CDN downloads.
const mediaDownloadTimeout = 30 * time.Second

// InboundProcessor preprocesses incoming Telegram messages through the
// autoreply pipeline before dispatching to the chat handler.
type InboundProcessor struct {
	cmdRegistry     *handlers.CommandRegistry
	cmdRouter       *handlers.CommandRouter
	chatHandler     *chat.Handler
	server          *Server
	logger          *slog.Logger
	mediaGroupBatch *MediaGroupBatcher
}

// NewInboundProcessor creates a processor with the full autoreply command set.
func NewInboundProcessor(s *Server) *InboundProcessor {
	registry := handlers.NewCommandRegistry(handlers.BuiltinChatCommands())
	router := handlers.NewCommandRouter(registry)

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

	// Handle edited messages: log the edit for observability. In a single-user
	// deployment the original session context is not retroactively updated, but
	// the edit is surfaced in logs so the operator can correlate if needed.
	if update.EditedMessage != nil {
		p.logger.Debug("telegram message edited",
			"chatId", update.EditedMessage.Chat.ID,
			"msgId", update.EditedMessage.MessageID,
		)
		return
	}

	// Handle channel posts (messages posted to a Telegram channel the bot is in).
	if update.ChannelPost != nil {
		p.logger.Debug("telegram channel post",
			"chatId", update.ChannelPost.Chat.ID,
			"msgId", update.ChannelPost.MessageID,
		)
		return
	}

	// Handle message reactions (emoji reactions added/removed by users).
	if update.MessageReaction != nil {
		p.logger.Debug("telegram message reaction",
			"chatId", update.MessageReaction.Chat.ID,
			"msgId", update.MessageReaction.MessageID,
		)
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

	// Plugin conversation binding: if a plugin has bound this conversation
	// to a specific session key, use it instead of the default.
	if p.server.conversationBindings != nil {
		bindings := p.server.conversationBindings.ListByChannel("telegram")
		for _, b := range bindings {
			if b.AccountID == chatID && b.Approved && b.SessionKey != "" {
				sessionKey = b.SessionKey
				break
			}
		}
	}

	// Thread bindings: when processing a message in a forum topic thread,
	// create a thread-specific session key so each topic gets its own context.
	if msg.MessageThreadID != 0 && msg.IsTopicMessage {
		if channel.ResolveThreadBindingsEnabled(nil, nil) {
			sessionKey = fmt.Sprintf("telegram:%s:thread:%d", chatID, msg.MessageThreadID)
		}
	}

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

	// Fire message.receive hook after parsing, before dispatch.
	if p.server.hooks != nil {
		p.server.safeGo("hooks:message.receive", func() {
			p.server.hooks.Fire(context.Background(), hooks.EventMessageReceive, map[string]string{
				"DENEB_CHANNEL":    "telegram",
				"DENEB_CHAT_ID":    chatID,
				"DENEB_MESSAGE":    msgText,
				"DENEB_SESSION_KEY": sessionKey,
			})
		})
	}

	// --- Part A: Ack reaction — send 👀 to acknowledge the incoming message.
	shouldAck := channel.ShouldAckReaction(channel.AckReactionGateParams{
		Scope:    channel.AckScopeAll,
		IsDirect: !msgCtx.IsGroup,
		IsGroup:  msgCtx.IsGroup,
	})
	var didAck bool
	if shouldAck {
		client := p.server.telegramPlug.Client()
		if client != nil {
			chatIDInt, _ := telegram.ParseChatID(chatID)
			if err := client.SetMessageReaction(context.Background(), chatIDInt, msg.MessageID, "👀"); err == nil {
				didAck = true
			}
		}
	}

	// --- Part B: Conversation label — resolve a display label for this session.
	convLabel := channel.ResolveConversationLabel(channel.ConversationLabelFields{
		ChatType:     msg.Chat.Type,
		SenderName:   senderName,
		From:         chatID,
		GroupSubject: msg.Chat.Title,
	})
	if convLabel != "" && p.server.sessions != nil {
		p.server.sessions.Patch(sessionKey, session.PatchFields{Label: &convLabel})
	}

	// Strip bot mentions in group chats.
	if msgCtx.IsGroup {
		msgCtx.BodyForAgent = inbound.StripMentions(msgCtx.BodyForAgent, "")
		msgCtx.BodyForCommands = inbound.StripMentions(msgCtx.BodyForCommands, "")

		// Handle /activation command to change group activation mode.
		if hasCmd, activationMode := autoreply.ParseActivationCommand(msgCtx.BodyForCommands, p.cmdRegistry); hasCmd {
			if activationMode != "" && p.server.sessions != nil {
				activationStr := string(activationMode)
				p.server.sessions.Patch(sessionKey, session.PatchFields{GroupActivation: &activationStr})
				p.sendCommandReply(chatID, &handlers.CommandResult{
					Reply: fmt.Sprintf("👥 Group activation: **%s**", activationMode), SkipAgent: true,
				})
			} else {
				p.sendCommandReply(chatID, &handlers.CommandResult{
					Reply: "👥 Usage: /activation mention|always", SkipAgent: true,
				})
			}
			return
		}
	}

	// --- Enrich the agent message before dispatch ---

	// Interactive replies: extract reply context when user replies to a message.
	agentMessage := msgCtx.BodyForAgent
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

	// Update msgCtx with the fully enriched message body for the dispatch pipeline.
	msgCtx.BodyForAgent = agentMessage

	// --- Subagent command intercept ---
	// Check for /subagents, /kill (subagent), /steer, /tell, /focus, /unfocus,
	// /agents commands and dispatch them through the subagent command handler
	// before the general autoreply pipeline.
	if normalized := strings.ToLower(strings.TrimSpace(msgCtx.BodyForCommands)); normalized != "" {
		if subagentpkg.ResolveHandledPrefix(normalized) != "" {
			var threadID string
			if msg.MessageThreadID != 0 {
				threadID = fmt.Sprintf("%d", msg.MessageThreadID)
			}
			subagentResult := p.dispatchSubagentCommand(
				normalized, sessionKey, "telegram", msgCtx.AccountID,
				threadID, msgCtx.SenderID, msgCtx.IsGroup,
			)
			if subagentResult != nil && subagentResult.ShouldStop {
				if subagentResult.Reply != "" {
					p.sendCommandReply(chatID, &handlers.CommandResult{Reply: subagentResult.Reply})
				}
				return
			}
		}
	}

	// --- Dispatch through the autoreply pipeline ---
	// DispatchFromConfig handles: abort detection, command dispatch, inline
	// directive parsing, model resolution, and agent execution (via bridge
	// executor that delegates to chat.send).

	dispatchCfg := autoreply.DispatchConfig{
		SessionKey: sessionKey,
		Channel:    "telegram",
		To:         chatID,
		AccountID:  msgCtx.AccountID,
		ThreadID:   msgCtx.ThreadID,
		IsGroup:    msgCtx.IsGroup,
	}

	executor := &chatSendExecutor{
		chatHandler: p.chatHandler,
		chatID:      chatID,
		messageID:   msg.MessageID,
		attachments: attachments,
		logger:      p.logger,
	}

	dispatchDeps := autoreply.ReplyDeps{
		Agent:       executor,
		Registry:    p.cmdRegistry,
		Router:      p.cmdRouter,
		CommandDeps: p.buildCommandDeps(sessionKey),
		History:     p.server.historyTracker,
		SessionFunc: func(key string) *types.SessionState {
			return &types.SessionState{
				SessionKey: key,
				Channel:    "telegram",
				IsGroup:    msgCtx.IsGroup,
			}
		},
	}

	dispatchResult := autoreply.DispatchFromConfig(
		context.Background(), msgCtx, dispatchCfg, dispatchDeps,
	)

	// If DispatchFromConfig produced command reply payloads (abort/command
	// handled without agent), deliver them directly to Telegram.
	if dispatchResult.Error != nil {
		p.logger.Warn("autoreply dispatch error",
			"chatId", chatID,
			"error", dispatchResult.Error,
		)
	}
	if len(dispatchResult.Payloads) > 0 && !executor.didSend {
		for _, payload := range dispatchResult.Payloads {
			if payload.Text != "" {
				p.sendCommandReply(chatID, &handlers.CommandResult{Reply: payload.Text})
			}
		}
	}

	// Remove ack reaction after the reply is sent.
	channel.RemoveAckReactionAfterReply(channel.RemoveAckReactionAfterReplyParams{
		RemoveAfterReply: true,
		DidAck:           didAck,
		Remove: func() error {
			client := p.server.telegramPlug.Client()
			if client == nil {
				return nil
			}
			chatIDInt, _ := telegram.ParseChatID(chatID)
			return client.SetMessageReaction(context.Background(), chatIDInt, msg.MessageID, "")
		},
		OnError: func(err error) {
			p.logger.Warn("failed to remove ack reaction", "error", err)
		},
	})
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

	// Fire message.receive hook for the media group.
	if p.server.hooks != nil {
		caption := media.MessageText(first)
		p.server.safeGo("hooks:message.receive", func() {
			p.server.hooks.Fire(context.Background(), hooks.EventMessageReceive, map[string]string{
				"DENEB_CHANNEL":    "telegram",
				"DENEB_CHAT_ID":    chatID,
				"DENEB_MESSAGE":    caption,
				"DENEB_SESSION_KEY": sessionKey,
			})
		})
	}

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
func (p *InboundProcessor) sendCommandReply(chatID string, result *handlers.CommandResult) {
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
func (p *InboundProcessor) buildCommandDeps(sessionKey string) *handlers.CommandDeps {
	sd := &handlers.StatusDeps{
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
			sd.ProviderUsage = make(map[string]*handlers.ProviderUsageStats, len(report.Providers))
			for name, ps := range report.Providers {
				sd.ProviderUsage[name] = &handlers.ProviderUsageStats{
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
			sd.ChannelHealth = make([]handlers.ChannelHealthEntry, len(snapshot))
			for i, ch := range snapshot {
				sd.ChannelHealth[i] = handlers.ChannelHealthEntry{
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

	return &handlers.CommandDeps{Status: sd, SubagentRuns: subagentRunsFn}
}

// chatSendExecutor bridges the autoreply.AgentExecutor interface to
// chat.Handler.Send. When the autoreply pipeline decides the message should
// go to the agent (not handled by a command or abort), RunTurn builds a
// chat.send request frame and dispatches it through the existing async
// chat handler pipeline.
type chatSendExecutor struct {
	chatHandler *chat.Handler
	chatID      string
	messageID   int64
	attachments []chat.ChatAttachment
	logger      *slog.Logger
	didSend     bool // set to true after chat.send dispatch
}

func (e *chatSendExecutor) RunTurn(ctx context.Context, cfg autoreply.AgentTurnConfig) (*autoreply.AgentTurnResult, error) {
	// Build delivery context with triggering message ID for reply threading.
	delivery := map[string]any{
		"channel": "telegram",
		"to":      e.chatID,
	}
	if e.messageID != 0 {
		delivery["messageId"] = strconv.FormatInt(e.messageID, 10)
	}

	sendParams := map[string]any{
		"sessionKey": cfg.SessionKey,
		"message":    cfg.Message,
		"delivery":   delivery,
	}
	if cfg.Model != "" {
		sendParams["model"] = cfg.Model
	}
	if len(e.attachments) > 0 {
		sendParams["attachments"] = e.attachments
	}

	req, err := protocol.NewRequestFrame(
		"tg-"+e.chatID+"-"+strconv.FormatInt(e.messageID, 10),
		"chat.send",
		sendParams,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat.send request: %w", err)
	}

	sendCtx, sendCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer sendCancel()
	resp := e.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		errMsg := "unknown error"
		if resp.Error != nil {
			errMsg = resp.Error.Message
		}
		e.logger.Warn("chat.send failed via autoreply executor",
			"chatId", e.chatID,
			"error", errMsg,
		)
	}

	e.didSend = true

	// Return empty result — actual reply delivery is async via chat handler.
	return &autoreply.AgentTurnResult{}, nil
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

	// Fire message.receive hook for callback query.
	if p.server.hooks != nil {
		p.server.safeGo("hooks:message.receive", func() {
			p.server.hooks.Fire(context.Background(), hooks.EventMessageReceive, map[string]string{
				"DENEB_CHANNEL":    "telegram",
				"DENEB_CHAT_ID":    chatID,
				"DENEB_MESSAGE":    cb.Data,
				"DENEB_SESSION_KEY": sessionKey,
			})
		})
	}

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

// dispatchSubagentCommand routes a subagent command through the subagent
// dispatcher, wiring ACP registry deps when available.
func (p *InboundProcessor) dispatchSubagentCommand(
	normalized string,
	sessionKey string,
	channelName string,
	accountID string,
	threadID string,
	senderID string,
	isGroup bool,
) *subagentpkg.SubagentCommandResult {
	var deps *subagentpkg.SubagentCommandDeps
	if p.server.acpDeps != nil && p.server.acpDeps.Registry != nil {
		deps = subagentpkg.NewSubagentCommandDepsFromACP(
			p.server.acpDeps.Registry,
			subagentpkg.ACPCommandDepsConfig{
				Infra: p.server.acpDeps.Infra,
			},
		)
	}
	return subagentpkg.HandleSubagentsCommand(
		normalized, sessionKey, channelName, accountID, threadID,
		senderID, isGroup, true, // isAuthorized: single-user deployment
		deps,
	)
}
