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
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
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

// fileReadPaths maps a tool_use ID to the file path that read requested.
type fileReadPaths map[string]string

// ExtractRecentFileReads scans messages for tool_result blocks from file-reading
// tools. Returns records deduplicated by path (most recent wins), ordered most
// recent first.
func ExtractRecentFileReads(messages []llm.Message) []FileReadRecord {
	// Two-pass: first collect all tool_use IDs that are file reads with their paths,
	// then match tool_result blocks to extract content.
	toolUses := collectFileReadToolUses(messages)
	if len(toolUses) == 0 {
		return nil
	}

	records := collectFileReadRecords(messages, toolUses)

	// Reverse so most recent is first.
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	return records
}

// collectFileReadToolUses gathers the tool_use IDs of file-reading calls that
// carry an extractable path, keyed by tool_use ID.
func collectFileReadToolUses(messages []llm.Message) fileReadPaths {
	toolUses := make(fileReadPaths)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content.Bytes(), &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			if _, ok := fileReadTools[b.Name]; !ok {
				continue
			}
			path := extractPathFromInput(json.RawMessage(b.Input.Bytes()))
			if path == "" {
				continue
			}
			toolUses[b.ID] = path
		}
	}
	return toolUses
}

// collectFileReadRecords matches tool_result blocks against the collected
// file-read tool uses, in message order. When the same path is re-read, the
// stale entry is dropped and re-appended so append order reflects actual read
// recency (the caller's final reverse then puts the most recent file first).
func collectFileReadRecords(messages []llm.Message, toolUses fileReadPaths) []FileReadRecord {
	seen := make(map[string]int) // path -> current index in records
	var records []FileReadRecord
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content.Bytes(), &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			records = appendFileReadRecord(records, seen, toolUses, b)
		}
	}
	return records
}

// appendFileReadRecord appends one tool_result block as a file-read record,
// evicting any stale record for the same path first. Non-file-read, empty, and
// error results pass through unchanged.
func appendFileReadRecord(records []FileReadRecord, seen map[string]int, toolUses fileReadPaths, b llm.ContentBlock) []FileReadRecord {
	if b.Type != "tool_result" {
		return records
	}
	path, ok := toolUses[b.ToolUseID]
	if !ok {
		return records
	}
	content := b.Content
	if content == "" || b.IsError {
		return records
	}
	rec := FileReadRecord{
		Path:    path,
		Content: content,
		Tokens:  EstimateTokens(content),
	}
	if idx, exists := seen[path]; exists {
		// Remove the stale record and fix up indices of everything
		// that shifted left so `seen` stays consistent.
		records = append(records[:idx], records[idx+1:]...)
		for p, i := range seen {
			if i > idx {
				seen[p] = i - 1
			}
		}
		delete(seen, path)
	}
	seen[path] = len(records)
	return append(records, rec)
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

// TruncateToolCallArgs shrinks oversized string leaves inside tool_use
// blocks' Input JSON. Raw byte-level truncation historically produced
// unterminated strings / missing braces, which providers reject as
// "invalid function arguments json string" — the session then gets stuck
// re-sending the same broken history every turn. Structural truncation
// via jsonutil.TruncateStringLeaves preserves validity so the LLM always
// sees well-formed JSON even after compaction shrinks long arguments.
//
// headChars bounds the maximum length of any single string value inside
// tool_use input. Non-string values pass through unchanged.
func TruncateToolCallArgs(messages []llm.Message, headChars int) []llm.Message {
	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content.Bytes(), &blocks) != nil {
			out[i] = msg
			continue
		}
		modified := false
		for bi, b := range blocks {
			if b.Type != "tool_use" || b.Input.IsZero() {
				continue
			}
			shrunk := jsonutil.TruncateStringLeaves(b.Input.String(), headChars)
			if shrunk != b.Input.String() {
				blocks[bi].Input = llm.FlexibleFromRaw([]byte(shrunk))
				modified = true
			}
		}
		if modified {
			raw, _ := json.Marshal(blocks)
			out[i] = llm.Message{Role: msg.Role, Content: llm.FlexibleFromRaw(raw)}
		} else {
			out[i] = msg
		}
	}
	return out
}

// StripImageBlocks removes image and document content blocks from messages,
// replacing them with text stubs. This prevents compaction API calls from
// hitting prompt-too-long errors on image-heavy sessions.
func StripImageBlocks(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content.Bytes(), &blocks) != nil {
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
			out[i] = llm.Message{Role: msg.Role, Content: llm.FlexibleFromRaw(raw)}
		} else {
			out[i] = msg
		}
	}
	return out
}

