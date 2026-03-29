// Discord quick commands and button interaction dispatch.
package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

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
			p.sendDiscordEmbed(interaction.ChannelID, []discord.Embed{{
				Title:       "🚀 푸시 완료",
				Description: "`" + branch + "` 브랜치를 원격 저장소에 업로드했습니다.",
				Color:       discord.ColorSuccess,
			}})
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
