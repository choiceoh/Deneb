// Package server — Discord inbound message preprocessing via the autoreply pipeline.
//
// Bridges the autoreply command/directive system into the Discord → chat.send
// flow so that slash commands (/new, /model, /think, etc.) and inline directives
// are processed before the message reaches the LLM agent.
package server

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/inbound"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// discordSessionSeen tracks which sessions have received initial context.
// Uses timestamps for TTL-based cleanup (24h expiry).
var (
	discordSessionSeen   = make(map[string]time.Time)
	discordSessionSeenMu sync.Mutex
)

const discordSessionTTL = 24 * time.Hour

// discordThreadParent maps a thread channel ID to its parent channel ID and
// creation timestamp. Used to resolve workspace from parent channel for thread sessions.
// Periodically pruned to prevent unbounded growth from archived-but-not-deleted threads.
var (
	discordThreadParent   = make(map[string]threadParentEntry) // threadChannelID → parentChannelID
	discordThreadParentMu sync.Mutex
)

type threadParentEntry struct {
	ParentID  string
	CreatedAt time.Time
}

const discordThreadParentTTL = 24 * time.Hour

// threadSessionKey returns the session key for a Discord thread.
func threadSessionKey(threadID string) string {
	return "discord:thread:" + threadID
}

// HandleDiscordMessage processes an incoming Discord message through the
// autoreply pipeline: command detection → directive parsing → chat.send dispatch.
func (p *InboundProcessor) HandleDiscordMessage(msg *discord.Message) {
	if msg == nil || msg.Content == "" {
		return
	}

	channelID := msg.ChannelID

	// Determine if this message is in a thread. If so, it gets its own
	// independent session key so each thread = isolated coding session.
	isThread := p.isDiscordThread(channelID)

	var sessionKey string
	if isThread {
		sessionKey = threadSessionKey(channelID)
	} else {
		sessionKey = "discord:" + channelID
	}

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
	inbound.FinalizeInboundContext(msgCtx)

	// Resolve workspace directory.
	// Thread sessions get an isolated git worktree so concurrent threads
	// don't conflict. Falls back to parent channel workspace if worktree
	// creation fails.
	workspaceDir := ""
	if p.server.discordPlug != nil {
		parentWorkspace := p.resolveParentWorkspace(channelID, isThread)
		if isThread && parentWorkspace != "" {
			workspaceDir = p.ensureThreadWorktree(channelID, parentWorkspace)
		}
		if workspaceDir == "" {
			workspaceDir = parentWorkspace
		}
	}

	// Try coding quick commands first (Discord-specific, no agent needed).
	trimmed := strings.TrimSpace(msgCtx.BodyForCommands)
	if strings.HasPrefix(trimmed, "/") {
		if handled := p.handleCodingQuickCommand(channelID, trimmed, workspaceDir); handled {
			return
		}
	}

	// Try standard slash command dispatch.
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
				Deps: p.buildCommandDeps(sessionKey),
			})
			if err == nil && result != nil && result.SkipAgent {
				// Reset auto-context on session lifecycle commands.
				if cmdKey == "new" || cmdKey == "reset" {
					discordSessionSeenMu.Lock()
					delete(discordSessionSeen, sessionKey)
					discordSessionSeenMu.Unlock()
				}
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

	// Auto-context injection: on first message in a session, prepend
	// workspace context (git branch, status) so the agent has immediate
	// project awareness for coding tasks.
	discordSessionSeenMu.Lock()
	lastSeen, exists := discordSessionSeen[sessionKey]
	isFirstMessage := !exists || time.Since(lastSeen) > discordSessionTTL
	if isFirstMessage {
		discordSessionSeen[sessionKey] = time.Now()
	}
	// Periodic cleanup: remove expired entries when map grows.
	if len(discordSessionSeen) > 100 {
		for k, t := range discordSessionSeen {
			if time.Since(t) > discordSessionTTL {
				delete(discordSessionSeen, k)
			}
		}
	}
	discordSessionSeenMu.Unlock()

	// Capture the clean user message before workspace context injection so the
	// thread namer sees only the user's words (not git status / project tree).
	cleanMessageForTitle := agentMessage

	if isFirstMessage && workspaceDir != "" {
		if ctx := buildWorkspaceContext(workspaceDir); ctx != "" {
			agentMessage = ctx + "\n\n---\n\n" + agentMessage
		}
	}

	// Process file attachments (code files uploaded by user).
	var attachments []chat.ChatAttachment
	if len(msg.Attachments) > 0 {
		attachments = p.downloadDiscordAttachments(msg.Attachments)
		// If no text but has attachments, use a default prompt.
		if agentMessage == "" && len(attachments) > 0 {
			agentMessage = "이 파일을 분석해 주세요."
		}
	}

	if agentMessage == "" {
		return
	}

	// Determine the delivery target.
	// Thread messages: reply in the same thread.
	// Channel messages: create a new thread (= new coding session) if auto-threading is on.
	deliveryTarget := channelID
	if isThread {
		// Already in a thread — replies stay here.
		deliveryTarget = channelID
	} else if isFirstMessage && p.server.discordSummarizer != nil && p.server.discordPlug.Config().AutoThreadNamesEnabled() {
		// First message in channel: create a thread for this coding session.
		if threadID := p.tryCreateDiscordThread(channelID, msg.ID, cleanMessageForTitle); threadID != "" {
			deliveryTarget = threadID
			// Switch session key to the new thread's session.
			sessionKey = threadSessionKey(threadID)
			// Mark thread session as seen so subsequent thread messages
			// don't re-inject workspace context redundantly.
			discordSessionSeenMu.Lock()
			discordSessionSeen[sessionKey] = time.Now()
			discordSessionSeenMu.Unlock()
			// Create isolated worktree for the new thread.
			if workspaceDir != "" {
				if wtDir := p.ensureThreadWorktree(threadID, workspaceDir); wtDir != "" {
					workspaceDir = wtDir
				}
			}
		}
	}

	// Build delivery context.
	delivery := map[string]any{
		"channel":   "discord",
		"to":        deliveryTarget,
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
	if len(attachments) > 0 {
		sendParams["attachments"] = attachments
	}

	// Pass per-channel workspace to the agent pipeline.
	if workspaceDir != "" {
		sendParams["workspaceDir"] = workspaceDir
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

// tryCreateDiscordThread generates a thread name via LLM and creates a Discord
// thread from the given message. Records the thread→parent mapping and returns
// the new thread's channel ID on success, or "" on failure (caller falls back to channel).
//
// The total operation is bounded by a 5-second context timeout so a slow LLM
// or Discord API call does not block the agent from starting.
func (p *InboundProcessor) tryCreateDiscordThread(channelID, messageID, content string) string {
	client := p.server.discordPlug.Client()
	if client == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check whether this channel type supports "Start Thread from Message".
	// Forum/media/voice channels use a different thread model and will
	// return error 50024 if we try the message-based endpoint.
	ch, err := client.GetChannel(ctx, channelID)
	if err != nil {
		p.logger.Warn("discord: failed to fetch channel for thread check",
			"channelId", channelID, "error", err)
		return ""
	}
	if !discord.SupportsMessageThreads(ch.Type) {
		p.logger.Debug("discord: skipping auto-thread for unsupported channel type",
			"channelId", channelID, "channelType", ch.Type)
		return ""
	}

	name := p.server.discordSummarizer.ThreadTitle(ctx, content)

	thread, err := client.CreateThread(ctx, channelID, messageID, name)
	if err != nil {
		p.logger.Warn("discord: failed to create auto thread",
			"channelId", channelID, "messageId", messageID, "error", err)
		return ""
	}

	threadSession := threadSessionKey(thread.ID)
	p.logger.Info("discord: created thread session",
		"sessionKey", threadSession, "threadId", thread.ID, "name", name)

	// Record thread → parent channel mapping for workspace resolution.
	discordThreadParentMu.Lock()
	discordThreadParent[thread.ID] = threadParentEntry{ParentID: channelID, CreatedAt: time.Now()}
	discordThreadParentMu.Unlock()
	// Persist to disk for restart recovery.
	if p.server.discordThreadStore != nil {
		p.server.discordThreadStore.Put(thread.ID, channelID)
	}

	return thread.ID
}

// resolveParentWorkspace looks up the workspace directory for a channel.
// For threads, resolves via the parent channel ID.
func (p *InboundProcessor) resolveParentWorkspace(channelID string, isThread bool) string {
	workspaceChannelID := channelID
	if isThread {
		if parentID := p.getThreadParent(channelID); parentID != "" {
			workspaceChannelID = parentID
		}
	}
	return p.server.discordPlug.Config().WorkspaceForChannel(workspaceChannelID)
}

// ensureThreadWorktree creates or retrieves a git worktree for a thread session.
// Returns the worktree directory path, or "" if creation fails (caller should
// fall back to the parent workspace).
func (p *InboundProcessor) ensureThreadWorktree(threadID, parentWorkspace string) string {
	wm := p.server.discordWorktrees
	if wm == nil {
		return ""
	}
	// Return existing worktree if already created.
	if ws := wm.Get(threadID); ws != nil {
		return ws.Dir
	}
	// Create new worktree.
	ws, err := wm.Create(threadID, parentWorkspace)
	if err != nil {
		p.logger.Warn("discord: failed to create thread worktree, using shared workspace",
			"threadId", threadID, "parentDir", parentWorkspace, "error", err)
		return ""
	}
	return ws.Dir
}

// isDiscordThread checks if a channelID is a known thread, either from the
// bot's thread parent cache or from our local thread→parent map.
func (p *InboundProcessor) isDiscordThread(channelID string) bool {
	// Check local mapping first (bot-created threads).
	discordThreadParentMu.Lock()
	_, ok := discordThreadParent[channelID]
	discordThreadParentMu.Unlock()
	if ok {
		return true
	}
	// Check the bot's Gateway-populated thread parent cache (user-created threads).
	if p.server.discordPlug != nil {
		if bot := p.server.discordPlug.Bot(); bot != nil {
			return bot.IsThread(channelID)
		}
	}
	return false
}

// getThreadParent returns the parent channel ID for a thread, or "" if unknown.
// Also prunes stale entries when the map exceeds 100 entries.
func (p *InboundProcessor) getThreadParent(threadID string) string {
	// Periodic cleanup: remove expired thread parent entries when map grows.
	discordThreadParentMu.Lock()
	if len(discordThreadParent) > 100 {
		for k, v := range discordThreadParent {
			if time.Since(v.CreatedAt) > discordThreadParentTTL {
				delete(discordThreadParent, k)
			}
		}
	}
	entry, ok := discordThreadParent[threadID]
	discordThreadParentMu.Unlock()
	if ok {
		return entry.ParentID
	}
	// Fall back to the bot's Gateway cache.
	if p.server.discordPlug != nil {
		if bot := p.server.discordPlug.Bot(); bot != nil {
			if parentID := bot.ThreadParent(threadID); parentID != "" {
				return parentID
			}
		}
	}
	// Fall back to persistent thread store (survives restart).
	if p.server.discordThreadStore != nil {
		return p.server.discordThreadStore.Get(threadID)
	}
	return ""
}

// HandleThreadEvent processes a Discord thread lifecycle event (archive/delete).
// When a thread is archived or deleted, the associated session is ended.
func (p *InboundProcessor) HandleThreadEvent(event discord.ThreadEvent) {
	sessionKey := threadSessionKey(event.ThreadID)

	if event.Archived || event.Deleted {
		p.logger.Info("discord: thread session ended",
			"threadId", event.ThreadID,
			"sessionKey", sessionKey,
			"archived", event.Archived,
			"deleted", event.Deleted)

		// Clear session auto-context state.
		discordSessionSeenMu.Lock()
		delete(discordSessionSeen, sessionKey)
		discordSessionSeenMu.Unlock()

		// End the session in the session manager if it exists.
		if p.server.sessions != nil {
			existing := p.server.sessions.Get(sessionKey)
			if existing != nil && existing.Status == session.StatusRunning {
				now := time.Now().UnixMilli()
				p.server.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
					Phase: session.PhaseEnd,
					Ts:    now,
				})
			}
		}

		// Send session end summary embed on archive (before removing worktree).
		if event.Archived && p.server.discordWorktrees != nil && p.server.discordPlug != nil {
			p.sendThreadArchiveSummary(event.ThreadID)
		}

		// Remove the thread's isolated worktree.
		if p.server.discordWorktrees != nil {
			p.server.discordWorktrees.Remove(event.ThreadID)
		}

		// Clean up thread parent mapping on delete.
		if event.Deleted {
			discordThreadParentMu.Lock()
			delete(discordThreadParent, event.ThreadID)
			discordThreadParentMu.Unlock()
			if p.server.discordThreadStore != nil {
				p.server.discordThreadStore.Delete(event.ThreadID)
			}
		}
	}
}

// sendThreadArchiveSummary gathers git info from the thread worktree and sends
// a session-end summary embed showing branch, recent commits, and change stats.
func (p *InboundProcessor) sendThreadArchiveSummary(threadID string) {
	ws := p.server.discordWorktrees.Get(threadID)
	if ws == nil {
		return
	}
	dir := ws.Dir

	// Gather git info from the worktree.
	branch := runGitQuiet(dir, "rev-parse", "--abbrev-ref", "HEAD")
	diffStat := runGitQuiet(dir, "diff", "--stat", "HEAD~3..HEAD")
	recentLog := runGitQuiet(dir, "log", "--oneline", "-5")

	// Format commit lines as bullet points for the embed.
	if recentLog != "" {
		lines := strings.Split(recentLog, "\n")
		var formatted []string
		for _, l := range lines {
			if l = strings.TrimSpace(l); l != "" {
				formatted = append(formatted, "- "+l)
			}
		}
		recentLog = strings.Join(formatted, "\n")
	}

	embed := discord.FormatSessionEndEmbed(branch, diffStat, recentLog)
	client := p.server.discordPlug.Client()
	ctx := context.Background()
	client.SendMessage(ctx, threadID, &discord.SendMessageRequest{
		Embeds:          []discord.Embed{embed},
		AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
	})
}

// runGitQuiet runs a git command and returns trimmed output, or "" on error.
func runGitQuiet(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveWorkspaceChannel extracts the workspace-lookup channel ID from a session key.
// For thread sessions ("discord:thread:<threadID>"), resolves to parent channel.
// For regular sessions ("discord:<channelID>"), returns the channel ID directly.
func resolveWorkspaceChannel(p *InboundProcessor, sessionKey string) string {
	if strings.HasPrefix(sessionKey, "discord:thread:") {
		threadID := strings.TrimPrefix(sessionKey, "discord:thread:")
		if parentID := p.getThreadParent(threadID); parentID != "" {
			return parentID
		}
		return threadID
	}
	return strings.TrimPrefix(sessionKey, "discord:")
}

// resolveDiscordWorkspaceDir returns the actual workspace directory for a session key,
// preferring the thread's isolated worktree if one exists.
func resolveDiscordWorkspaceDir(p *InboundProcessor, sessionKey string) string {
	if p.server.discordPlug == nil {
		return ""
	}
	// For thread sessions, check worktree first.
	if strings.HasPrefix(sessionKey, "discord:thread:") {
		threadID := strings.TrimPrefix(sessionKey, "discord:thread:")
		if p.server.discordWorktrees != nil {
			if ws := p.server.discordWorktrees.Get(threadID); ws != nil {
				return ws.Dir
			}
		}
	}
	// Fall back to channel workspace config.
	wsChannelID := resolveWorkspaceChannel(p, sessionKey)
	return p.server.discordPlug.Config().WorkspaceForChannel(wsChannelID)
}

// handleCodingQuickCommand handles Discord-specific quick commands for vibe coders.
// Only includes commands that make sense for someone who doesn't read/write code:
// project status, commit, push, and dashboard.
// Returns true if the command was handled.
func (p *InboundProcessor) handleCodingQuickCommand(channelID, text, workspaceDir string) bool {
	if workspaceDir == "" {
		return false
	}

	cmd := extractCommandKey(text)
	switch cmd {

	case "dashboard", "d", "status", "ws":
		// /dashboard — enhanced visual project health panel for vibe coders.
		sessionKey := discordSessionKeyForChannel(p.server.discordPlug, channelID)
		embeds, buttons := p.buildEnhancedDashboard(workspaceDir, sessionKey)
		p.sendDiscordEmbedWithButtons(channelID, embeds, buttons)
		return true

	case "commit":
		// /commit [message] — stage all changes and commit with a message.
		parts := strings.SplitN(text, " ", 2)
		commitMsg := ""
		if len(parts) > 1 {
			commitMsg = strings.TrimSpace(parts[1])
		}
		if commitMsg == "" {
			commitMsg = "Auto-commit from Discord"
		}
		runGitCmd(workspaceDir, "add", "-A")
		output := runGitCmd(workspaceDir, "commit", "-m", commitMsg)
		if output == "" {
			output = "커밋할 변경 사항 없음"
		}
		success := strings.Contains(output, "file") || strings.Contains(output, "changed")
		if success {
			sessionKey := "discord:" + channelID
			p.sendDiscordEmbedWithButtons(channelID, []discord.Embed{{
				Title:       "💾 커밋 완료",
				Description: discord.TruncateText(output, 200),
				Color:       discord.ColorSuccess,
			}}, discord.AfterCommitButtons(sessionKey))
		} else {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title:       "💾 커밋",
				Description: output,
				Color:       discord.ColorWarning,
			}})
		}
		return true

	case "push":
		// /push — push current branch to remote.
		branch := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "HEAD")
		output := runCmdWithTimeout(workspaceDir, 30*time.Second, "git", "push", "-u", "origin", branch)
		if output == "" {
			output = "푸시 완료"
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:       "🚀 푸시 완료",
			Description: "`" + branch + "` 브랜치를 원격 저장소에 업로드했습니다.",
			Color:       discord.ColorSuccess,
			Fields: []discord.EmbedField{
				{Name: "브랜치", Value: "`" + branch + "`", Inline: true},
			},
		}})
		return true

	case "help":
		// /help — show vibe-coder-friendly help.
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatVibeCoderHelpEmbed()})
		return true
	}

	return false
}

