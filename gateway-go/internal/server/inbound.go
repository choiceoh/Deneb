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
	"html"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/inbound"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/streaming"
	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
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
		sessionKey = fmt.Sprintf("telegram:%s:thread:%d", chatID, msg.MessageThreadID)
	}

	// Build autoreply MsgContext from the Telegram message.
	var senderID string
	var senderName string
	if msg.From != nil {
		senderID = fmt.Sprintf("%d", msg.From.ID)
		senderName = buildSenderName(msg.From)
	}

	msgCtx := &types.MsgContext{
		Body:       msgText,
		RawBody:    msgText,
		From:       chatID,
		To:         chatID,
		MessageSid: fmt.Sprintf("tg-%s-%d", chatID, msg.MessageID),
		SessionOrigin: types.SessionOrigin{
			SessionKey: sessionKey,
			Channel:    "telegram",
			IsGroup:    isGroupChat(msg.Chat),
		},
		SenderInfo: types.SenderInfo{
			SenderID:   senderID,
			SenderName: senderName,
			ChatType:   msg.Chat.Type,
		},
		CommandControl: types.CommandControl{
			CommandAuthorized: true, // single-user deployment
		},
	}

	// Normalize inbound context (defaults for CommandBody, BodyForAgent, etc.).
	inbound.FinalizeInboundContext(msgCtx)

	// Fire message.receive hook after parsing, before dispatch (shell + internal).
	{
		env := map[string]string{
			"DENEB_CHANNEL":     "telegram",
			"DENEB_CHAT_ID":     chatID,
			"DENEB_MESSAGE":     msgText,
			"DENEB_SESSION_KEY": sessionKey,
		}
		if p.server.hooks != nil {
			p.server.safeGo("hooks:message.receive", func() {
				p.server.hooks.Fire(context.Background(), hooks.EventMessageReceive, env)
			})
		}
		if p.server.internalHooks != nil {
			p.server.safeGo("internal-hooks:message.receive", func() {
				p.server.internalHooks.TriggerFromEvent(context.Background(), hooks.EventMessageReceive, sessionKey, env)
			})
		}
	}

	// --- Part A: Ack reaction — send 👀 to acknowledge the incoming message.
	var didAck bool
	{
		client := p.server.telegramPlug.Client()
		if client != nil {
			chatIDInt, _ := telegram.ParseChatID(chatID)
			if err := client.SetMessageReaction(context.Background(), chatIDInt, msg.MessageID, "👀"); err == nil {
				didAck = true
			}
		}
	}

	// --- Part B: Conversation label — resolve a display label for this session.
	convLabel := senderName
	if convLabel == "" && msg.Chat.Title != "" {
		convLabel = msg.Chat.Title
	}
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

	// Quick commands with inline keyboard or formatted response.
	{
		bareCmd := strings.ToLower(strings.TrimSpace(msgCtx.BodyForCommands))
		// Strip @bot suffix for matching.
		if atIdx := strings.IndexByte(bareCmd, '@'); atIdx >= 0 {
			bareCmd = bareCmd[:atIdx]
		}
		switch bareCmd {
		case "/models":
			p.handleModelsCommand(chatID)
			return
		case "/status", "/dashboard", "/d", "/ws":
			p.handleStatusDashboardCommand(chatID, sessionKey)
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
		Agent:           executor,
		Registry:        p.cmdRegistry,
		Router:          p.cmdRouter,
		CommandDeps:     p.buildCommandDeps(sessionKey),
		History:         p.server.historyTracker,
		AbortMemory:     p.server.abortMemory,
		ModelCandidates: p.buildModelCandidates(),
		SessionFunc: func(key string) *types.SessionState {
			return &types.SessionState{
				SessionOrigin: types.SessionOrigin{
					SessionKey: key,
					Channel:    "telegram",
					IsGroup:    msgCtx.IsGroup,
				},
			}
		},
		OnSessionEvent: func(eventType, sessKey, reason string) {
			if p.server.pluginTypedHookRunner != nil {
				go p.server.pluginTypedHookRunner.RunVoidHook(
					context.Background(), plugin.HookBeforeReset, map[string]any{
						"type":       eventType,
						"sessionKey": sessKey,
						"reason":     reason,
						"channel":    "telegram",
						"ts":         time.Now().UnixMilli(),
					})
			}
		},
		ThinkingRuntime: p.server.thinkingRuntime,
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
		// Deliver reply payloads through the full streaming pipeline for
		// sequential ordering, deduplication, and coalescing.
		pipeline := streaming.NewBlockReplyPipelineFull(context.Background(), streaming.BlockReplyPipelineConfig{
			OnBlockReply: func(ctx context.Context, payload types.ReplyPayload) error {
				if payload.Text != "" {
					p.sendCommandReply(chatID, &handlers.CommandResult{Reply: payload.Text})
				}
				return nil
			},
			TimeoutMs: 10000,
			Logger:    p.logger,
		})
		for _, payload := range dispatchResult.Payloads {
			pipeline.Enqueue(payload)
		}
		pipeline.FlushAndWait(true)
		pipeline.Stop()
	}

	// Remove ack reaction after the reply is sent — but only when the agent
	// was NOT invoked. When the agent runs, StatusReactionController manages
	// the full emoji lifecycle (👀 → 🤔 → 🔥 → 👍/😱) and clearing here
	// would wipe the terminal emoji, causing visible flicker.
	if didAck && !executor.didSend {
		if client := p.server.telegramPlug.Client(); client != nil {
			chatIDInt, _ := telegram.ParseChatID(chatID)
			if err := client.SetMessageReaction(context.Background(), chatIDInt, msg.MessageID, ""); err != nil {
				p.logger.Warn("failed to remove ack reaction", "error", err)
			}
		}
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

	// Fire message.receive hook for the media group (shell + internal).
	{
		caption := media.MessageText(first)
		env := map[string]string{
			"DENEB_CHANNEL":     "telegram",
			"DENEB_CHAT_ID":     chatID,
			"DENEB_MESSAGE":     caption,
			"DENEB_SESSION_KEY": sessionKey,
		}
		if p.server.hooks != nil {
			p.server.safeGo("hooks:message.receive", func() {
				p.server.hooks.Fire(context.Background(), hooks.EventMessageReceive, env)
			})
		}
		if p.server.internalHooks != nil {
			p.server.safeGo("internal-hooks:message.receive", func() {
				p.server.internalHooks.TriggerFromEvent(context.Background(), hooks.EventMessageReceive, sessionKey, env)
			})
		}
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
		"sessionKey":  sessionKey,
		"message":     agentMessage,
		"delivery":    delivery,
		"clientRunId": shortid.New("run"),
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

	var zeroCallsFn func() *handlers.RPCZeroCallsReport
	if p.server.dispatcher != nil {
		disp := p.server.dispatcher
		zeroCallsFn = func() *handlers.RPCZeroCallsReport {
			return buildZeroCallsReport(disp)
		}
	}

	return &handlers.CommandDeps{Status: sd, SubagentRuns: subagentRunsFn, ZeroCallsFn: zeroCallsFn}
}

// buildModelCandidates converts the model role registry into autoreply
// ModelCandidates for directive-based model resolution (/model, !model).
func (p *InboundProcessor) buildModelCandidates() []model.ModelCandidate {
	reg := p.server.modelRegistry
	if reg == nil {
		return nil
	}
	configured := reg.ConfiguredModels()
	seen := make(map[string]bool)
	var candidates []model.ModelCandidate
	for role, cfg := range configured {
		if cfg.Model == "" {
			continue
		}
		fullID := cfg.ProviderID + "/" + cfg.Model
		if seen[fullID] {
			continue
		}
		seen[fullID] = true
		candidates = append(candidates, model.ModelCandidate{
			Provider: cfg.ProviderID,
			Model:    cfg.Model,
			Label:    string(role),
		})
	}
	return candidates
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
		"sessionKey":  cfg.SessionKey,
		"message":     cfg.Message,
		"delivery":    delivery,
		"clientRunId": shortid.New("run"),
	}
	if cfg.Model != "" {
		sendParams["model"] = cfg.Model
	}
	if len(e.attachments) > 0 {
		sendParams["attachments"] = e.attachments
	}
	if cfg.DeepWork {
		sendParams["deepWork"] = true
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

	// Fire message.receive hook for callback query (shell + internal).
	{
		env := map[string]string{
			"DENEB_CHANNEL":     "telegram",
			"DENEB_CHAT_ID":     chatID,
			"DENEB_MESSAGE":     cb.Data,
			"DENEB_SESSION_KEY": sessionKey,
		}
		if p.server.hooks != nil {
			p.server.safeGo("hooks:message.receive", func() {
				p.server.hooks.Fire(context.Background(), hooks.EventMessageReceive, env)
			})
		}
		if p.server.internalHooks != nil {
			p.server.safeGo("internal-hooks:message.receive", func() {
				p.server.internalHooks.TriggerFromEvent(context.Background(), hooks.EventMessageReceive, sessionKey, env)
			})
		}
	}

	// Intercept model quick-change callbacks — handle immediately without agent.
	if action, payload := telegram.ParseCallbackData(cb.Data); action == telegram.ActionModelSwitch {
		p.handleModelSwitchCallback(cb, chatID, payload)
		return
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
			"sessionKey":  sessionKey,
			"message":     agentMessage,
			"clientRunId": shortid.New("run"),
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
		cfg := subagentpkg.ACPCommandDepsConfig{
			Infra: p.server.acpDeps.Infra,
		}
		// Wire SessionSendFn from the ACP deps when available so that
		// /subagents send, /steer, and spawn initial-message delivery work.
		if p.server.acpDeps.SessionSendFn != nil {
			cfg.SessionSendFn = p.server.acpDeps.SessionSendFn
		}
		// Wire SessionBindings so /focus, /unfocus, and /agents commands
		// can resolve and mutate conversation-to-session bindings.
		if p.server.acpDeps.Bindings != nil {
			cfg.SessionBindings = p.server.acpDeps.Bindings
		}
		// Wire TranscriptLoader so /subagents log can display session history.
		if p.server.acpDeps.TranscriptLoader != nil {
			loader := p.server.acpDeps.TranscriptLoader
			cfg.TranscriptLoader = func(sessionKey string, limit int) ([]subagentpkg.ChatLogMessage, error) {
				roles, contents, err := loader(sessionKey, limit)
				if err != nil {
					return nil, err
				}
				msgs := make([]subagentpkg.ChatLogMessage, len(roles))
				for i := range roles {
					msgs[i] = subagentpkg.ChatLogMessage{Role: roles[i], Content: contents[i]}
				}
				return msgs, nil
			}
		}
		deps = subagentpkg.NewSubagentCommandDepsFromACP(
			p.server.acpDeps.Registry, cfg,
		)
	}
	return subagentpkg.HandleSubagentsCommand(
		normalized, sessionKey, channelName, accountID, threadID,
		senderID, isGroup, true, // isAuthorized: single-user deployment
		deps,
	)
}

// buildZeroCallsReport cross-references registered RPC methods with
// RPCRequestsTotal to find methods that have never been called.
func buildZeroCallsReport(disp *rpc.Dispatcher) *handlers.RPCZeroCallsReport {
	methods := disp.Methods()
	sort.Strings(methods)

	counts := metrics.RPCRequestsTotal.Snapshot()

	var zeroCalls []string
	for _, m := range methods {
		okKey := m + "\x00" + "ok"
		errKey := m + "\x00" + "error"
		if counts[okKey]+counts[errKey] == 0 {
			zeroCalls = append(zeroCalls, m)
		}
	}

	return &handlers.RPCZeroCallsReport{
		ZeroCalls:    zeroCalls,
		TotalMethods: len(methods),
	}
}

// modelEntry describes a model shown in the /models quick-change keyboard.
type modelEntry struct {
	label   string // button label (e.g., "main: glm-5-turbo")
	fullID  string // full model ID sent to LLM (e.g., "zai/glm-5-turbo")
	display string // short display name (e.g., "glm-5-turbo")
}

// quickChangeModels returns the ordered list of models for the /models keyboard.
// Includes role-based models from the registry + extra frequently-used models.
func (p *InboundProcessor) quickChangeModels() []modelEntry {
	var entries []modelEntry

	// 1. Role-based models from registry.
	if reg := p.server.modelRegistry; reg != nil {
		roles := []struct {
			role  modelrole.Role
			label string
		}{
			{modelrole.RoleMain, "main"},
			{modelrole.RoleLightweight, "lightweight"},
			{modelrole.RolePilot, "pilot"},
			{modelrole.RoleFallback, "fallback"},
		}
		seen := make(map[string]bool)
		for _, r := range roles {
			cfg := reg.Config(r.role)
			if cfg.Model == "" {
				continue
			}
			fullID := reg.FullModelID(r.role)
			seen[fullID] = true
			entries = append(entries, modelEntry{
				label:   r.label + ": " + shortModelName(cfg.Model),
				fullID:  fullID,
				display: shortModelName(cfg.Model),
			})
		}

		// 2. Extra models not already covered by roles.
		extras := []struct {
			provider string
			model    string
		}{
			{"zai", "glm-5v-turbo"},
			{"zai", "glm-5.1"},
		}
		for _, e := range extras {
			fullID := e.provider + "/" + e.model
			if seen[fullID] {
				continue
			}
			entries = append(entries, modelEntry{
				label:   e.model,
				fullID:  fullID,
				display: e.model,
			})
		}
	}

	return entries
}

// shortModelName strips the provider prefix from a model name.
func shortModelName(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx < len(model)-1 {
		return model[idx+1:]
	}
	return model
}

// handleModelsCommand sends a model quick-change message with an inline keyboard.
func (p *InboundProcessor) handleModelsCommand(chatID string) {
	entries := p.quickChangeModels()
	if len(entries) == 0 {
		p.sendCommandReply(chatID, &handlers.CommandResult{Reply: "모델 레지스트리를 사용할 수 없습니다.", SkipAgent: true})
		return
	}

	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	currentModel := p.chatHandler.DefaultModel()
	if currentModel == "" && p.server.modelRegistry != nil {
		currentModel = p.server.modelRegistry.FullModelID(modelrole.RoleMain)
	}

	text := "🤖 <b>모델 퀵체인지</b>\n\n"
	text += "현재: <code>" + currentModel + "</code>"

	keyboard := p.buildModelKeyboard(entries, currentModel)

	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := telegram.SendText(ctx, client, id, text, telegram.SendOptions{
		ParseMode: "HTML",
		Keyboard:  keyboard,
	}); err != nil {
		p.logger.Warn("failed to send models command reply", "error", err)
	}
}

// buildModelKeyboard builds a 2-column inline keyboard from model entries.
func (p *InboundProcessor) buildModelKeyboard(entries []modelEntry, currentModel string) *telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	var row []telegram.InlineKeyboardButton
	for i, e := range entries {
		label := e.label
		if e.fullID == currentModel {
			label = "✓ " + label
		}

		row = append(row, telegram.InlineKeyboardButton{
			Text:         label,
			CallbackData: telegram.ActionModelSwitch + ":" + e.fullID,
		})

		if len(row) == 2 || i == len(entries)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	return telegram.BuildInlineKeyboard(rows)
}

// handleModelSwitchCallback processes a model quick-change button press.
func (p *InboundProcessor) handleModelSwitchCallback(cb *telegram.CallbackQuery, chatID string, fullModelID string) {
	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	// Apply model change.
	p.chatHandler.SetDefaultModel(fullModelID)

	// Persist to deneb.json so the choice survives restarts.
	go func() {
		cfgPath := config.ResolveConfigPath()
		if err := config.PersistDefaultModel(cfgPath, fullModelID, p.logger); err != nil {
			p.logger.Warn("failed to persist model choice", "model", fullModelID, "error", err)
		}
	}()

	displayModel := shortModelName(fullModelID)

	// Acknowledge with toast.
	ackCtx, ackCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ackCancel()
	if err := telegram.AnswerCallbackQuery(ackCtx, client, cb.ID, "✓ "+displayModel); err != nil {
		p.logger.Warn("failed to answer model switch callback", "error", err)
	}

	// Edit original message to update the checkmark.
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	text := "🤖 <b>모델 퀵체인지</b>\n\n"
	text += "현재: <code>" + fullModelID + "</code>"

	entries := p.quickChangeModels()
	keyboard := p.buildModelKeyboard(entries, fullModelID)

	editCtx, editCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer editCancel()
	if _, err := telegram.EditMessageText(editCtx, client, id, cb.Message.MessageID, text, "HTML", keyboard); err != nil {
		p.logger.Warn("failed to edit model switch message", "error", err)
	}
}

// handleStatusDashboardCommand sends a combined gateway + session status message.
func (p *InboundProcessor) handleStatusDashboardCommand(chatID, sessionKey string) {
	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	var b strings.Builder
	b.Grow(1024)

	b.WriteString("<b>📊 상태 대시보드</b>\n")
	b.WriteString("──────────────────\n\n")

	// Gateway info.
	if p.server.version != "" {
		b.WriteString("🖥️ <b>Gateway:</b> v")
		b.WriteString(html.EscapeString(p.server.version))
		if !p.server.startedAt.IsZero() {
			b.WriteString(" | Uptime: ")
			b.WriteString(formatDashboardUptime(time.Since(p.server.startedAt)))
		}
		b.WriteByte('\n')
	}

	// Rust FFI status.
	rustIcon := "❌"
	if p.server.rustFFI {
		rustIcon = "✅"
	}
	b.WriteString("🔧 <b>Rust Core:</b> ")
	b.WriteString(rustIcon)

	// Session count.
	if p.server.sessions != nil {
		fmt.Fprintf(&b, " | Sessions: %d", p.server.sessions.Count())
	}

	// WS connections.
	fmt.Fprintf(&b, " | WS: %d\n", p.server.clientCnt.Load())
	b.WriteByte('\n')

	// Current session info.
	b.WriteString("<b>📋 세션</b>\n")
	sess := p.server.sessions.Get(sessionKey)
	if sess != nil {
		statusIcon := "🟢"
		switch sess.Status {
		case session.StatusRunning:
			statusIcon = "🔄"
		case session.StatusFailed:
			statusIcon = "❌"
		case session.StatusKilled:
			statusIcon = "⛔"
		case session.StatusTimeout:
			statusIcon = "⏰"
		}
		fmt.Fprintf(&b, "%s <b>상태:</b> %s\n", statusIcon, string(sess.Status))
	}

	// Current model.
	currentModel := p.chatHandler.DefaultModel()
	if currentModel == "" && p.server.modelRegistry != nil {
		currentModel = p.server.modelRegistry.FullModelID(modelrole.RoleMain)
	}
	if currentModel != "" {
		b.WriteString("🤖 <b>모델:</b> <code>")
		b.WriteString(html.EscapeString(currentModel))
		b.WriteString("</code>\n")
	}

	// Mode settings.
	if sess != nil {
		var modes []string
		if sess.ThinkingLevel != "" && sess.ThinkingLevel != "off" {
			modes = append(modes, "Think: "+sess.ThinkingLevel)
		}
		if sess.FastMode != nil && *sess.FastMode {
			modes = append(modes, "Fast: on")
		}
		if sess.ReasoningLevel != "" && sess.ReasoningLevel != "off" {
			modes = append(modes, "Reasoning: "+sess.ReasoningLevel)
		}
		if sess.ElevatedLevel != "" && sess.ElevatedLevel != "off" {
			modes = append(modes, "Elevated: "+sess.ElevatedLevel)
		}
		if sess.ToolPreset != "" {
			modes = append(modes, "Preset: "+sess.ToolPreset)
		}
		if len(modes) > 0 {
			b.WriteString("⚙️ <b>모드:</b> ")
			b.WriteString(html.EscapeString(strings.Join(modes, " | ")))
			b.WriteByte('\n')
		}

		// Token usage.
		if sess.TotalTokens != nil && *sess.TotalTokens > 0 {
			in, out := int64(0), int64(0)
			if sess.InputTokens != nil {
				in = *sess.InputTokens
			}
			if sess.OutputTokens != nil {
				out = *sess.OutputTokens
			}
			fmt.Fprintf(&b, "📊 <b>토큰:</b> %s (in: %s, out: %s)\n",
				formatDashboardTokens(*sess.TotalTokens),
				formatDashboardTokens(in),
				formatDashboardTokens(out))
		}

		// Failure reason.
		if sess.FailureReason != "" {
			b.WriteString("⚠️ <b>마지막 오류:</b> ")
			b.WriteString(html.EscapeString(sess.FailureReason))
			b.WriteByte('\n')
		}
	}

	b.WriteByte('\n')

	// Per-provider API usage.
	if p.server.usageTracker != nil {
		report := p.server.usageTracker.Status()
		if report != nil && len(report.Providers) > 0 {
			b.WriteString("<b>📈 API 사용량</b>\n")
			names := make([]string, 0, len(report.Providers))
			for name := range report.Providers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				ps := report.Providers[name]
				total := ps.Tokens.Input + ps.Tokens.Output
				fmt.Fprintf(&b, "  %s — %s회, %s tokens\n",
					html.EscapeString(name),
					formatDashboardTokens(ps.Calls),
					formatDashboardTokens(total))
			}
			b.WriteByte('\n')
		}
	}

	// Channel health.
	if p.server.channelHealth != nil {
		snapshot := p.server.channelHealth.HealthSnapshot()
		if len(snapshot) > 0 {
			b.WriteString("<b>📡 채널</b>\n")
			for _, ch := range snapshot {
				icon := "💚"
				status := "정상"
				if !ch.Healthy {
					icon = "❌"
					status = "비정상"
					if ch.Reason != "" {
						status = ch.Reason
					}
				}
				fmt.Fprintf(&b, "  %s %s: %s\n", icon,
					html.EscapeString(ch.ChannelID),
					html.EscapeString(status))
			}
		}
	}

	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := telegram.SendText(ctx, client, id, b.String(), telegram.SendOptions{
		ParseMode: "HTML",
	}); err != nil {
		p.logger.Warn("failed to send status dashboard", "error", err)
	}
}

// formatDashboardUptime formats a duration as compact uptime (e.g. "2d 5h 32m").
func formatDashboardUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// formatDashboardTokens formats token counts in compact form (e.g. "1.2M", "890K").
func formatDashboardTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
