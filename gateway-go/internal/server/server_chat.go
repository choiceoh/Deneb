package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

func (s *Server) initGmailPoll() {
	snap, err := config.LoadConfigFromDefaultPath()
	if err != nil || snap == nil {
		return
	}
	pollCfg := snap.Config.GmailPoll
	if pollCfg == nil || pollCfg.Enabled == nil || !*pollCfg.Enabled {
		return
	}

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".deneb")

	cfg := gmailpoll.Config{
		StateDir:   stateDir,
		LLMBaseURL: modelrole.DefaultSglangBaseURL,
		Model:      resolveDefaultModel(s.logger), // use main model by default
	}
	if pollCfg.IntervalMin != nil {
		cfg.IntervalMin = *pollCfg.IntervalMin
	}
	if pollCfg.Query != "" {
		cfg.Query = pollCfg.Query
	}
	if pollCfg.MaxPerCycle != nil {
		cfg.MaxPerCycle = *pollCfg.MaxPerCycle
	}
	if pollCfg.Model != "" {
		cfg.Model = pollCfg.Model // explicit override from config
	}
	if pollCfg.PromptFile != "" {
		cfg.PromptFile = pollCfg.PromptFile
	}

	s.gmailPollSvc = gmailpoll.NewService(cfg, s.logger)

	// Wire Telegram notifier.
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			s.gmailPollSvc.SetNotifier(&telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			})
		}
	}

	// Register as a periodic task within the autonomous service.
	// The autonomous service handles lifecycle, panic recovery, and scheduling.
	if s.autonomousSvc != nil {
		s.autonomousSvc.RegisterTask(s.gmailPollSvc)
		s.logger.Info("gmailpoll registered with autonomous service",
			"interval", fmt.Sprintf("%dm", cfg.IntervalMin))
	} else {
		s.logger.Warn("gmailpoll: autonomous service not available, polling disabled")
	}
}

// registerNativeSystemMethods registers native Go system RPC methods:
// usage, logs, doctor, maintenance, update.