// buildDashboardEmbeds is superseded by buildEnhancedDashboard which adds
// lint status, stash count, upstream info, file details, and action buttons.

// sendDiscordEmbedWithButtons sends embeds with action buttons to a Discord channel.
func (p *InboundProcessor) sendDiscordEmbedWithButtons(channelID string, embeds []discord.Embed, buttons []discord.Component) {
	client := p.server.discordPlug.Client()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.SendMessage(ctx, channelID, &discord.SendMessageRequest{
		Embeds:          embeds,
		Components:      buttons,
		AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
	})
	if err != nil {
		p.logger.Warn("failed to send discord embed with buttons", "channelId", channelID, "error", err)
	}
}

// sendDiscordFileReply sends a file attachment with a text summary to a Discord channel.
func (p *InboundProcessor) sendDiscordFileReply(channelID, summary, fileName string, data []byte) {
	client := p.server.discordPlug.Client()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.SendMessageWithFile(ctx, channelID, summary, fileName, data); err != nil {
		p.logger.Warn("failed to send discord file reply", "channelId", channelID, "error", err)
	}
}

// sendDiscordQuickReply sends a quick reply to a Discord channel.
func (p *InboundProcessor) sendDiscordQuickReply(channelID, text string) {
	client := p.server.discordPlug.Client()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := discord.SendText(ctx, client, channelID, text, ""); err != nil {
		p.logger.Warn("failed to send discord quick reply", "channelId", channelID, "error", err)
	}
}

