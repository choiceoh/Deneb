// Package server — Discord inbound message preprocessing via the autoreply pipeline.
//
// Bridges the autoreply command/directive system into the Discord → chat.send
// flow so that slash commands (/new, /model, /think, etc.) and inline directives
// are processed before the message reaches the LLM agent.
package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandleDiscordMessage processes an incoming Discord message through the
// autoreply pipeline: command detection → directive parsing → chat.send dispatch.
func (p *InboundProcessor) HandleDiscordMessage(msg *discord.Message) {
	if msg == nil || msg.Content == "" {
		return
	}

	channelID := msg.ChannelID
	sessionKey := "discord:" + channelID

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
	autoreply.FinalizeInboundContext(msgCtx)

	// Try slash command dispatch.
	trimmed := strings.TrimSpace(msgCtx.BodyForCommands)
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
				Deps: p.buildCommandDeps(),
			})
			if err == nil && result != nil && result.SkipAgent {
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

	// Build delivery context.
	delivery := map[string]any{
		"channel":   "discord",
		"to":        channelID,
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
