// restore.go provides post-compaction file restoration.
//
// After LLM/emergency compaction summarizes old messages, file contents the
// agent was actively using are lost. This module extracts recently-read file
// records from pre-compaction messages and re-injects them as a restoration
// message, so the agent retains access to actively-edited files.
package compaction

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

const (
	restorationBudgetTokens = 50_000 // total budget for restoration message
	perFileBudgetTokens     = 5_000  // max tokens per restored file
	maxRestoredFiles        = 20     // cap on number of restored files
)

// fileReadTools lists tool names that read files. When these appear in
// tool_use blocks, the corresponding tool_result contains file content
// worth restoring after compaction.
var fileReadTools = map[string]struct{}{
	"read_file": {},
	"read":      {},
	"grep":      {},
}

// FileReadRecord captures a file read from the conversation history.
type FileReadRecord struct {
	Path    string // file path extracted from tool_use input
	Content string // content from the tool_result
	Tokens  int    // estimated token count
}

// ExtractRecentFileReads scans messages for tool_result blocks from file-reading
// tools. Returns records deduplicated by path (most recent wins), ordered most
// recent first.
func ExtractRecentFileReads(messages []llm.Message) []FileReadRecord {
	// Two-pass: first collect all tool_use IDs that are file reads with their paths,
	// then match tool_result blocks to extract content.
	type toolUseInfo struct {
		name string
		path string
	}
	toolUses := make(map[string]toolUseInfo) // tool_use_id -> info

	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			if _, ok := fileReadTools[b.Name]; !ok {
				continue
			}
			path := extractPathFromInput(b.Input)
			if path == "" {
				continue
			}
			toolUses[b.ID] = toolUseInfo{name: b.Name, path: path}
		}
	}

	if len(toolUses) == 0 {
		return nil
	}

	// Collect results: scan forward so later entries overwrite earlier (most recent wins).
	seen := make(map[string]int) // path -> index in records
	var records []FileReadRecord

	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_result" {
				continue
			}
			info, ok := toolUses[b.ToolUseID]
			if !ok {
				continue
			}
			content := b.Content
			if content == "" || b.IsError {
				continue
			}
			rec := FileReadRecord{
				Path:    info.path,
				Content: content,
				Tokens:  EstimateTokens(content),
			}
			if idx, exists := seen[info.path]; exists {
				records[idx] = rec // overwrite with more recent
			} else {
				seen[info.path] = len(records)
				records = append(records, rec)
			}
		}
	}

	// Reverse so most recent is first.
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	return records
}

// BuildRestorationMessages consolidates file read records into a single user
// message within the token budget. Returns nil if no records or budget exhausted.
func BuildRestorationMessages(records []FileReadRecord, budgetTokens int) []llm.Message {
	if len(records) == 0 {
		return nil
	}
	if budgetTokens <= 0 {
		budgetTokens = restorationBudgetTokens
	}

	var sb strings.Builder
	sb.WriteString("[컴팩션 후 파일 복원: 최근 읽은 파일 내용을 다시 제공합니다.]\n")

	used := EstimateTokens(sb.String())
	count := 0
	for _, rec := range records {
		if count >= maxRestoredFiles {
			break
		}
		tokens := rec.Tokens
		if tokens > perFileBudgetTokens {
			// Truncate content to per-file budget.
			runes := []rune(rec.Content)
			maxRunes := perFileBudgetTokens * runesPerToken
			if maxRunes < len(runes) {
				rec.Content = string(runes[:maxRunes]) + "\n... (truncated)"
				tokens = perFileBudgetTokens
			}
		}
		if used+tokens > budgetTokens {
			break
		}
		fmt.Fprintf(&sb, "\n--- %s ---\n%s\n", rec.Path, rec.Content)
		used += tokens
		count++
	}

	if count == 0 {
		return nil
	}

	// Return as a user+assistant pair so that inserting these before the
	// current user turn does not create two consecutive user messages, which
	// violates the strict role-alternation requirement of most LLM APIs.
	return []llm.Message{
		llm.NewTextMessage("user", sb.String()),
		llm.NewTextMessage("assistant", "파일 내용 복원 완료."),
	}
}

// StripImageBlocks removes image and document content blocks from messages,
// replacing them with text stubs. This prevents compaction API calls from
// hitting prompt-too-long errors on image-heavy sessions.
func StripImageBlocks(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content, &blocks) != nil {
			out[i] = msg
			continue
		}

		modified := false
		filtered := make([]llm.ContentBlock, 0, len(blocks))
		for _, b := range blocks {
			switch b.Type {
			case "image":
				filtered = append(filtered, llm.ContentBlock{
					Type: "text",
					Text: "[image removed for compaction]",
				})
				modified = true
			default:
				// Check for image_url blocks (nested struct).
				if b.ImageURL != nil {
					filtered = append(filtered, llm.ContentBlock{
						Type: "text",
						Text: "[image removed for compaction]",
					})
					modified = true
				} else {
					filtered = append(filtered, b)
				}
			}
		}

		if modified {
			raw, _ := json.Marshal(filtered)
			out[i] = llm.Message{Role: msg.Role, Content: raw}
		} else {
			out[i] = msg
		}
	}
	return out
}

// extractPathFromInput parses the file path from a tool_use input JSON.
// Looks for common field names: "path", "file_path", "file", "pattern".
func extractPathFromInput(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(input, &fields) != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "file", "pattern"} {
		if raw, ok := fields[key]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}
