// Discord chat handler wiring, follow-ups, and auto-verify.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
)

// wireDiscordChatHandler connects the Discord Gateway message handler to the
// chat handler for coding-focused agent sessions. It wraps the existing
// channel handlers so both Telegram and Discord can coexist.
func (s *Server) wireDiscordChatHandler() {
	// Initialize lightweight LLM features using the local sglang model.
	discordCfg := s.discordPlug.Config()
	if s.chatHandler != nil && s.chatHandler.ModelRegistry() != nil {
		reg := s.chatHandler.ModelRegistry()
		lwClient := reg.Client(modelrole.RoleLightweight)
		lwModel := reg.Model(modelrole.RoleLightweight)
		if discordCfg.AutoThreadNamesEnabled() {
			s.discordThreadNamer = discord.NewThreadNamer(lwClient, lwModel)
			s.logger.Info("discord: auto thread naming enabled", "model", lwModel)
		}
		s.discordReasoningSumm = discord.NewReasoningSummarizer(lwClient, lwModel)
		if s.discordReasoningSumm != nil {
			s.logger.Info("discord: reasoning summarizer enabled", "model", lwModel)
		}
	}

	// Initialize per-thread worktree manager for workspace isolation.
	s.discordWorktrees = discord.NewWorktreeManager("", s.logger)

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

		// Analyze reply outcome for context-aware buttons and auto-verification.
		outcome := discord.AnalyzeReply(text)
		var components []discord.Component
		if delivery.To != "" {
			sessionKey := discordSessionKeyForChannel(s.discordPlug, delivery.To)
			components = discord.ContextButtons(outcome, sessionKey)
		}

		// Send the main reply text (with buttons on last chunk if any).
		if len(components) > 0 {
			chunks := discord.ChunkText(formatted.Text, discord.TextChunkLimit)
			for i, chunk := range chunks {
				req := &discord.SendMessageRequest{
					Content:         chunk,
					AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
				}
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
		} else {
			if _, err := discord.SendText(ctx, client, delivery.To, formatted.Text, delivery.MessageID); err != nil {
				return err
			}
		}

		// Post-reply follow-ups for vibe coders: error translation and auto-verify.
		s.sendVibeCoderFollowUps(ctx, client, delivery, text, outcome)
		return nil
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

	// Discord does not use status reactions — progress is shown via
	// ProgressTracker embeds instead. Reaction wiring is intentionally
	// skipped to avoid 404 spam when the triggering message is ephemeral
	// or deleted (e.g. thread-start messages in Discord).

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
				tracker.SetSummarizer(s.discordReasoningSumm)
				activeTrackers[delivery.To] = tracker
			}
		}
		progressMu.Unlock()

		if tracker == nil {
			return
		}

		switch event.Type {
		case "start":
			tracker.StartStep(ctx, event.Name, event.Reason)
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

	// Wire thread lifecycle handler: archive/delete → end session.
	s.discordPlug.SetThreadEventHandler(func(event discord.ThreadEvent) {
		inbound.HandleThreadEvent(event)
	})

	// Register Discord's per-channel upload limit for the send_file tool.
	s.chatHandler.SetChannelUploadLimit("discord", s.discordPlug.MaxUploadBytes())

	s.logger.Info("discord chat handler wired (coding channel)")
}

// sendVibeCoderFollowUps sends post-reply embeds for vibe coders:
// - Error Korean translation when errors are detected
// - Auto build/test verification when code changes are detected
func (s *Server) sendVibeCoderFollowUps(ctx context.Context, client *discord.Client, delivery *chat.DeliveryContext, text string, outcome discord.ReplyOutcome) {
	if delivery.To == "" {
		return
	}

	// Error translation + recovery: add Korean explanation and auto-fix buttons.
	if outcome == discord.OutcomeBuildFail || outcome == discord.OutcomeTestFail || outcome == discord.OutcomeError {
		if embed := discord.FormatErrorTranslationEmbed(text); embed != nil {
			sessionKey := discordSessionKeyForChannel(s.discordPlug, delivery.To)
			client.SendMessage(ctx, delivery.To, &discord.SendMessageRequest{
				Embeds:          []discord.Embed{*embed},
				Components:      discord.ErrorRecoveryButtons(sessionKey),
				AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
			})
		}
	}

	// Auto-verify: when code changes are detected, run build + test and show results.
	if outcome == discord.OutcomeCodeChange {
		s.sendAutoVerifyEmbed(ctx, client, delivery.To)
	}
}

