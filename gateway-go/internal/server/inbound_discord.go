// Package server — Discord inbound message preprocessing via the autoreply pipeline.
//
// Bridges the autoreply command/directive system into the Discord → chat.send
// flow so that slash commands (/new, /model, /think, etc.) and inline directives
// are processed before the message reaches the LLM agent.
package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/inbound"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// discordSessionSeen tracks which sessions have received initial context.
// Uses timestamps for TTL-based cleanup (24h expiry).
var (
	discordSessionSeen   = make(map[string]time.Time)
	discordSessionSeenMu sync.Mutex
)

const discordSessionTTL = 24 * time.Hour

// discordSessionThread maps a session key to the Discord thread channel ID that
// was created for it. Populated on the first message of each new coding session
// when auto thread names are enabled.
var (
	discordSessionThread   = make(map[string]string) // sessionKey → threadChannelID
	discordSessionThreadMu sync.Mutex
	// discordThreadSession is the reverse map: threadChannelID → sessionKey.
	// Used to route incoming thread messages back to the originating session.
	discordThreadSession   = make(map[string]string)
	discordThreadSessionMu sync.Mutex
)

// HandleDiscordMessage processes an incoming Discord message through the
// autoreply pipeline: command detection → directive parsing → chat.send dispatch.
func (p *InboundProcessor) HandleDiscordMessage(msg *discord.Message) {
	if msg == nil || msg.Content == "" {
		return
	}

	channelID := msg.ChannelID

	// If this message arrived in a thread that was auto-created by us, route it
	// back to the parent session so conversation history stays consistent.
	discordThreadSessionMu.Lock()
	parentSession, isKnownThread := discordThreadSession[channelID]
	discordThreadSessionMu.Unlock()

	sessionKey := "discord:" + channelID
	if isKnownThread {
		sessionKey = parentSession
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

	// Resolve per-channel workspace directory.
	// For thread messages, use the parent channel ID so workspace mappings apply correctly.
	workspaceDir := ""
	if p.server.discordPlug != nil {
		workspaceChannelID := channelID
		if isKnownThread {
			// parentSession is "discord:<parentChannelID>"
			workspaceChannelID = strings.TrimPrefix(parentSession, "discord:")
		}
		workspaceDir = p.server.discordPlug.Config().WorkspaceForChannel(workspaceChannelID)
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
					// Clear thread mapping so the next message creates a fresh thread.
					discordSessionThreadMu.Lock()
					if oldThread, ok := discordSessionThread[sessionKey]; ok {
						delete(discordSessionThread, sessionKey)
						discordThreadSessionMu.Lock()
						delete(discordThreadSession, oldThread)
						discordThreadSessionMu.Unlock()
					}
					discordSessionThreadMu.Unlock()
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

	// Determine the delivery target. If this session already has a thread,
	// send replies there. If not but we should create one, do so now.
	deliveryTarget := channelID
	if isKnownThread {
		// Incoming message is from a thread we created — replies stay in that thread.
		deliveryTarget = channelID
	} else {
		// Check whether the session already has a thread from a previous message.
		discordSessionThreadMu.Lock()
		existingThread, hasThread := discordSessionThread[sessionKey]
		discordSessionThreadMu.Unlock()

		if hasThread {
			deliveryTarget = existingThread
		} else if isFirstMessage && p.server.discordThreadNamer != nil {
			// First message in a new session: generate a thread name and create the thread.
			// Use cleanMessageForTitle (no workspace context) so the LLM sees only the user's words.
			if threadID := p.tryCreateDiscordThread(sessionKey, channelID, msg.ID, cleanMessageForTitle); threadID != "" {
				deliveryTarget = threadID
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
// thread from the given message. Stores the thread mapping and returns the new
// thread's channel ID on success, or "" on failure (caller falls back to channel).
//
// The total operation is bounded by a 5-second context timeout so a slow LLM
// or Discord API call does not block the agent from starting.
func (p *InboundProcessor) tryCreateDiscordThread(sessionKey, channelID, messageID, content string) string {
	client := p.server.discordPlug.Client()
	if client == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := p.server.discordThreadNamer.Generate(ctx, content)

	thread, err := client.CreateThread(ctx, channelID, messageID, name)
	if err != nil {
		p.logger.Warn("discord: failed to create auto thread",
			"channelId", channelID, "messageId", messageID, "error", err)
		return ""
	}

	p.logger.Info("discord: created auto thread",
		"sessionKey", sessionKey, "threadId", thread.ID, "name", name)

	discordSessionThreadMu.Lock()
	discordSessionThread[sessionKey] = thread.ID
	discordSessionThreadMu.Unlock()

	discordThreadSessionMu.Lock()
	discordThreadSession[thread.ID] = sessionKey
	discordThreadSessionMu.Unlock()

	return thread.ID
}

// handleCodingQuickCommand handles Discord-specific coding shortcuts that
// return results directly without going through the agent.
// Returns true if the command was handled.
func (p *InboundProcessor) handleCodingQuickCommand(channelID, text, workspaceDir string) bool {
	if workspaceDir == "" {
		return false
	}

	cmd := extractCommandKey(text)
	switch cmd {
	case "diff":
		output := runGitCmd(workspaceDir, "diff", "--stat")
		if output == "" {
			output = "No changes."
		}
		p.sendDiscordQuickReply(channelID, "```diff\n"+output+"\n```")
		return true

	case "gdiff":
		output := runGitCmd(workspaceDir, "diff")
		if output == "" {
			output = "No changes."
		}
		p.sendDiscordQuickReply(channelID, "```diff\n"+output+"\n```")
		return true

	case "tree":
		depth := "2"
		// Parse optional depth: /tree 3
		parts := strings.Fields(text)
		if len(parts) > 1 {
			depth = parts[1]
		}
		output := runCmd(workspaceDir, "find", ".", "-maxdepth", depth,
			"-not", "-path", "*/.*", "-not", "-path", "*/node_modules/*",
			"-not", "-path", "*/target/*")
		if output == "" {
			output = "(empty)"
		}
		p.sendDiscordQuickReply(channelID, "```\n"+output+"\n```")
		return true

	case "branch", "branches":
		output := runGitCmd(workspaceDir, "branch", "-v", "--no-color")
		if output == "" {
			output = "No git branches."
		}
		p.sendDiscordQuickReply(channelID, "```\n"+output+"\n```")
		return true

	case "log":
		count := "10"
		parts := strings.Fields(text)
		if len(parts) > 1 {
			count = parts[1]
		}
		output := runGitCmd(workspaceDir, "log", "--oneline", "-"+count, "--no-color")
		if output == "" {
			output = "No commits."
		}
		p.sendDiscordQuickReply(channelID, "```\n"+output+"\n```")
		return true

	case "ws", "workspace":
		ctx := buildWorkspaceContext(workspaceDir)
		if ctx == "" {
			ctx = "Workspace: `" + workspaceDir + "`"
		}
		p.sendDiscordQuickReply(channelID, ctx)
		return true
	}

	return false
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}