func (s *Server) wireTelegramChatHandler() {
	// Recent-send dedup cache: prevents the same text from being delivered
	// to the same chat twice within a short window (e.g. when the LLM uses
	// the message tool AND also produces a text response without NO_REPLY).
	var recentMu sync.Mutex
	recentSends := make(map[string]time.Time) // key: "chatID:text[:200]"
	const recentTTL = 10 * time.Second

	// Set reply function: delivers assistant responses back to Telegram.
	s.chatHandler.SetReplyFunc(func(ctx context.Context, delivery *chat.DeliveryContext, text string) error {
		if delivery == nil || delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return fmt.Errorf("telegram client not connected")
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}

		// Dedup: skip if the same text was sent to this chat recently.
		dedupKey := delivery.To + ":" + truncateForDedup(text, 200)
		recentMu.Lock()
		if sentAt, dup := recentSends[dedupKey]; dup && time.Since(sentAt) < recentTTL {
			recentMu.Unlock()
			s.logger.Info("suppressed duplicate reply to telegram",
				"chatId", delivery.To, "textLen", len(text))
			return nil
		}
		// Evict stale entries (cheap, single-user so map stays tiny).
		for k, t := range recentSends {
			if time.Since(t) >= recentTTL {
				delete(recentSends, k)
			}
		}
		recentSends[dedupKey] = time.Now()
		recentMu.Unlock()

		// Parse optional button directive from agent reply.
		cleanText, keyboard := parseReplyButtons(text)
		opts := telegram.SendOptions{ParseMode: "HTML", Keyboard: keyboard}
		html := telegram.MarkdownToTelegramHTML(cleanText)
		_, err = telegram.SendText(ctx, client, chatID, html, opts)
		return err
	})

	// Set media send function: delivers files back to Telegram.
	s.chatHandler.SetMediaSendFunc(func(ctx context.Context, delivery *chat.DeliveryContext, filePath, mediaType, caption string, silent bool) error {
		if delivery == nil {
			return nil
		}

		if delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return fmt.Errorf("telegram client not connected")
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}

		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		fileName := filepath.Base(filePath)
		opts := telegram.SendOptions{DisableNotification: silent}

		switch mediaType {
		case "photo":
			if telegram.ValidatePhotoMetadata(f) {
				_, err = telegram.UploadPhoto(ctx, client, chatID, fileName, f, caption, opts)
				if err != nil {
					// Telegram rejected the photo (e.g. format issue, server-side resize
					// failure). Seek back and retry as a document so the file is not lost.
					s.logger.Warn("uploadPhoto failed, falling back to document",
						"file", fileName, "error", err)
					if _, seekErr := f.Seek(0, 0); seekErr == nil {
						_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
					}
				}
			} else {
				// Metadata check failed (unsupported format, oversized dimensions, bad
				// aspect ratio) — skip sendPhoto and send directly as a document.
				s.logger.Info("photo metadata invalid, sending as document", "file", fileName)
				_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
			}
		case "video":
			// Upload as document — Telegram sendVideo requires a URL/file_id, not multipart.
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		case "audio":
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		case "voice":
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		default: // "document" or unknown
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		}
		return err
	})

	// Set typing indicator function: sends "typing" chat action to Telegram
	// periodically during agent runs so the user sees "typing..." in the chat.
	s.chatHandler.SetTypingFunc(func(ctx context.Context, delivery *chat.DeliveryContext) error {
		if delivery == nil {
			return nil
		}

		if delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return nil
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		return client.SendChatAction(ctx, chatID, "typing")
	})

	// Set reaction function: sets emoji reactions on the user's triggering message
	// to show agent status phases (👀→🤔→🔥→👍).
	s.chatHandler.SetReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
		if delivery == nil || delivery.Channel != "telegram" || delivery.MessageID == "" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return nil
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		msgID, err := strconv.ParseInt(delivery.MessageID, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid message ID %q: %w", delivery.MessageID, err)
		}
		return client.SetMessageReaction(ctx, chatID, msgID, emoji)
	})

	// Create the inbound processor that routes Telegram messages through
	// the autoreply command/directive pipeline before dispatching to chat.send.
	inbound := NewInboundProcessor(s)

	// Set update handler: routes through autoreply preprocessing → chat.send.
	s.telegramPlug.SetHandler(func(_ context.Context, update *telegram.Update) {
		inbound.HandleTelegramUpdate(update)
	})

	// Register Telegram's per-channel upload limit for the send_file tool.
	s.chatHandler.SetChannelUploadLimit("telegram", s.telegramPlug.MaxUploadBytes())

	s.logger.Info("telegram chat handler wired (with autoreply preprocessing)")
}

