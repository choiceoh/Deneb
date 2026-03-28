package discord

import (
	"fmt"
	"strings"
)

// Discord message limits.
const (
	// MaxMessageLength is the Discord hard limit for message content.
	MaxMessageLength = 2000
)

// ChunkText splits text into chunks that fit within Discord's limit.
// Code block-aware: never splits inside a fenced code block. If a code block
// would be split, it closes the block in the current chunk and reopens it
// in the next chunk with the same language tag.
func ChunkText(text string, maxLen int) []string {
	if maxLen <= 0 {
		return []string{text}
	}

	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		splitAt := findCodeBlockAwareSplit(remaining, maxLen)
		if splitAt <= 0 || splitAt > len(remaining) {
			if len(remaining) <= maxLen {
				splitAt = len(remaining)
			} else {
				splitAt = maxLen
			}
		}
		chunks = append(chunks, remaining[:splitAt])
		remaining = remaining[splitAt:]
	}

	return chunks
}

// findCodeBlockAwareSplit finds a split point that doesn't break code blocks.
// If we're inside a code block at the split point, it finds the best split
// outside a code block, or splits at the code block boundary.
func findCodeBlockAwareSplit(text string, maxLen int) int {
	splitAt := findSplitPoint(text, maxLen)
	candidate := text[:splitAt]
	if strings.Count(candidate, "```")%2 == 0 {
		return splitAt
	}

	// Unbalanced fences: prefer splitting before the opening fence.
	lastFence := strings.LastIndex(candidate, "```")
	if lastFence > 0 {
		return lastFence
	}

	// If text starts with a fence and the closing fence is near, keep the block
	// together even if that means a modestly oversized chunk.
	if lastFence == 0 {
		if closeRel := strings.Index(text[3:], "```"); closeRel >= 0 {
			closeEnd := 3 + closeRel + 3
			upperBound := maxLen + (maxLen / 4)
			if upperBound > MaxMessageLength {
				upperBound = MaxMessageLength
			}
			if closeEnd <= upperBound {
				return closeEnd
			}
		}
	}

	return splitAt
}

// lastOpenCodeBlock checks if text ends inside an unclosed fenced code block.
// Returns the index of the opening ``` and the language tag, or (-1, "") if not.
func lastOpenCodeBlock(text string) (int, string) {
	openCount := 0
	lastOpenIdx := -1
	lastLang := ""

	i := 0
	for i < len(text) {
		idx := strings.Index(text[i:], "```")
		if idx < 0 {
			break
		}
		pos := i + idx

		if openCount%2 == 0 {
			// Opening fence — extract language tag.
			lastOpenIdx = pos
			after := text[pos+3:]
			if nl := strings.IndexByte(after, '\n'); nl >= 0 {
				lastLang = strings.TrimSpace(after[:nl])
			} else {
				lastLang = ""
			}
		}
		openCount++
		i = pos + 3
	}

	// Odd count means we're inside an unclosed block.
	if openCount%2 == 1 {
		return lastOpenIdx, lastLang
	}
	return -1, ""
}

// findSplitPoint finds a good split point within maxLen for normal text
// (outside code blocks).
func findSplitPoint(text string, maxLen int) int {
	// Try to split at a code block boundary.
	if idx := strings.LastIndex(text[:maxLen], "\n```"); idx > maxLen/4 {
		return idx + 1
	}

	// Try to split at a double newline (paragraph boundary).
	if idx := strings.LastIndex(text[:maxLen], "\n\n"); idx > maxLen/4 {
		return idx + 1
	}

	// Try to split at a newline.
	if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/4 {
		return idx + 1
	}

	// Try to split at a space.
	if idx := strings.LastIndex(text[:maxLen], " "); idx > maxLen/4 {
		return idx + 1
	}

	return maxLen
}