// sendDiscordEmbed sends one or more embeds to a Discord channel.
func (p *InboundProcessor) sendDiscordEmbed(channelID string, embeds []discord.Embed) {
	client := p.server.discordPlug.Client()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.SendMessage(ctx, channelID, &discord.SendMessageRequest{
		Embeds:          embeds,
		AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
	})
	if err != nil {
		p.logger.Warn("failed to send discord embed", "channelId", channelID, "error", err)
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

// HandleDiscordInteraction processes a Discord interaction (button click, slash command).
func (p *InboundProcessor) HandleDiscordInteraction(ctx context.Context, interaction *discord.Interaction) {
	if interaction == nil {
		return
	}

	client := p.server.discordPlug.Client()
	if client == nil {
		return
	}

	customID := interaction.Data.CustomID
	if customID == "" {
		return
	}

	action, sessionKey := discord.ParseButtonAction(customID)
	if action == "" || sessionKey == "" {
		return
	}

	// Acknowledge the interaction immediately to prevent Discord timeout.
	client.CreateInteractionResponse(ctx, interaction.ID, interaction.Token, &discord.InteractionResponse{
		Type: discord.InteractionResponseDeferredUpdate,
	})

	// Map button actions to agent messages.
	var agentMessage string
	switch action {
	case "test":
		agentMessage = "프로젝트 테스트를 실행해 주세요."
	case "commit":
		agentMessage = "변경 사항을 커밋해 주세요. 적절한 커밋 메시지를 자동 생성해 주세요."
	case "revert":
		agentMessage = "마지막 변경 사항을 되돌려 주세요."
	case "fix":
		agentMessage = "테스트 실패를 수정해 주세요."
	case "details":
		agentMessage = "마지막 실행 결과를 자세히 보여주세요."
	case "mergecheck":
		// Check for merge conflicts inline — run git merge --no-commit --no-ff
		// then abort to leave worktree clean.
		if ws := resolveDiscordWorkspaceDir(p, sessionKey); ws != "" {
			branch := runGitCmd(ws, "rev-parse", "--abbrev-ref", "HEAD")
			// Fetch latest remote refs.
			runCmdWithTimeout(ws, 30*time.Second, "git", "fetch", "--all")
			// Try a dry-run merge against the default branch.
			target := detectDefaultBranch(ws)
			mergeOut := runCmdWithTimeout(ws, 15*time.Second, "git", "merge", "--no-commit", "--no-ff", target)
			conflictFiles := runGitCmd(ws, "diff", "--name-only", "--diff-filter=U")
			hasConflict := strings.Contains(mergeOut, "conflict") ||
				strings.Contains(mergeOut, "CONFLICT") ||
				conflictFiles != ""
			// Always abort the trial merge.
			runGitCmd(ws, "merge", "--abort")

			embed := discord.FormatMergeConflictCheckEmbed(hasConflict, conflictFiles, branch, target)
			var buttons []discord.Component
			if hasConflict {
				buttons = discord.MergeConflictButtons(sessionKey)
			}
			p.sendDiscordEmbedWithButtons(interaction.ChannelID, []discord.Embed{embed}, buttons)
		}
		return
	case "mergeabort":
		// Abort an in-progress merge.
		if ws := resolveDiscordWorkspaceDir(p, sessionKey); ws != "" {
			abortOut := runGitCmd(ws, "merge", "--abort")
			if strings.Contains(abortOut, "error") || strings.Contains(abortOut, "fatal") {
				p.sendDiscordEmbed(interaction.ChannelID, []discord.Embed{{
					Title:       "❌ 병합 중단 실패",
					Description: "진행 중인 병합이 없거나 이미 중단되었습니다.",
					Color:       discord.ColorError,
				}})
			} else {
				p.sendDiscordEmbed(interaction.ChannelID, []discord.Embed{{
					Title:       "⛔ 병합 중단 완료",
					Description: "병합을 중단하고 이전 상태로 되돌렸습니다.",
					Color:       discord.ColorSuccess,
				}})
			}
		}
		return
	case "mergefix":
		agentMessage = "현재 병합 충돌을 확인하고 자동으로 해결해 주세요. 충돌이 있는 파일들을 분석하고, 양쪽 변경 사항을 적절히 통합해서 충돌 마커를 제거해 주세요. 해결이 끝나면 결과를 요약해 주세요."
	case "mergedetail":
		agentMessage = "현재 병합 충돌 상태를 자세히 분석해 주세요. 충돌이 있는 파일 목록, 각 파일의 충돌 내용, 그리고 양쪽 브랜치에서 어떤 변경이 있었는지 설명해 주세요."

	// --- Diff Preview buttons ---
	case "diffapply":
		agentMessage = "미리보기한 변경 사항을 적용해 주세요."
	case "diffreject":
		agentMessage = "미리보기한 변경 사항을 적용하지 마세요. 다른 방법을 제안해 주세요."
	case "difffull":
		agentMessage = "변경 사항의 전체 diff를 보여주세요."

	// --- Error Recovery buttons ---
	case "autofix":
		agentMessage = "발생한 오류를 분석하고 자동으로 수정해 주세요. 수정 후 빌드와 테스트를 다시 실행해서 확인해 주세요."
	case "altfix":
		agentMessage = "이전 수정 방법이 실패했습니다. 완전히 다른 접근 방법으로 문제를 해결해 주세요. 이전 변경은 되돌리고 새로운 전략을 사용해 주세요."

	// --- Smart Test buttons ---
	case "testall":
		agentMessage = "전체 테스트 스위트를 실행해 주세요."

	// --- Git Workflow buttons ---
	case "branchcreate":
		agentMessage = "현재 작업을 위한 새 브랜치를 생성해 주세요. 적절한 브랜치 이름을 자동으로 정하고, 브랜치 생성 후 전환해 주세요."
	case "prcreate":
		agentMessage = "현재 브랜치의 변경 사항으로 Pull Request를 생성해 주세요. PR 제목과 설명을 변경 내용 기반으로 자동 생성해 주세요."

	// --- Dashboard button ---
	case "dashboard":
		if ws := resolveDiscordWorkspaceDir(p, sessionKey); ws != "" {
			embeds, buttons := p.buildEnhancedDashboard(ws, sessionKey)
			p.sendDiscordEmbedWithButtons(interaction.ChannelID, embeds, buttons)
		}
		return

	case "push":
		// Push current branch to remote — handle inline for quick feedback.
		if ws := resolveDiscordWorkspaceDir(p, sessionKey); ws != "" {
			branch := runGitCmd(ws, "rev-parse", "--abbrev-ref", "HEAD")
			runCmdWithTimeout(ws, 30*time.Second, "git", "push", "-u", "origin", branch)
			lastCommit := runGitCmd(ws, "log", "--oneline", "-1")
			desc := "`" + branch + "` 브랜치를 원격 저장소에 업로드했습니다."
			var fields []discord.EmbedField
			fields = append(fields, discord.EmbedField{
				Name: "🌿 브랜치", Value: "`" + branch + "`", Inline: true,
			})
			if lastCommit != "" {
				fields = append(fields, discord.EmbedField{
					Name: "📜 최근 커밋", Value: discord.TruncateText(lastCommit, 100), Inline: false,
				})
			}
			p.sendDiscordEmbedWithButtons(interaction.ChannelID, []discord.Embed{{
				Title:       "🚀 푸시 완료",
				Description: desc,
				Color:       discord.ColorSuccess,
				Fields:      fields,
			}}, []discord.Component{discord.ActionRow(
				discord.Button("📊 현황 보기", fmt.Sprintf("dashboard:%s", sessionKey), discord.ButtonSecondary),
				discord.Button("🆕 새 작업", fmt.Sprintf("new:%s", sessionKey), discord.ButtonPrimary),
			)})
		}
		return
	case "new":
		agentMessage = "새 작업을 시작합니다. 무엇을 도와드릴까요?"
		// Clear session state for fresh start.
		discordSessionSeenMu.Lock()
		delete(discordSessionSeen, sessionKey)
		discordSessionSeenMu.Unlock()
	case "cancel":
		// Acknowledge only, no action.
		return
	default:
		return
	}

	// Resolve delivery target: use the interaction channel.
	channelID := interaction.ChannelID
	delivery := map[string]any{
		"channel": "discord",
		"to":      channelID,
	}

	sendParams := map[string]any{
		"sessionKey": sessionKey,
		"message":    agentMessage,
		"delivery":   delivery,
	}

	// Resolve workspace for the session (worktree if available).
	if ws := resolveDiscordWorkspaceDir(p, sessionKey); ws != "" {
		sendParams["workspaceDir"] = ws
	}

	req, err := protocol.NewRequestFrame(
		"dc-interaction-"+interaction.ID,
		"chat.send",
		sendParams,
	)
	if err != nil {
		p.logger.Error("failed to build chat.send for interaction", "error", err)
		return
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer sendCancel()
	resp := p.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		p.logger.Warn("chat.send failed for discord interaction",
			"action", action, "error", resp.Error)
	}
}

// maxAttachmentSize is the max file size to download from Discord (1 MB).
const maxAttachmentSize = 1 * 1024 * 1024

// downloadDiscordAttachments downloads file attachments from a Discord message
// and converts them to ChatAttachments for the agent pipeline.
func (p *InboundProcessor) downloadDiscordAttachments(attachments []discord.Attachment) []chat.ChatAttachment {
	var result []chat.ChatAttachment
	for _, att := range attachments {
		if att.Size > maxAttachmentSize {
			p.logger.Info("skipping large discord attachment",
				"filename", att.Filename, "size", att.Size)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		data, err := downloadURL(ctx, att.URL)
		cancel()
		if err != nil {
			p.logger.Warn("failed to download discord attachment",
				"filename", att.Filename, "error", err)
			continue
		}

		// Determine type: code files → "file", images → "image".
		attType := "file"
		lang := discord.DetectCodeLanguage(att.Filename)
		if isImageFilename(att.Filename) {
			attType = "image"
		}

		ca := chat.ChatAttachment{
			Type:     attType,
			Name:     att.Filename,
			Data:     base64.StdEncoding.EncodeToString(data),
			MimeType: guessMimeType(att.Filename),
		}
		_ = lang // language info available if needed for context

		result = append(result, ca)
	}
	return result
}

// downloadURL fetches raw bytes from a URL.
func downloadURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAttachmentSize+1))
}