// loadTelegramConfig extracts Telegram channel config from deneb.json.
// Returns nil if Telegram is not configured.
func loadTelegramConfig(_ *config.GatewayRuntimeConfig) *telegram.Config {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid {
		return nil
	}

	// Extract channels.telegram from raw config JSON.
	if snapshot.Raw == "" {
		return nil
	}

	var root struct {
		Channels struct {
			Telegram *telegram.Config `json:"telegram"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		return nil
	}
	return root.Channels.Telegram
}

// wireDiscordChatHandler connects the Discord Gateway message handler to the
// chat handler for coding-focused agent sessions. It wraps the existing
// channel handlers so both Telegram and Discord can coexist.
func (s *Server) wireDiscordChatHandler() {
	// Initialize auto thread namer using the lightweight model (local sglang).
	discordCfg := s.discordPlug.Config()
	if discordCfg.AutoThreadNamesEnabled() && s.chatHandler != nil && s.chatHandler.ModelRegistry() != nil {
		reg := s.chatHandler.ModelRegistry()
		lwClient := reg.Client(modelrole.RoleLightweight)
		lwModel := reg.Model(modelrole.RoleLightweight)
		s.discordThreadNamer = discord.NewThreadNamer(lwClient, lwModel)
		s.logger.Info("discord: auto thread naming enabled", "model", lwModel)
	}

	// Recent-send dedup cache.
	var recentMu sync.Mutex
	recentSends := make(map[string]time.Time)
	const recentTTL = 10 * time.Second

	// Chain reply function: wraps existing replyFunc to add Discord support.
	prevReply := s.chatHandler.ReplyFunc()
	s.chatHandler.SetReplyFunc(func(ctx context.Context, delivery *chat.DeliveryContext, text string) error {
		if delivery == nil || delivery.Channel != "discord" {
			if prevReply != nil {
				return prevReply(ctx, delivery, text)
			}
			return nil
		}
		client := s.discordPlug.Client()
		if client == nil {
			return fmt.Errorf("discord client not connected")
		}

		// Dedup.
		dedupKey := delivery.To + ":" + truncateForDedup(text, 200)
		recentMu.Lock()
		if sentAt, dup := recentSends[dedupKey]; dup && time.Since(sentAt) < recentTTL {
			recentMu.Unlock()
			return nil
		}
		for k, t := range recentSends {
			if time.Since(t) >= recentTTL {
				delete(recentSends, k)
			}
		}
		recentSends[dedupKey] = time.Now()
		recentMu.Unlock()

		// Smart formatting: extract large code blocks and send as file attachments.
		formatted := discord.FormatReply(text)
		if formatted.FileContent != nil {
			// Send file attachment with summary text.
			_, err := client.SendMessageWithFile(ctx, delivery.To,
				formatted.Text, formatted.FileName, formatted.FileContent)
			return err
		}

		// Detect code changes in reply for actionable buttons.
		var components []discord.Component
		if containsCodeChange(text) && delivery.To != "" {
			sessionKey := "discord:" + delivery.To
			components = discord.CodeActionButtons(sessionKey)
		}

		// If we have components, use SendMessage with components; otherwise plain text.
		if len(components) > 0 {
			chunks := discord.ChunkText(formatted.Text, discord.TextChunkLimit)
			for i, chunk := range chunks {
				req := &discord.SendMessageRequest{
					Content:         chunk,
					AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
				}
				// Attach buttons only to the last chunk.
				if i == len(chunks)-1 {
					req.Components = components
				}
				if i == 0 && delivery.MessageID != "" {
					req.MessageReference = &discord.MessageReference{MessageID: delivery.MessageID}
				}
				if _, err := client.SendMessage(ctx, delivery.To, req); err != nil {
					return err
				}
			}
			return nil
		}

		_, err := discord.SendText(ctx, client, delivery.To, formatted.Text, delivery.MessageID)
		return err
	})

	// Chain media send function.
	prevMedia := s.chatHandler.MediaSendFunc()
	s.chatHandler.SetMediaSendFunc(func(ctx context.Context, delivery *chat.DeliveryContext, filePath, mediaType, caption string, silent bool) error {
		if delivery == nil || delivery.Channel != "discord" {
			if prevMedia != nil {
				return prevMedia(ctx, delivery, filePath, mediaType, caption, silent)
			}
			return nil
		}
		client := s.discordPlug.Client()
		if client == nil {
			return fmt.Errorf("discord client not connected")
		}

		fileData, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}

		fileName := filepath.Base(filePath)
		_, err = client.SendMessageWithFile(ctx, delivery.To, caption, fileName, fileData)
		return err
	})

	// Chain typing indicator with throttling.
	// Discord typing expires after 10s; throttle to avoid excessive API calls.
	var lastTypingMu sync.Mutex
	lastTypingAt := make(map[string]time.Time) // channelID → last typing time
	const typingThrottle = 8 * time.Second     // refresh before 10s expiry

	prevTyping := s.chatHandler.TypingFunc()
	s.chatHandler.SetTypingFunc(func(ctx context.Context, delivery *chat.DeliveryContext) error {
		if delivery == nil || delivery.Channel != "discord" {
			if prevTyping != nil {
				return prevTyping(ctx, delivery)
			}
			return nil
		}

		// Throttle: skip if last typing was recent.
		lastTypingMu.Lock()
		if last, ok := lastTypingAt[delivery.To]; ok && time.Since(last) < typingThrottle {
			lastTypingMu.Unlock()
			return nil
		}
		lastTypingAt[delivery.To] = time.Now()
		lastTypingMu.Unlock()

		client := s.discordPlug.Client()
		if client == nil {
			return nil
		}
		return client.TriggerTyping(ctx, delivery.To)
	})

	// Chain reaction function.
	prevReaction := s.chatHandler.ReactionFunc()
	s.chatHandler.SetReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
		if delivery == nil || delivery.Channel != "discord" || delivery.MessageID == "" {
			if prevReaction != nil {
				return prevReaction(ctx, delivery, emoji)
			}
			return nil
		}
		client := s.discordPlug.Client()
		if client == nil {
			return nil
		}
		return client.CreateReaction(ctx, delivery.To, delivery.MessageID, emoji)
	})

	// Chain remove reaction function (Discord additive reactions need explicit removal).
	prevRemoveReaction := s.chatHandler.RemoveReactionFunc()
	s.chatHandler.SetRemoveReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
		if delivery == nil || delivery.Channel != "discord" || delivery.MessageID == "" {
			if prevRemoveReaction != nil {
				return prevRemoveReaction(ctx, delivery, emoji)
			}
			return nil
		}
		client := s.discordPlug.Client()
		if client == nil {
			return nil
		}
		return client.DeleteOwnReaction(ctx, delivery.To, delivery.MessageID, emoji)
	})

	// Wire tool progress tracking for Discord: when the agent executes tools,
	// update a progress embed in-place to show real-time execution status.
	var progressMu sync.Mutex
	activeTrackers := make(map[string]*discord.ProgressTracker) // deliveryTarget → tracker

	prevProgress := s.chatHandler.ToolProgressFunc()
	s.chatHandler.SetToolProgressFunc(func(ctx context.Context, delivery *chat.DeliveryContext, event chat.ToolProgressEvent) {
		if delivery == nil || delivery.Channel != "discord" {
			if prevProgress != nil {
				prevProgress(ctx, delivery, event)
			}
			return
		}
		dcClient := s.discordPlug.Client()
		if dcClient == nil {
			return
		}

		progressMu.Lock()
		tracker := activeTrackers[delivery.To]
		if tracker == nil && event.Type == "start" {
			// Create progress tracker on first tool start.
			tracker = discord.NewProgressTracker(ctx, dcClient, delivery.To)
			if tracker != nil {
				activeTrackers[delivery.To] = tracker
			}
		}
		progressMu.Unlock()

		if tracker == nil {
			return
		}

		switch event.Type {
		case "start":
			tracker.StartStep(ctx, event.Name)
		case "complete":
			tracker.CompleteStep(ctx, event.Name, event.IsError)
			// Check if all steps are done to finalize.
			// Finalize is called lazily; the agent reply will come separately.
		}
	})

	// Hook into reply func to finalize progress trackers when the agent responds.
	prevReplyForProgress := s.chatHandler.ReplyFunc()
	s.chatHandler.SetReplyFunc(func(ctx context.Context, delivery *chat.DeliveryContext, text string) error {
		// Finalize any active progress tracker for this Discord target.
		if delivery != nil && delivery.Channel == "discord" {
			progressMu.Lock()
			if tracker := activeTrackers[delivery.To]; tracker != nil {
				tracker.Finalize(ctx)
				delete(activeTrackers, delivery.To)
			}
			progressMu.Unlock()
		}
		if prevReplyForProgress != nil {
			return prevReplyForProgress(ctx, delivery, text)
		}
		return nil
	})

	// Route Discord messages through the autoreply inbound processor for
	// command detection (/reset, /model, etc.) and directive parsing before
	// dispatching to the chat pipeline.
	inbound := NewInboundProcessor(s)
	s.discordPlug.SetHandler(func(_ context.Context, msg *discord.Message) {
		inbound.HandleDiscordMessage(msg)
	})

	// Wire interaction handler for button clicks and slash commands.
	s.discordPlug.SetInteractionHandler(func(ctx context.Context, interaction *discord.Interaction) {
		inbound.HandleDiscordInteraction(ctx, interaction)
	})

	// Register Discord's per-channel upload limit for the send_file tool.
	s.chatHandler.SetChannelUploadLimit("discord", s.discordPlug.MaxUploadBytes())

	s.logger.Info("discord chat handler wired (coding channel)")
}

// containsCodeChange detects whether an agent reply includes code modifications
// (diffs, file edits) that warrant follow-up action buttons.
func containsCodeChange(text string) bool {
	indicators := []string{
		"```diff",
		"--- a/",
		"+++ b/",
		"파일을 수정했습니다",
		"파일을 생성했습니다",
		"wrote to",
		"edited",
		"applied edit",
	}
	lower := strings.ToLower(text)
	for _, ind := range indicators {
		if strings.Contains(lower, strings.ToLower(ind)) {
			return true
		}
	}
	return false
}

// loadDiscordConfig extracts Discord channel config from deneb.json.
// Returns nil if Discord is not configured.
func loadDiscordConfig(_ *config.GatewayRuntimeConfig) *discord.Config {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid {
		return nil
	}

	if snapshot.Raw == "" {
		return nil
	}

	var root struct {
		Channels struct {
			Discord *discord.Config `json:"discord"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		return nil
	}
	return root.Channels.Discord
}

// loadProviderConfigs reads LLM provider configs (apiKey, baseUrl, api) from deneb.json.
func loadProviderConfigs(logger *slog.Logger) map[string]chat.ProviderConfig {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return nil
	}

	var root struct {
		Models struct {
			Providers map[string]chat.ProviderConfig `json:"providers"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse provider configs", "error", err)
		return nil
	}

	if len(root.Models.Providers) > 0 {
		logger.Info("loaded provider configs", "count", len(root.Models.Providers))
	}
	return root.Models.Providers
}

// resolveDefaultModel reads agents.defaultModel or agents.defaults.model from
// deneb.json, falling back to the registry's main model default.
// The model field can be either a string ("model-name") or an object
// ({"primary": "model-name", "fallbacks": [...]}).
func resolveDefaultModel(logger *slog.Logger) string {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return "" // empty: registry will provide the default
	}
	var root struct {
		Agents struct {
			DefaultModel string          `json:"defaultModel"`
			Defaults     json.RawMessage `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse agents config for model", "error", err)
		return "" // empty: registry will provide the default
	}
	if root.Agents.DefaultModel != "" {
		return root.Agents.DefaultModel
	}
	if len(root.Agents.Defaults) > 0 {
		model := extractModelFromDefaults(root.Agents.Defaults)
		if model != "" {
			return model
		}
	}
	return "" // empty: registry will provide the default
}

// extractModelFromDefaults handles both string and object forms of the model field.
func extractModelFromDefaults(raw json.RawMessage) string {
	var defaults struct {
		Model json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal(raw, &defaults); err != nil || len(defaults.Model) == 0 {
		return ""
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(defaults.Model, &s); err == nil && s != "" {
		return s
	}
	// Try object with primary field.
	var obj struct {
		Primary string `json:"primary"`
	}
	if err := json.Unmarshal(defaults.Model, &obj); err == nil && obj.Primary != "" {
		return obj.Primary
	}
	return ""
}

// resolveWorkspaceDir determines the workspace directory for file tool operations.
// Reads agents.defaults.workspace / agents.list[].workspace from config,
// falling back to ~/.deneb/workspace (matching TS resolveAgentWorkspaceDir).
func resolveWorkspaceDir() string {
	snap, err := config.LoadConfigFromDefaultPath()
	if err == nil && snap != nil {
		dir := config.ResolveAgentWorkspaceDir(&snap.Config)
		if dir != "" {
			return dir
		}
	}
	// Config unavailable — fall back to built-in default.
	return config.ResolveAgentWorkspaceDir(nil)
}

// resolveDenebDir returns the path to ~/.deneb.
func resolveDenebDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".deneb")
	}
	return "/tmp/deneb"
}
