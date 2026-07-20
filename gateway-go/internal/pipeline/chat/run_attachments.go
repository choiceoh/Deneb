package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
)

// docExtractBudget bounds the TOTAL synchronous document-extraction time for
// one turn's attachments. Extraction (docparse; scanned-PDF OCR via the
// sidecar) was the one hot-path stage with no sub-budget: a multi-document
// turn could burn most of the 5-minute turn deadline before the first LLM
// call. Documents that do not finish inside the budget pass through
// unextracted — the same graceful path as an extraction failure — and the
// shortfall is logged.
const docExtractBudget = 2 * time.Minute

// hasImageAttachment returns true if any attachment is an image.
func hasImageAttachment(attachments []ChatAttachment) bool {
	for _, att := range attachments {
		if att.Type == "image" {
			return true
		}
	}
	return false
}

// hasDocumentAttachment reports whether the turn ingested a user document — a
// PDF/Office/CSV attachment (extractable), or one already converted to
// "document_text" by prepareDocumentAttachments. This is the hard anchor the
// deliverable auto-publish safety net gates on: no document this turn → no auto
// card, so ordinary chat can never trip it.
func hasDocumentAttachment(attachments []ChatAttachment) bool {
	for _, att := range attachments {
		if att.Type == "document_text" || toolwire.IsExtractableDocument(att.MimeType, att.Name) {
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
				// Base64-encoded inline image (from native attachment upload).
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
			// Server-extracted document text (PDF/Office/CSV), produced by
			// prepareDocumentAttachments from a raw native attachment.
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

// prepareDocumentAttachments converts raw document attachments (PDF, Office,
// CSV) into "document_text" attachments carrying server-extracted text. The
// native client sends these as base64 Data + a document MimeType with no
// explicit Type, so without this step they match neither the image nor the
// document_text branch in buildAttachmentBlocks and get silently dropped.
// Images and already-extracted text pass through unchanged. Oversized
// documents are digested by the local lightweight model when allowLocalAI is
// set (run_attachments_digest.go); otherwise they are visibly head-truncated.
func prepareDocumentAttachments(ctx context.Context, attachments []ChatAttachment, allowLocalAI bool) []ChatAttachment {
	if len(attachments) == 0 {
		return attachments
	}
	// Shared budget across ALL documents of the turn (concurrency.md rule 7:
	// bounded work on the request path). Once spent, remaining docs fall
	// through untouched like any other extraction failure.
	extractCtx, cancel := context.WithTimeout(ctx, docExtractBudget)
	defer cancel()
	var summarize chunkSummarizer
	if allowLocalAI {
		summarize = defaultChunkSummarizer()
	}
	budgetSpent := false
	usedRunes := 0
	out := make([]ChatAttachment, 0, len(attachments))
	for _, att := range attachments {
		if att.Type == "image" || att.Type == "document_text" || att.Data == "" ||
			!toolwire.IsExtractableDocument(att.MimeType, att.Name) {
			out = append(out, att)
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			out = append(out, att) // not base64 — leave untouched
			continue
		}
		if extractCtx.Err() != nil {
			if !budgetSpent {
				budgetSpent = true
				slog.Warn("document extraction budget spent; remaining attachments pass through unextracted",
					"budget", docExtractBudget, "attachment", att.Name)
			}
			out = append(out, att)
			continue
		}
		text, ok := toolwire.ExtractDocumentText(extractCtx, raw, att.Name, att.MimeType)
		if !ok || strings.TrimSpace(text) == "" {
			out = append(out, att) // extraction failed — leave untouched
			continue
		}
		label := att.Name
		if label == "" {
			label = "document"
		}
		// Per-doc inline allowance shrinks as the turn budget is spent by
		// earlier documents, floored so a later document never degrades to
		// uselessness. Digest output itself is small, so the loop converges.
		inlineCap := docInlineRuneCap
		if remaining := turnInlineRuneCap - usedRunes; remaining < inlineCap {
			inlineCap = remaining
			if inlineCap < docInlineRuneFloor {
				inlineCap = docInlineRuneFloor
			}
		}
		text = digestOversizedDocument(extractCtx, label, text, inlineCap, summarize)
		usedRunes += utf8.RuneCountInString(text)
		// document_text branch renders Data as the text body.
		out = append(out, ChatAttachment{Type: "document_text", Name: label, Data: text})
	}
	return out
}

// appendAttachmentsToHistory finds the last user message in the history and
// replaces it with a multimodal version that includes attachment content blocks.
// This is needed because transcript persistence stores text only; the
// attachments must be re-injected before sending to the LLM.
func appendAttachmentsToHistory(messages []llm.Message, text string, attachments []ChatAttachment) []llm.Message {
	// Find the last user message.
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
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
	if err := json.Unmarshal(msg.Content.Bytes(), &s); err == nil {
		return s
	}
	// Try as content block array.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}