// StripThinkingBlocks removes extended-thinking blocks ("thinking" and
// "redacted_thinking") from messages, returning the rewritten slice and the
// number of blocks removed.
//
// Anthropic-compatible endpoints reject a request with a 400 when an echoed
// thinking block carries an invalid or stale cryptographic signature — for
// example after a mid-loop compaction shifts the prefix the block was signed
// against, or when signed reasoning is replayed to a model that did not mint
// it. The classified recovery (llmerr.ReasonThinkingSignature ->
// Action.StripThink) is to drop the thinking blocks and retry; the reasoning
// text is non-user-facing, so losing it on a recovery retry costs nothing.
//
// An assistant turn that becomes empty (thinking-only) is left with an empty
// block array — the Anthropic converter already skips empty/thinking-only
// assistant messages, and no tool_result depends on a thinking-only turn.
func StripThinkingBlocks(messages []llm.Message) ([]llm.Message, int) {
	out := make([]llm.Message, len(messages))
	stripped := 0
	for i, msg := range messages {
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content.Bytes(), &blocks) != nil {
			// String content (no blocks) — nothing to strip.
			out[i] = msg
			continue
		}
		modified := false
		filtered := make([]llm.ContentBlock, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "thinking" || b.Type == "redacted_thinking" {
				stripped++
				modified = true
				continue
			}
			filtered = append(filtered, b)
		}
		if modified {
			raw, _ := json.Marshal(filtered)
			out[i] = llm.Message{Role: msg.Role, Content: llm.FlexibleFromRaw(raw)}
		} else {
			out[i] = msg
		}
	}
	return out, stripped
}

// TruncateOldToolResults replaces the content of old tool_result blocks
// with a short placeholder when the content exceeds minChars runes.
// Operates on tool_result blocks older than turnThreshold assistant turns
// (the same boundary as MicroCompact). Zero-cost: no LLM call.
//
// This is Phase 1 cheap pruning in the Hermes Agent compaction model:
// before invoking an LLM summarizer, drop bulky old tool_result content
// that the agent rarely re-references. MicroCompact (which only strips
// code fences) typically runs first — blocks it shrinks below minChars
// fall through this pass unchanged.
//
// Protected results (protectedToolResultIDs) are exempt, with one carve-out:
// when the model habitually repeats the same protected call, older copies
// whose content is byte-identical to a newer surviving copy are stubbed too
// (supersededProtectedResultIDs) — the payload stays resident at the newest
// copy, so no re-fetch is provoked. minChars gates this as well: each stub
// breaks the prefix cache at its offset once, so only duplicates big enough
// to pay for that are cleared.
//
// Returns modified messages and the count of tool_result blocks that
// were stubbed.
func TruncateOldToolResults(messages []llm.Message, turnThreshold, minChars int) ([]llm.Message, int) {
	if len(messages) == 0 || turnThreshold <= 0 || minChars <= 0 {
		return messages, 0
	}

	var assistantIdx []int
	for i, m := range messages {
		if m.Role == "assistant" {
			assistantIdx = append(assistantIdx, i)
		}
	}
	if len(assistantIdx) <= turnThreshold {
		return messages, 0
	}
	cutoff := assistantIdx[len(assistantIdx)-turnThreshold]

	const (
		placeholder = "[older tool output cleared to save context]"
		// duplicatePlaceholder marks a protected result stubbed only because a
		// newer identical call retains the same content — pointing the model at
		// the resident copy instead of provoking a re-fetch.
		duplicatePlaceholder = "[duplicate tool output cleared — an identical newer call in this session retains the full result]"
	)

	protected := protectedToolResultIDs(messages)
	superseded := supersededProtectedResultIDs(messages, protected)

	stubbed := 0
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	for i := 0; i < cutoff; i++ {
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(messages[i].Content.Bytes(), &blocks); err != nil {
			continue
		}
		changed := false
		for j := range blocks {
			if blocks[j].Type != "tool_result" {
				continue
			}
			if protected[blocks[j].ToolUseID] && !superseded[blocks[j].ToolUseID] {
				continue // tool schema payload — stubbing it forces a re-fetch
			}
			if utf8.RuneCountInString(blocks[j].Content) <= minChars {
				continue
			}
			original := blocks[j].Content
			// A spilled result's full output still lives on disk; keep its
			// read_spillover pointer in the stub so the agent can still page
			// through it instead of losing access when the marker is cleared.
			if ref := spilloverRef(original); ref != "" {
				blocks[j].Content = fmt.Sprintf(
					"[older tool output cleared — full output still available via read_spillover(%q)]", ref,
				)
			} else if superseded[blocks[j].ToolUseID] {
				blocks[j].Content = duplicatePlaceholder
			} else {
				blocks[j].Content = placeholder
			}
			// Deferred-tool activation notices must survive the stub: the next
			// run's history replay (chat/deferred_replay.go) re-derives which
			// deferred tools stay active from these lines. One line kept per
			// notice — the body still compacts.
			for _, notice := range chatport.ExtractActivationNotices(original) {
				blocks[j].Content += "\n" + notice
			}
			changed = true
			stubbed++
		}
		if changed {
			if raw, err := json.Marshal(blocks); err == nil {
				result[i] = llm.Message{Role: messages[i].Role, Content: llm.FlexibleFromRaw(raw)}
			}
		}
	}
	return result, stubbed
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
