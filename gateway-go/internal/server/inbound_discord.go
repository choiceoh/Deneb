// Package server — Discord inbound message preprocessing via the autoreply pipeline.
//
// Bridges the autoreply command/directive system into the Discord → chat.send
// flow so that slash commands (/new, /model, /think, etc.) and inline directives
// are processed before the message reaches the LLM agent.
package server

import (
	"context"
	"os/exec"
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
