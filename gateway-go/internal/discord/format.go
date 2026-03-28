package discord

import "strings"

// Discord message limits.
const (
	// MaxMessageLength is the Discord hard limit for message content.
	MaxMessageLength = 2000
)

// ChunkText splits text into chunks that fit within Discord's limit.
// Tries to split at code block boundaries, then newlines, then spaces.
func ChunkText(text string, maxLen int) []string {
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

		splitAt := findSplitPoint(remaining, maxLen)
		chunks = append(chunks, remaining[:splitAt])
		remaining = remaining[splitAt:]
	}

	return chunks
}

// findSplitPoint finds a good split point within maxLen.
func findSplitPoint(text string, maxLen int) int {
	// Try to split at a code block boundary.
	if idx := strings.LastIndex(text[:maxLen], "\n```"); idx > maxLen/4 {
		return idx + 1 // Include the newline, split before ```
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

	// Hard split at maxLen.
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
