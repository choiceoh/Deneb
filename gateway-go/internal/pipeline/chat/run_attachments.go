package chat

import (
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// hasImageAttachment returns true if any attachment is an image.
func hasImageAttachment(attachments []ChatAttachment) bool {
	for _, att := range attachments {
		if att.Type == "image" {
			return true
		}
	}
	return false
}

// buildAttachmentBlocks creates a multimodal content block array from text and
// attachments. Images with base64 Data get inline ImageSource blocks;
// images with URL get URL-referenced blocks.
func buildAttachmentBlocks(text string, attachments []ChatAttachment) []llm.ContentBlock {
	blocks := make([]llm.ContentBlock, 0, len(attachments)+1)
	if text != "" {
		blocks = append(blocks, llm.ContentBlock{Type: "text", Text: text})
	}
	for _, att := range attachments {
		switch att.Type {
		case "image":
			if att.Data != "" {
				// Base64-encoded inline image (from Telegram download).
				blocks = append(blocks, llm.ContentBlock{
					Type: "image",
					Source: &llm.ImageSource{
						Type:      "base64",
						MediaType: att.MimeType,
						Data:      att.Data,
					},
				})
			} else if att.URL != "" {
				blocks = append(blocks, llm.ContentBlock{
					Type: "image",
					Source: &llm.ImageSource{
						Type:      "url",
						MediaType: att.MimeType,
						Data:      att.URL,
					},
				})
			}

		case "document_text":
			// Text extracted from a document (PDF, Office, etc.) via LiteParse.
			label := att.Name
			if label == "" {
				label = "document"
			}
			blocks = append(blocks, llm.ContentBlock{
				Type: "text",
				Text: fmt.Sprintf("[%s]\n\n%s", label, att.Data),
			})
		}
	}
	return blocks
}

// appendAttachmentsToHistory finds the last user message in the history and
// replaces it with a multimodal version that includes attachment content blocks.
// This is needed because transcript persistence stores text only; the
// attachments must be re-injected before sending to the LLM.
func appendAttachmentsToHistory(messages []llm.Message, text string, attachments []ChatAttachment) []llm.Message {
	// Find the last user message.
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		var role struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(messages[i].Content, &role); err == nil && role.Role == "" {
			// Content is a string, not structured. Check role from the Message.
		}
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	if lastUserIdx < 0 {
		// No user message in history; append a new multimodal message.
		blocks := buildAttachmentBlocks(text, attachments)
		return append(messages, llm.NewBlockMessage("user", blocks))
	}

	// Replace the last user message with a multimodal version.
	// Extract existing text from the message.
	existingText := extractTextFromMessage(messages[lastUserIdx])
	if existingText == "" {
		existingText = text
	}

	blocks := buildAttachmentBlocks(existingText, attachments)
	result := make([]llm.Message, len(messages))
	copy(result, messages)
	result[lastUserIdx] = llm.NewBlockMessage("user", blocks)
	return result
}

// extractTextFromMessage extracts the text content from a Message.
// Handles both string content and structured content block arrays.
func extractTextFromMessage(msg llm.Message) string {
	// Try as plain string first.
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		return s
	}
	// Try as content block array.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}