// isImageFilename checks if a filename looks like an image.
func isImageFilename(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// guessMimeType returns a MIME type based on file extension.
func guessMimeType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".go"):
		return "text/x-go"
	case strings.HasSuffix(lower, ".py"):
		return "text/x-python"
	case strings.HasSuffix(lower, ".js"):
		return "text/javascript"
	case strings.HasSuffix(lower, ".ts"):
		return "text/typescript"
	case strings.HasSuffix(lower, ".rs"):
		return "text/x-rust"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "text/yaml"
	case strings.HasSuffix(lower, ".md"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// buildWorkspaceContext gathers lightweight workspace info for first-message context.
// Returns a formatted string with git branch, short status, and project root files.
func buildWorkspaceContext(workspaceDir string) string {
	if _, err := os.Stat(workspaceDir); err != nil {
		return ""
	}

	var parts []string

	// Git branch + short status.
	if branch := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		parts = append(parts, "**Branch:** `"+branch+"`")
	}
	if status := runGitCmd(workspaceDir, "status", "--short"); status != "" {
		lines := strings.Split(strings.TrimSpace(status), "\n")
		if len(lines) > 15 {
			lines = append(lines[:15], fmt.Sprintf("... and %d more files", len(lines)-15))
		}
		parts = append(parts, "**Git Status:**\n```\n"+strings.Join(lines, "\n")+"\n```")
	} else if len(parts) > 0 {
		parts = append(parts, "**Git Status:** clean")
	}

	// Top-level directory listing.
	if ls := runCmd(workspaceDir, "ls", "-1"); ls != "" {
		lines := strings.Split(strings.TrimSpace(ls), "\n")
		if len(lines) > 20 {
			lines = append(lines[:20], fmt.Sprintf("... and %d more", len(lines)-20))
		}
		parts = append(parts, "**Project Root:**\n```\n"+strings.Join(lines, "\n")+"\n```")
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Workspace Context\n`" + workspaceDir + "`\n\n" + strings.Join(parts, "\n")
}

// runGitCmd runs a git command in the given directory and returns trimmed stdout.
func runGitCmd(dir string, args ...string) string {
	return runCmd(dir, "git", args...)
}

// runCmd runs a command in the given directory with a 5-second timeout.
func runCmd(dir string, name string, args ...string) string {
	return runCmdWithTimeout(dir, 5*time.Second, name, args...)
}

// runCmdWithTimeout runs a command with a custom timeout. Returns combined
// stdout+stderr trimmed output.
func runCmdWithTimeout(dir string, timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		// Return partial output even on error (useful for build/test failures).
		if s := strings.TrimSpace(out.String()); s != "" {
			return s
		}
		return ""
	}
	return strings.TrimSpace(out.String())
}