// WrapCodeBlock wraps text in a Discord code block with optional language.
func WrapCodeBlock(text, lang string) string {
	var b strings.Builder
	b.WriteString("```")
	b.WriteString(lang)
	b.WriteByte('\n')
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```")
	return b.String()
}

// DetectCodeLanguage guesses the language from file extension or content.
func DetectCodeLanguage(filename string) string {
	ext := ""
	if idx := strings.LastIndex(filename, "."); idx >= 0 {
		ext = filename[idx+1:]
	}

	switch strings.ToLower(ext) {
	case "go":
		return "go"
	case "rs":
		return "rust"
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "sh", "bash":
		return "bash"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "toml":
		return "toml"
	case "md":
		return "markdown"
	case "sql":
		return "sql"
	case "html":
		return "html"
	case "css":
		return "css"
	case "diff", "patch":
		return "diff"
	case "proto":
		return "protobuf"
	default:
		return ""
	}
}

// TruncateForFile determines if text should be sent as a file attachment
// instead of inline. Returns true if the text exceeds the threshold.
func TruncateForFile(text string) bool {
	return len(text) > TextChunkLimit
}

// FormattedReply is the result of FormatReply processing.
type FormattedReply struct {
	// Text is the message text to send (may be modified from original).
	Text string
	// FileContent is non-nil when a large code block was extracted as a file.
	FileContent []byte
	// FileName is the name for the file attachment.
	FileName string
}

// FormatReply processes an agent reply for Discord delivery.
// If the reply contains a single large code block (>1500 chars), it extracts
// the code block as a file attachment and replaces it with a summary.
func FormatReply(text string) FormattedReply {
	// Try to extract a dominant code block.
	block, lang, before, after := extractLargestCodeBlock(text)
	if block == "" || len(block) < 800 {
		// If text fits in a single message, send as-is.
		if len(text) <= TextChunkLimit {
			return FormattedReply{Text: text}
		}
		// No large code block found — send as regular text.
		return FormattedReply{Text: text}
	}

	// Build file attachment from the code block.
	ext := ".txt"
	if lang != "" {
		ext = langToFileExt(lang)
	}

	// Build summary text from surrounding content.
	summary := strings.TrimSpace(before)
	if after := strings.TrimSpace(after); after != "" {
		if summary != "" {
			summary += "\n\n" + after
		} else {
			summary = after
		}
	}
	if summary == "" {
		lines := strings.Count(block, "\n") + 1
		if lang != "" {
			summary = fmt.Sprintf("📎 코드 출력 (%s, %d줄)", strings.TrimSpace(lang), lines)
		} else {
			summary = fmt.Sprintf("📎 코드 출력 (%d줄)", lines)
		}
	}

	return FormattedReply{
		Text:        summary,
		FileContent: []byte(block),
		FileName:    "output" + ext,
	}
}

// extractLargestCodeBlock finds the largest fenced code block in text.
// Returns the code content (without fences), language tag, text before, and text after.
func extractLargestCodeBlock(text string) (code, lang, before, after string) {
	bestLen := 0
	bestStart := -1
	bestEnd := -1
	bestLang := ""

	i := 0
	for {
		start := strings.Index(text[i:], "```")
		if start < 0 {
			break
		}
		start += i

		// Extract language tag.
		langEnd := strings.IndexByte(text[start+3:], '\n')
		if langEnd < 0 {
			break
		}
		blockLang := strings.TrimSpace(text[start+3 : start+3+langEnd])

		// Find closing fence.
		codeStart := start + 3 + langEnd + 1
		end := strings.Index(text[codeStart:], "```")
		if end < 0 {
			break
		}
		end += codeStart

		blockContent := text[codeStart:end]
		if len(blockContent) > bestLen {
			bestLen = len(blockContent)
			bestStart = start
			bestEnd = end + 3
			bestLang = blockLang
		}

		i = end + 3
	}

	if bestStart < 0 {
		return "", "", text, ""
	}

	return text[bestStart+3+len(bestLang)+1 : bestEnd-3], bestLang,
		text[:bestStart], text[bestEnd:]
}

// langToFileExt maps language identifiers to file extensions with dot.
func langToFileExt(lang string) string {
	switch strings.TrimSpace(lang) {
	case "go":
		return ".go"
	case "rust":
		return ".rs"
	case "python":
		return ".py"
	case "javascript", "js":
		return ".js"
	case "typescript", "ts":
		return ".ts"
	case "bash", "sh", "shell":
		return ".sh"
	case "json":
		return ".json"
	case "yaml", "yml":
		return ".yaml"
	case "diff":
		return ".diff"
	case "sql":
		return ".sql"
	case "html":
		return ".html"
	case "css":
		return ".css"
	case "proto", "protobuf":
		return ".proto"
	default:
		return ".txt"
	}
}
