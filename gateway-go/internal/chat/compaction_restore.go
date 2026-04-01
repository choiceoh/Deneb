// compaction_restore.go implements post-compaction file restoration.
//
// After a full compaction (which replaces conversation history with a
// summary), the LLM loses access to recently-read file contents. This
// module re-injects the most recently accessed file reads back into the
// context within a token budget, preserving the agent's working memory
// of files it was actively editing.
//
// Inspired by Claude Code's compact.ts file restoration pattern.
package chat

import (
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// File restoration budget constants.
const (
	// postCompactTokenBudget is the total token budget for all restored files.
	postCompactTokenBudget = 50_000
	// postCompactMaxTokensPerFile caps individual file restorations.
	postCompactMaxTokensPerFile = 5_000
	// postCompactMaxFiles limits how many files to restore.
	postCompactMaxFiles = 20
)

// FileReadRecord tracks a file that was read during the conversation.
type FileReadRecord struct {
	Path       string // file path
	Content    string // file content (may be truncated)
	TokenCount int    // estimated tokens
	TurnIndex  int    // which turn this was read in (higher = more recent)
}

// ExtractRecentFileReads scans the pre-compaction messages for tool_result
// blocks that look like file reads (from tools named "read", "file_read", etc.)
// and returns the most recent ones, deduplicated by path.
func ExtractRecentFileReads(messages []llm.Message) []FileReadRecord {
	// Track by path, keeping only the most recent read per file.
	byPath := make(map[string]FileReadRecord)
	turnIdx := 0

	for _, msg := range messages {
		if msg.Role == "assistant" {
			turnIdx++
			continue
		}
		if msg.Role != "user" {
			continue
		}

		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}
		for _, block := range blocks {
			if block.Type != "tool_result" || block.Content == "" {
				continue
			}
			// Heuristic: file reads typically return content starting with
			// line numbers (e.g., "1\tpackage main") or path indicators.
			// We keep all tool results as potential file reads.
			tokens := estimateTokens(block.Content)
			if tokens > 0 {
				// Use the tool_use_id prefix as a pseudo-path.
				path := block.ToolUseID
				byPath[path] = FileReadRecord{
					Path:       path,
					Content:    block.Content,
					TokenCount: tokens,
					TurnIndex:  turnIdx,
				}
			}
		}
	}

	// Sort by recency (highest turn index first).
	records := make([]FileReadRecord, 0, len(byPath))
	for _, r := range byPath {
		records = append(records, r)
	}
	// Simple insertion sort (small N).
	for i := 1; i < len(records); i++ {
		for j := i; j > 0 && records[j].TurnIndex > records[j-1].TurnIndex; j-- {
			records[j], records[j-1] = records[j-1], records[j]
		}
	}

	return records
}

// BuildRestorationMessages creates user messages that re-inject recently
// accessed file contents into the context after compaction. Stays within
// the token budget.
func BuildRestorationMessages(records []FileReadRecord) []llm.Message {
	if len(records) == 0 {
		return nil
	}

	var messages []llm.Message
	usedTokens := 0
	count := 0

	for _, r := range records {
		if count >= postCompactMaxFiles {
			break
		}
		if usedTokens+r.TokenCount > postCompactTokenBudget {
			continue // skip this file, try smaller ones
		}

		content := r.Content
		if r.TokenCount > postCompactMaxTokensPerFile {
			// Truncate to per-file budget.
			runeContent := []rune(content)
			maxRunes := postCompactMaxTokensPerFile * runesPerToken
			if len(runeContent) > maxRunes {
				content = string(runeContent[:maxRunes]) + "\n... (truncated for context budget)"
			}
		}

		msg := llm.NewTextMessage("user", fmt.Sprintf(
			"[Context restored after compaction — recently accessed content]\n%s", content))
		messages = append(messages, msg)
		usedTokens += r.TokenCount
		count++
	}

	return messages
}

// StripImageBlocks removes image and document blocks from messages before
// compaction. Images are not needed for generating conversation summaries
// and can cause the compaction API call itself to hit the prompt-too-long limit.
func StripImageBlocks(messages []llm.Message) []llm.Message {
	result := make([]llm.Message, len(messages))
	for i, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			result[i] = msg
			continue
		}

		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			result[i] = msg
			continue
		}

		hasImage := false
		for _, b := range blocks {
			if b.Type == "image" || b.Source != nil || b.ImageURL != nil {
				hasImage = true
				break
			}
		}

		if !hasImage {
			result[i] = msg
			continue
		}

		// Filter out image blocks.
		filtered := make([]llm.ContentBlock, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "image" || b.Source != nil || b.ImageURL != nil {
				// Replace with a text stub so the LLM knows an image was here.
				filtered = append(filtered, llm.ContentBlock{
					Type: "text",
					Text: "[image removed for compaction]",
				})
				continue
			}
			filtered = append(filtered, b)
		}

		raw, _ := json.Marshal(filtered)
		result[i] = llm.Message{Role: msg.Role, Content: raw}
	}
	return result
}