// detectDefaultBranch returns the default branch name (main or master)
// by checking the remote HEAD. Falls back to "main".
func detectDefaultBranch(dir string) string {
	ref := runGitCmd(dir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if ref != "" {
		// e.g. "refs/remotes/origin/main" → "origin/main"
		parts := strings.SplitN(ref, "refs/remotes/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	// Fallback: check if origin/main exists.
	if out := runGitCmd(dir, "rev-parse", "--verify", "origin/main"); out != "" {
		return "origin/main"
	}
	if out := runGitCmd(dir, "rev-parse", "--verify", "origin/master"); out != "" {
		return "origin/master"
	}
	return "origin/main"
}

// detectProjectType determines the project type from marker files.
func detectProjectType(dir string) string {
	markers := map[string]string{
		"go.mod":           "go",
		"Cargo.toml":       "rust",
		"package.json":     "node",
		"pyproject.toml":   "python",
		"setup.py":         "python",
		"requirements.txt": "python",
		"Makefile":         "make",
	}
	for file, lang := range markers {
		if _, err := os.Stat(dir + "/" + file); err == nil {
			return lang
		}
	}
	return ""
}

// testCommand returns the test command for a project type.
func testCommand(projType string) (string, []string) {
	switch projType {
	case "go":
		return "go", []string{"test", "./..."}
	case "rust":
		return "cargo", []string{"test"}
	case "node":
		return "npm", []string{"test"}
	case "python":
		return "python", []string{"-m", "pytest"}
	case "make":
		return "make", []string{"test"}
	}
	return "", nil
}

// buildCommand returns the build command for a project type.
func buildCommand(projType string) (string, []string) {
	switch projType {
	case "go":
		return "go", []string{"build", "./..."}
	case "rust":
		return "cargo", []string{"build"}
	case "node":
		return "npm", []string{"run", "build"}
	case "make":
		return "make", []string{"all"}
	}
	return "", nil
}

// lintCommand returns the lint/vet command for a project type.
func lintCommand(projType string) (string, []string) {
	switch projType {
	case "go":
		return "go", []string{"vet", "./..."}
	case "rust":
		return "cargo", []string{"clippy", "--workspace", "--", "-D", "warnings"}
	case "node":
		return "npx", []string{"eslint", "."}
	case "python":
		return "python", []string{"-m", "ruff", "check", "."}
	}
	return "", nil
}

// buildEnhancedDashboard creates the enhanced dashboard with lint, stash, upstream info.
func (p *InboundProcessor) buildEnhancedDashboard(workspaceDir, sessionKey string) ([]discord.Embed, []discord.Component) {
	branch := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "HEAD")
	status := runGitCmd(workspaceDir, "status", "--short")
	recentLog := runGitCmd(workspaceDir, "log", "--oneline", "-5", "--no-color")

	// Count changed files and build summary.
	changedFiles := 0
	filesSummary := ""
	if status != "" {
		lines := strings.Split(strings.TrimSpace(status), "\n")
		changedFiles = len(lines)
		// Show first 5 files.
		var summaryLines []string
		for i, line := range lines {
			if i >= 5 {
				summaryLines = append(summaryLines, fmt.Sprintf("... 외 %d개", len(lines)-5))
				break
			}
			summaryLines = append(summaryLines, "`"+strings.TrimSpace(line)+"`")
		}
		filesSummary = strings.Join(summaryLines, "\n")
	}

	// Upstream tracking info.
	upstream := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")

	// Stash count.
	stashCount := 0
	if stashOut := runGitCmd(workspaceDir, "stash", "list"); stashOut != "" {
		stashCount = len(strings.Split(strings.TrimSpace(stashOut), "\n"))
	}

	// Build/test/lint status (concurrent).
	projType := detectProjectType(workspaceDir)
	buildStatus := "⏭️ 미확인"
	testStatus := "⏭️ 미확인"
	lintStatus := "⏭️ 미확인"

	if cmdName, cmdArgs := buildCommand(projType); cmdName != "" {
		buildOut := runCmdWithTimeout(workspaceDir, 15*time.Second, cmdName, cmdArgs...)
		lower := strings.ToLower(buildOut)
		if buildOut == "" || (!strings.Contains(lower, "error") && !strings.Contains(lower, "fail")) {
			buildStatus = "✅ 성공"
		} else {
			buildStatus = "❌ 실패"
		}
	}

	if cmdName, cmdArgs := testCommand(projType); cmdName != "" {
		testOut := runCmdWithTimeout(workspaceDir, 30*time.Second, cmdName, cmdArgs...)
		lower := strings.ToLower(testOut)
		if testOut == "" || (!strings.Contains(lower, "fail") && !strings.Contains(lower, "error") && !strings.Contains(lower, "panic")) {
			testStatus = "✅ 전체 통과"
		} else {
			testStatus = "❌ 일부 실패"
		}
	}

	if cmdName, cmdArgs := lintCommand(projType); cmdName != "" {
		lintOut := runCmdWithTimeout(workspaceDir, 15*time.Second, cmdName, cmdArgs...)
		lower := strings.ToLower(lintOut)
		if lintOut == "" || (!strings.Contains(lower, "error") && !strings.Contains(lower, "warning")) {
			lintStatus = "✅ 깨끗"
		} else if strings.Contains(lower, "error") {
			lintStatus = "❌ 오류 있음"
		} else {
			lintStatus = "⚠️ 경고 있음"
		}
	}

	// Format recent commits.
	commitSummary := "커밋 없음"
	if recentLog != "" {
		lines := strings.Split(strings.TrimSpace(recentLog), "\n")
		var commitLines []string
		for _, line := range lines {
			if len(line) > 0 {
				commitLines = append(commitLines, "• "+line)
			}
		}
		commitSummary = strings.Join(commitLines, "\n")
	}

	data := discord.DashboardData{
		Branch:       branch,
		ChangedFiles: changedFiles,
		FilesSummary: filesSummary,
		BuildStatus:  buildStatus,
		TestStatus:   testStatus,
		LintStatus:   lintStatus,
		RecentLog:    commitSummary,
		Upstream:     upstream,
		StashCount:   stashCount,
	}

	embed := discord.FormatEnhancedDashboardEmbed(data)
	buttons := discord.DashboardButtons(sessionKey)
	return []discord.Embed{embed}, buttons
}

// changedGoPackages returns the Go packages that have uncommitted changes.
// Uses git diff to find changed .go files and maps them to packages.
func changedGoPackages(workspaceDir string) []string {
	diff := runGitCmd(workspaceDir, "diff", "--name-only", "HEAD")
	if diff == "" {
		// Also check untracked files.
		diff = runGitCmd(workspaceDir, "ls-files", "--others", "--exclude-standard")
	}
	if diff == "" {
		return nil
	}

	pkgSet := make(map[string]bool)
	for _, file := range strings.Split(strings.TrimSpace(diff), "\n") {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		dir := file
		if idx := strings.LastIndex(file, "/"); idx >= 0 {
			dir = file[:idx]
		} else {
			dir = "."
		}
		pkgSet["./" + dir + "/..."] = true
	}

	pkgs := make([]string, 0, len(pkgSet))
	for pkg := range pkgSet {
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}