// sendAutoVerifyEmbed runs build and test commands in the workspace and sends
// a result embed to the Discord channel. This gives vibe coders immediate
// feedback on whether the agent's changes actually work.
func (s *Server) sendAutoVerifyEmbed(ctx context.Context, client *discord.Client, channelID string) {
	if s.discordPlug == nil {
		return
	}

	// Resolve workspace for this channel. For threads, prefer worktree.
	sessionKey := discordSessionKeyForChannel(s.discordPlug, channelID)
	workspaceDir := ""
	if strings.HasPrefix(sessionKey, "discord:thread:") {
		threadID := strings.TrimPrefix(sessionKey, "discord:thread:")
		if s.discordWorktrees != nil {
			if ws := s.discordWorktrees.Get(threadID); ws != nil {
				workspaceDir = ws.Dir
			}
		}
	}
	if workspaceDir == "" {
		wsChannelID := channelID
		if bot := s.discordPlug.Bot(); bot != nil {
			if parentID := bot.ThreadParent(channelID); parentID != "" {
				wsChannelID = parentID
			}
		}
		workspaceDir = s.discordPlug.Config().WorkspaceForChannel(wsChannelID)
	}
	if workspaceDir == "" {
		return
	}

	// Run build and test with timeouts (non-blocking to the reply).
	buildResult, buildOk := runQuickVerify(workspaceDir, "build")
	testResult, testOk := runQuickVerify(workspaceDir, "test")

	// Build the verification embed.
	var fields []discord.EmbedField

	buildEmoji := "✅"
	if !buildOk {
		buildEmoji = "❌"
	}
	fields = append(fields, discord.EmbedField{
		Name: "🔨 빌드", Value: buildEmoji + " " + discord.TruncateText(buildResult, 200), Inline: false,
	})

	testEmoji := "✅"
	if !testOk {
		testEmoji = "❌"
	}
	fields = append(fields, discord.EmbedField{
		Name: "🧪 테스트", Value: testEmoji + " " + discord.TruncateText(testResult, 200), Inline: false,
	})

	color := discord.ColorSuccess
	title := "✅ 자동 검증 통과"
	if !buildOk || !testOk {
		color = discord.ColorError
		title = "⚠️ 자동 검증 실패"
	}

	embed := discord.Embed{
		Title:     title,
		Color:     color,
		Fields:    fields,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Footer:    &discord.EmbedFooter{Text: "코드 변경 감지 → 자동 빌드/테스트 실행"},
	}

	// If verification failed, add fix button.
	var components []discord.Component
	if !buildOk || !testOk {
		sessionKey := discordSessionKeyForChannel(s.discordPlug, channelID)
		components = discord.BuildFailButtons(sessionKey)
	}

	client.SendMessage(ctx, channelID, &discord.SendMessageRequest{
		Embeds:          []discord.Embed{embed},
		Components:      components,
		AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
	})
}

// runQuickVerify runs a build or test command and returns (summary, success).
func runQuickVerify(workspaceDir, kind string) (string, bool) {
	projType := detectProjectType(workspaceDir)
	if projType == "" {
		return "프로젝트 타입 감지 실패", false
	}

	var cmdName string
	var cmdArgs []string

	switch kind {
	case "build":
		cmdName, cmdArgs = buildCommand(projType)
	case "test":
		cmdName, cmdArgs = testCommand(projType)
	}

	if cmdName == "" {
		return "해당 없음", true
	}

	output := runCmdWithTimeout(workspaceDir, 30*time.Second, cmdName, cmdArgs...)
	if output == "" {
		return "성공", true
	}

	lower := strings.ToLower(output)
	isError := strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.Contains(lower, "panic")
	if isError {
		// Extract just the last few lines for a concise summary.
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		return strings.Join(lines, "\n"), false
	}

	return "성공", true
}

// discordSessionKeyForChannel returns the session key for a Discord channel ID.
// For threads, returns "discord:thread:<id>"; for regular channels, "discord:<id>".
func discordSessionKeyForChannel(plug *discord.Plugin, channelID string) string {
	if plug != nil {
		if bot := plug.Bot(); bot != nil && bot.IsThread(channelID) {
			return "discord:thread:" + channelID
		}
	}
	// Also check the local thread parent map.
	discordThreadParentMu.Lock()
	_, isThread := discordThreadParent[channelID]
	discordThreadParentMu.Unlock()
	if isThread {
		return "discord:thread:" + channelID
	}
	return "discord:" + channelID
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
