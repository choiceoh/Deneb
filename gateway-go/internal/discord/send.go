package discord

import (
	"context"
	"fmt"
	"strings"
)

// SendResult holds the result of a send operation.
type SendResult struct {
	MessageID string `json:"messageId"`
	ChannelID string `json:"channelId"`
}

// SendText sends a text message to a Discord channel, automatically chunking
// if needed. For very long messages, sends as a file attachment.
func SendText(ctx context.Context, c *Client, channelID string, text string, replyToID string) ([]SendResult, error) {
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	// If text is very long, send as file attachment.
	if len(text) > MaxMessageLength*3 {
		return sendAsFile(ctx, c, channelID, text, replyToID)
	}

	chunks := ChunkText(text, TextChunkLimit)
	var results []SendResult

	for i, chunk := range chunks {
		req := &SendMessageRequest{
			Content: chunk,
			AllowedMentions: &AllowedMentions{
				Parse: []string{}, // Suppress all pings by default.
			},
		}

		// Only attach reply reference to the first chunk.
		if i == 0 && replyToID != "" {
			req.MessageReference = &MessageReference{
				MessageID: replyToID,
			}
		}

		msg, err := c.SendMessage(ctx, channelID, req)
		if err != nil {
			return results, fmt.Errorf("sendMessage chunk %d: %w", i, err)
		}

		results = append(results, SendResult{
			MessageID: msg.ID,
			ChannelID: msg.ChannelID,
		})
	}

	return results, nil
}

// SendCodeBlock sends text wrapped in a code block. If the content is too long,
// it's sent as a file attachment with the appropriate extension.
func SendCodeBlock(ctx context.Context, c *Client, channelID string, code string, lang string, replyToID string) ([]SendResult, error) {
	wrapped := WrapCodeBlock(code, lang)

	// If wrapped code fits in a message, send inline.
	if len(wrapped) <= TextChunkLimit {
		return SendText(ctx, c, channelID, wrapped, replyToID)
	}

	// Too long for inline — send as file attachment.
	ext := langToFileExt(lang)
	return sendAsFile(ctx, c, channelID, code, replyToID, "output"+ext)
}

// sendAsFile sends text as a file attachment. Optional fileName parameter.
func sendAsFile(ctx context.Context, c *Client, channelID string, text string, replyToID string, fileNames ...string) ([]SendResult, error) {
	fileName := "output.txt"
	if len(fileNames) > 0 && fileNames[0] != "" {
		fileName = fileNames[0]
	}

	// Determine summary for the message content.
	summary := summarizeContent(text)

	msg, err := c.SendMessageWithFile(ctx, channelID, summary, fileName, []byte(text))
	if err != nil {
		return nil, fmt.Errorf("sendFile: %w", err)
	}

	// Note: reply reference is not supported on file uploads via the simple API.
	_ = replyToID

	return []SendResult{{
		MessageID: msg.ID,
		ChannelID: msg.ChannelID,
	}}, nil
}

// summarizeContent creates a brief summary of the content being sent as a file.
func summarizeContent(text string) string {
	lines := strings.Count(text, "\n") + 1
	return fmt.Sprintf("📎 Output (%d lines, %d chars)", lines, len(text))
}

