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
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "📊 Git Diff", Description: "변경 사항 없음", Color: discord.ColorInfo,
			}})
			return true
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatGitDiffEmbed(output)})
		return true

	case "gdiff":
		output := runGitCmd(workspaceDir, "diff")
		if output == "" {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "📊 Git Diff (full)", Description: "변경 사항 없음", Color: discord.ColorInfo,
			}})
			return true
		}
		// Full diff can be large — send as code block text (not embed) for readability.
		p.sendDiscordQuickReply(channelID, "```diff\n"+output+"\n```")
		return true

	case "tree":
		depth := "2"
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
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:       "🌳 Directory Tree (depth " + depth + ")",
			Description: "```\n" + discord.TruncateText(output, 4000) + "\n```",
			Color:       discord.ColorInfo,
		}})
		return true

	case "branch", "branches":
		output := runGitCmd(workspaceDir, "branch", "-v", "--no-color")
		if output == "" {
			output = "No git branches."
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatBranchEmbed(output)})
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
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatGitLogEmbed(output)})
		return true

	case "ws", "workspace", "status":
		branch := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "HEAD")
		status := runGitCmd(workspaceDir, "status", "--short")
		diffStats := runGitCmd(workspaceDir, "diff", "--stat")
		recentLog := runGitCmd(workspaceDir, "log", "--oneline", "-5", "--no-color")
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatStatusEmbed(branch, status, diffStats, recentLog)})
		return true

	case "test":
		projType := detectProjectType(workspaceDir)
		cmdName, cmdArgs := testCommand(projType)
		if cmdName == "" {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "🧪 테스트", Description: "프로젝트 타입을 감지할 수 없습니다.", Color: discord.ColorWarning,
			}})
			return true
		}
		output := runCmdWithTimeout(workspaceDir, 60*time.Second, cmdName, cmdArgs...)
		success := output != "" && !strings.Contains(output, "FAIL")
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatBuildEmbed(output, success)})
		return true

	case "build":
		projType := detectProjectType(workspaceDir)
		cmdName, cmdArgs := buildCommand(projType)
		if cmdName == "" {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "🔨 빌드", Description: "프로젝트 타입을 감지할 수 없습니다.", Color: discord.ColorWarning,
			}})
			return true
		}
		output := runCmdWithTimeout(workspaceDir, 60*time.Second, cmdName, cmdArgs...)
		success := !strings.Contains(strings.ToLower(output), "error")
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatBuildEmbed(output, success)})
		return true

	case "lint":
		projType := detectProjectType(workspaceDir)
		cmdName, cmdArgs := lintCommand(projType)
		if cmdName == "" {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "🔍 린트", Description: "프로젝트 타입을 감지할 수 없습니다.", Color: discord.ColorWarning,
			}})
			return true
		}
		output := runCmdWithTimeout(workspaceDir, 30*time.Second, cmdName, cmdArgs...)
		if output == "" {
			output = "린트 이슈 없음 ✅"
		}
		hasIssues := strings.Contains(output, "error") || strings.Contains(output, "warning")
		color := discord.ColorSuccess
		if hasIssues {
			color = discord.ColorWarning
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:       "🔍 린트",
			Description: "```\n" + discord.TruncateText(output, 4000) + "\n```",
			Color:       color,
		}})
		return true

	case "commit":
		// /commit [message] — stage and commit. If no message, generate one.
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
		color := discord.ColorSuccess
		title := "💾 커밋 완료"
		if !success {
			color = discord.ColorWarning
			title = "💾 커밋"
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:       title,
			Description: "```\n" + discord.TruncateText(output, 4000) + "\n```",
			Color:       color,
		}})
		return true

	case "push":
		branch := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "HEAD")
		output := runCmdWithTimeout(workspaceDir, 30*time.Second, "git", "push", "-u", "origin", branch)
		if output == "" {
			output = "푸시 완료"
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:       "🚀 Push",
			Description: "```\n" + discord.TruncateText(output, 4000) + "\n```",
			Color:       discord.ColorSuccess,
			Fields: []discord.EmbedField{
				{Name: "브랜치", Value: "`" + branch + "`", Inline: true},
			},
		}})
		return true

	case "file", "cat":
		// /file <path> [startLine] [endLine] — view file with syntax highlighting.
		parts := strings.Fields(text)
		if len(parts) < 2 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "📄 File", Description: "사용법: `/file <경로> [시작줄] [끝줄]`", Color: discord.ColorWarning,
			}})
			return true
		}
		filePath := resolveWorkspacePath(workspaceDir, parts[1])
		startLine, endLine := 0, 0
		if len(parts) >= 3 {
			fmt.Sscanf(parts[2], "%d", &startLine)
		}
		if len(parts) >= 4 {
			fmt.Sscanf(parts[3], "%d", &endLine)
		}
		content, lineCount, err := readFileWithLines(filePath, startLine, endLine)
		if err != nil {
			p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatErrorEmbed(err.Error(), parts[1], 0)})
			return true
		}
		lang := discord.DetectCodeLanguage(parts[1])
		rangeInfo := ""
		if startLine > 0 || endLine > 0 {
			rangeInfo = fmt.Sprintf(" (L%d–L%d)", startLine, endLine)
		}
		// If content is small enough, send as embed code block.
		if len(content) < 3800 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title:       fmt.Sprintf("📄 %s%s", parts[1], rangeInfo),
				Description: "```" + lang + "\n" + content + "\n```",
				Color:       discord.ColorInfo,
				Footer:      &discord.EmbedFooter{Text: fmt.Sprintf("%d줄", lineCount)},
			}})
		} else {
			// Send as file attachment for large files.
			ext := discord.DetectCodeLanguage(parts[1])
			if ext == "" {
				ext = "txt"
			}
			p.sendDiscordFileReply(channelID, fmt.Sprintf("📄 **%s**%s (%d줄)", parts[1], rangeInfo, lineCount),
				parts[1], []byte(content))
		}
		return true

	case "grep", "search":
		// /grep <pattern> [glob] — search codebase with ripgrep.
		parts := strings.SplitN(text, " ", 3)
		if len(parts) < 2 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "🔎 Grep", Description: "사용법: `/grep <패턴> [파일패턴]`", Color: discord.ColorWarning,
			}})
			return true
		}
		pattern := parts[1]
		rgArgs := []string{"--no-heading", "--line-number", "--color=never", "-m", "50", pattern}
		if len(parts) >= 3 {
			rgArgs = append(rgArgs, "--glob", parts[2])
		}
		output := runCmdWithTimeout(workspaceDir, 15*time.Second, "rg", rgArgs...)
		if output == "" {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title:       "🔎 Grep: `" + pattern + "`",
				Description: "일치하는 결과 없음",
				Color:       discord.ColorInfo,
			}})
			return true
		}
		lines := strings.Split(output, "\n")
		matchCount := len(lines)
		if matchCount > 30 {
			lines = lines[:30]
			output = strings.Join(lines, "\n") + fmt.Sprintf("\n... 외 %d건", matchCount-30)
		}
		if len(output) < 3800 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title:       fmt.Sprintf("🔎 Grep: `%s` (%d건)", pattern, matchCount),
				Description: "```\n" + output + "\n```",
				Color:       discord.ColorInfo,
			}})
		} else {
			p.sendDiscordFileReply(channelID,
				fmt.Sprintf("🔎 **%s** — %d건 일치", pattern, matchCount),
				"grep-results.txt", []byte(output))
		}
		return true

	case "run", "exec":
		// /run <command> — execute a shell command directly and show output.
		parts := strings.SplitN(text, " ", 2)
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "⚡ Run", Description: "사용법: `/run <명령어>`", Color: discord.ColorWarning,
			}})
			return true
		}
		shellCmd := strings.TrimSpace(parts[1])
		start := time.Now()
		output := runCmdWithTimeout(workspaceDir, 30*time.Second, "bash", "-c", shellCmd)
		elapsed := time.Since(start)
		if output == "" {
			output = "(출력 없음)"
		}
		color := discord.ColorSuccess
		title := "⚡ " + discord.TruncateText(shellCmd, 200)
		if strings.Contains(strings.ToLower(output), "error") || strings.Contains(output, "FAIL") {
			color = discord.ColorError
		}
		if len(output) < 3800 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title:       title,
				Description: "```\n" + discord.TruncateText(output, 4000) + "\n```",
				Color:       color,
				Footer:      &discord.EmbedFooter{Text: fmt.Sprintf("%dms", elapsed.Milliseconds())},
			}})
		} else {
			p.sendDiscordFileReply(channelID,
				fmt.Sprintf("⚡ `%s` (%dms)", discord.TruncateText(shellCmd, 150), elapsed.Milliseconds()),
				"output.txt", []byte(output))
		}
		return true

	case "stash":
		// /stash [pop|list|show|drop] — git stash operations.
		parts := strings.Fields(text)
		subCmd := "push"
		if len(parts) > 1 {
			subCmd = parts[1]
		}
		var output string
		switch subCmd {
		case "list":
			output = runGitCmd(workspaceDir, "stash", "list")
			if output == "" {
				output = "스태시 없음"
			}
		case "pop":
			output = runGitCmd(workspaceDir, "stash", "pop")
		case "show":
			output = runGitCmd(workspaceDir, "stash", "show", "-p")
		case "drop":
			output = runGitCmd(workspaceDir, "stash", "drop")
		default:
			// Default: stash push with message.
			msg := "Discord stash"
			if len(parts) > 1 {
				msg = strings.Join(parts[1:], " ")
			}
			output = runGitCmd(workspaceDir, "stash", "push", "-m", msg)
		}
		if output == "" {
			output = "완료"
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:       "📦 Git Stash (" + subCmd + ")",
			Description: "```\n" + discord.TruncateText(output, 4000) + "\n```",
			Color:       discord.ColorInfo,
		}})
		return true

	case "checkout", "switch":
		// /checkout <branch> — switch git branch.
		parts := strings.Fields(text)
		if len(parts) < 2 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "🌿 Checkout", Description: "사용법: `/checkout <브랜치>`", Color: discord.ColorWarning,
			}})
			return true
		}
		branch := parts[1]
		// Try checkout first; if it fails, try creating a new branch.
		output := runGitCmd(workspaceDir, "checkout", branch)
		if output == "" || strings.Contains(output, "error") {
			output = runGitCmd(workspaceDir, "checkout", "-b", branch)
		}
		if output == "" {
			output = "`" + branch + "`(으)로 전환 완료"
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:       "🌿 Checkout",
			Description: "```\n" + discord.TruncateText(output, 4000) + "\n```",
			Color:       discord.ColorSuccess,
			Fields: []discord.EmbedField{
				{Name: "브랜치", Value: "`" + branch + "`", Inline: true},
			},
		}})
		return true

	case "blame":
		// /blame <file> [line] — show git blame for a file.
		parts := strings.Fields(text)
		if len(parts) < 2 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "🔍 Blame", Description: "사용법: `/blame <파일> [줄번호]`", Color: discord.ColorWarning,
			}})
			return true
		}
		filePath := parts[1]
		blameArgs := []string{"blame", "--no-color", filePath}
		if len(parts) >= 3 {
			lineNum := parts[2]
			blameArgs = []string{"blame", "--no-color", "-L", lineNum + "," + lineNum, filePath}
			// Support range: /blame file.go 10 20
			if len(parts) >= 4 {
				blameArgs = []string{"blame", "--no-color", "-L", lineNum + "," + parts[3], filePath}
			}
		}
		output := runCmdWithTimeout(workspaceDir, 10*time.Second, "git", blameArgs...)
		if output == "" {
			output = "blame 정보 없음"
		}
		if len(output) < 3800 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title:       "🔍 Blame: " + filePath,
				Description: "```\n" + output + "\n```",
				Color:       discord.ColorInfo,
			}})
		} else {
			p.sendDiscordFileReply(channelID,
				"🔍 **"+filePath+"** blame 결과",
				"blame.txt", []byte(output))
		}
		return true

	case "agents":
		// /agents — list active agent sessions with status.
		sessions := p.server.SessionManager.sessions.List()
		if len(sessions) == 0 {
			p.sendDiscordEmbed(channelID, []discord.Embed{{
				Title: "🤖 에이전트", Description: "활성 세션 없음", Color: discord.ColorInfo,
			}})
			return true
		}
		var fields []discord.EmbedField
		for _, s := range sessions {
			statusEmoji := "⬜"
			switch string(s.Status) {
			case "running":
				statusEmoji = "🔄"
			case "done":
				statusEmoji = "✅"
			case "failed":
				statusEmoji = "❌"
			case "killed":
				statusEmoji = "🛑"
			case "timeout":
				statusEmoji = "⏰"
			}
			value := statusEmoji + " " + string(s.Status)
			if s.Model != "" {
				value += " · `" + s.Model + "`"
			}
			if s.RuntimeMs != nil {
				value += fmt.Sprintf(" · %dms", *s.RuntimeMs)
			}
			name := s.Key
			if s.Label != "" {
				name = s.Label
			}
			fields = append(fields, discord.EmbedField{
				Name: name, Value: value, Inline: false,
			})
			if len(fields) >= 15 {
				break
			}
		}
		p.sendDiscordEmbed(channelID, []discord.Embed{{
			Title:  fmt.Sprintf("🤖 에이전트 세션 (%d)", len(sessions)),
			Color:  discord.ColorInfo,
			Fields: fields,
		}})
		return true

	case "help":
		// /help — show all available coding commands.
		p.sendDiscordEmbed(channelID, []discord.Embed{discord.FormatHelpEmbed()})
		return true
	}

	return false
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

	// Resolve workspace for the session.
	if p.server.discordPlug != nil {
		wsChannelID := strings.TrimPrefix(sessionKey, "discord:")
		if ws := p.server.discordPlug.Config().WorkspaceForChannel(wsChannelID); ws != "" {
			sendParams["workspaceDir"] = ws
		}
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

// detectProjectType determines the project type from marker files.
func detectProjectType(dir string) string {
	markers := map[string]string{
		"go.mod":          "go",
		"Cargo.toml":      "rust",
		"package.json":    "node",
		"pyproject.toml":  "python",
		"setup.py":        "python",
		"requirements.txt": "python",
		"Makefile":        "make",
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

// lintCommand returns the lint command for a project type.
func lintCommand(projType string) (string, []string) {
	switch projType {
	case "go":
		return "go", []string{"vet", "./..."}
	case "rust":
		return "cargo", []string{"clippy"}
	case "node":
		return "npx", []string{"eslint", "."}
	case "python":
		return "python", []string{"-m", "ruff", "check", "."}
	}
	return "", nil
}

// resolveWorkspacePath joins a relative path to the workspace root. Prevents
// directory traversal by rejecting paths with "..".
func resolveWorkspacePath(workspaceDir, relPath string) string {
	if strings.Contains(relPath, "..") {
		return ""
	}
	if strings.HasPrefix(relPath, "/") {
		return relPath
	}
	return workspaceDir + "/" + relPath
}

// readFileWithLines reads a file and returns its content with optional line range.
// Returns content, total line count, and error.
func readFileWithLines(path string, startLine, endLine int) (string, int, error) {
	if path == "" {
		return "", 0, fmt.Errorf("잘못된 파일 경로")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("파일을 읽을 수 없습니다: %v", err)
	}

	allLines := strings.Split(string(data), "\n")
	totalLines := len(allLines)

	// Apply line range if specified.
	if startLine > 0 || endLine > 0 {
		if startLine < 1 {
			startLine = 1
		}
		if endLine < 1 || endLine > totalLines {
			endLine = totalLines
		}
		if startLine > totalLines {
			return "", totalLines, fmt.Errorf("시작 줄(%d)이 파일 길이(%d줄)를 초과합니다", startLine, totalLines)
		}
		// Add line numbers for ranged output.
		var numbered []string
		for i := startLine - 1; i < endLine && i < totalLines; i++ {
			numbered = append(numbered, fmt.Sprintf("%4d │ %s", i+1, allLines[i]))
		}
		return strings.Join(numbered, "\n"), endLine - startLine + 1, nil
	}

	// For full file, cap at 100 lines to keep Discord messages manageable.
	if totalLines > 100 {
		var numbered []string
		for i := 0; i < 100; i++ {
			numbered = append(numbered, fmt.Sprintf("%4d │ %s", i+1, allLines[i]))
		}
		return strings.Join(numbered, "\n") + fmt.Sprintf("\n... 외 %d줄", totalLines-100), totalLines, nil
	}

	var numbered []string
	for i, line := range allLines {
		numbered = append(numbered, fmt.Sprintf("%4d │ %s", i+1, line))
	}
	return strings.Join(numbered, "\n"), totalLines, nil
}
