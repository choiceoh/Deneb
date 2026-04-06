// Package mediatokens extracts MEDIA: tokens from LLM output text.
//
// Ported from core-rs/core/src/parsing/media_tokens.rs.
// Extracts MEDIA: <url/path> tokens from text while respecting fenced code
// blocks, and detects [[audio_as_voice]] tags.
package mediatokens

import (
	"strings"
)

// Result of media token parsing.
type Result struct {
	Text         string   // Cleaned text (MEDIA lines removed).
	MediaURLs    []string // Extracted URLs/paths.
	AudioAsVoice bool     // [[audio_as_voice]] detected.
}

// Parse extracts MEDIA: tokens from text output.
// Returns cleaned text with MEDIA lines removed, extracted media URLs,
// and whether [[audio_as_voice]] was present.
func Parse(raw string) Result {
	trimmedRaw := strings.TrimRight(raw, " \t\n\r")
	if strings.TrimSpace(trimmedRaw) == "" {
		return Result{}
	}

	hasMediaToken := containsIgnoreCase(trimmedRaw, "media:")
	hasAudioTag := strings.Contains(trimmedRaw, "[[")

	if !hasMediaToken && !hasAudioTag {
		return Result{Text: trimmedRaw}
	}

	hasFenceMarkers := strings.Contains(trimmedRaw, "```") || strings.Contains(trimmedRaw, "~~~")
	var fenceSpans []fenceSpan
	if hasFenceMarkers {
		fenceSpans = parseFenceSpans(trimmedRaw)
	}

	lines := strings.Split(trimmedRaw, "\n")
	var keptLines []string
	var media []string
	foundMediaToken := false
	lineOffset := 0

	for _, line := range lines {
		// Skip MEDIA extraction inside fenced code blocks.
		if hasFenceMarkers && isInsideFence(fenceSpans, lineOffset) {
			keptLines = append(keptLines, line)
			lineOffset += len(line) + 1
			continue
		}

		trimmedStart := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmedStart, "MEDIA:") {
			keptLines = append(keptLines, line)
			lineOffset += len(line) + 1
			continue
		}

		// Extract payload after "MEDIA:".
		payload := strings.TrimSpace(trimmedStart[6:])
		// Strip optional wrapping backtick.
		payload = strings.TrimPrefix(payload, "`")
		payload = strings.TrimSuffix(payload, "`")
		payload = strings.TrimSpace(payload)

		if payload == "" {
			keptLines = append(keptLines, line)
			lineOffset += len(line) + 1
			continue
		}

		anyValid := false

		// Stage 1: Try unwrapping quoted payload.
		if unquoted, ok := tryUnwrapQuoted(payload); ok {
			candidate := cleanCandidate(unquoted)
			normalized := normalizeMediaSource(candidate)
			if isValidMediaAllowSpaces(normalized) || isBareFilename(normalized) {
				media = append(media, normalized)
				anyValid = true
				foundMediaToken = true
			}
		}

		// Stage 2: Try each space-separated part.
		if !anyValid {
			for _, part := range strings.Fields(payload) {
				candidate := cleanCandidate(part)
				normalized := normalizeMediaSource(candidate)
				if isValidMedia(normalized) {
					media = append(media, normalized)
					anyValid = true
					foundMediaToken = true
				}
			}
		}

		// Stage 3: Fallback — try entire payload as a single path.
		if !anyValid {
			candidate := cleanCandidate(payload)
			normalized := normalizeMediaSource(candidate)
			if isValidMediaAllowSpaces(normalized) || isBareFilename(normalized) {
				media = append(media, normalized)
				foundMediaToken = true
				anyValid = true
			}
		}

		if !anyValid {
			candidate := cleanCandidate(payload)
			if isLikelyLocalPath(candidate) {
				foundMediaToken = true
				// Drop the line.
			} else {
				keptLines = append(keptLines, line)
			}
		}

		lineOffset += len(line) + 1
	}

	cleanedText := strings.Join(keptLines, "\n")
	cleanedText = collapseWhitespace(cleanedText)

	// Strip [[audio_as_voice]] tag.
	cleanedText, audioAsVoice := stripAudioTag(cleanedText)
	if audioAsVoice {
		cleanedText = collapseWhitespace(cleanedText)
	}
	cleanedText = strings.TrimSpace(cleanedText)

	if len(media) == 0 {
		text := trimmedRaw
		if foundMediaToken || audioAsVoice {
			text = cleanedText
		}
		return Result{Text: text, AudioAsVoice: audioAsVoice}
	}

	return Result{
		Text:         cleanedText,
		MediaURLs:    media,
		AudioAsVoice: audioAsVoice,
	}
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	sLen := len(s)
	subLen := len(substr)
	if subLen > sLen {
		return false
	}
	for i := 0; i <= sLen-subLen; i++ {
		match := true
		for j := 0; j < subLen; j++ {
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 'a' - 'A'
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 'a' - 'A'
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// fenceSpan marks a fenced code block region.
type fenceSpan struct {
	start, end int
}

func parseFenceSpans(text string) []fenceSpan {
	var spans []fenceSpan
	lines := strings.Split(text, "\n")
	inFence := false
	var fenceChar byte
	fenceLen := 0
	fenceStart := 0
	offset := 0

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if !inFence {
			if ch, n := detectFenceOpen(trimmed); n >= 3 {
				inFence = true
				fenceChar = ch
				fenceLen = n
				fenceStart = offset
			}
		} else if isFenceClose(trimmed, fenceChar, fenceLen) {
			spans = append(spans, fenceSpan{start: fenceStart, end: offset + len(line)})
			inFence = false
		}
		offset += len(line) + 1
	}

	// If fence was never closed, extend to end.
	if inFence {
		spans = append(spans, fenceSpan{start: fenceStart, end: len(text)})
	}
	return spans
}

func detectFenceOpen(trimmed string) (byte, int) {
	if len(trimmed) < 3 {
		return 0, 0
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	count := 0
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == ch {
			count++
		} else {
			break
		}
	}
	if count >= 3 {
		return ch, count
	}
	return 0, 0
}

func isFenceClose(trimmed string, fenceChar byte, fenceLen int) bool {
	if len(trimmed) < fenceLen {
		return false
	}
	count := 0
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == fenceChar {
			count++
		} else if trimmed[i] == ' ' || trimmed[i] == '\t' {
			break
		} else {
			return false
		}
	}
	return count >= fenceLen
}

func isInsideFence(spans []fenceSpan, offset int) bool {
	for _, s := range spans {
		if offset >= s.start && offset < s.end {
			return true
		}
	}
	return false
}

// isValidMedia checks if candidate looks like a valid media source (no whitespace).
func isValidMedia(candidate string) bool {
	if candidate == "" || len(candidate) > 4096 {
		return false
	}
	if strings.ContainsAny(candidate, " \t\n\r") {
		return false
	}
	return isValidMediaCore(candidate) || isBareFilename(candidate)
}

// isValidMediaAllowSpaces is like isValidMedia but allows whitespace.
func isValidMediaAllowSpaces(candidate string) bool {
	if candidate == "" || len(candidate) > 4096 {
		return false
	}
	return isValidMediaCore(candidate)
}

// isValidMediaCore checks URL or local path patterns.
func isValidMediaCore(candidate string) bool {
	if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
		return true
	}
	if strings.HasPrefix(candidate, "/") ||
		strings.HasPrefix(candidate, "./") ||
		strings.HasPrefix(candidate, "../") ||
		strings.HasPrefix(candidate, "~") {
		return true
	}
	// Windows drive letter.
	if len(candidate) >= 3 &&
		isASCIIAlpha(candidate[0]) &&
		candidate[1] == ':' &&
		(candidate[2] == '\\' || candidate[2] == '/') {
		return true
	}
	// UNC path.
	if strings.HasPrefix(candidate, "\\\\") {
		return true
	}
	return false
}

// isBareFilename checks if candidate is a bare filename with extension (1-10 chars).
func isBareFilename(candidate string) bool {
	if candidate == "" || len(candidate) > 260 {
		return false
	}
	dotPos := strings.LastIndexByte(candidate, '.')
	if dotPos < 0 {
		return false
	}
	extLen := len(candidate) - dotPos - 1
	if extLen < 1 || extLen > 10 {
		return false
	}
	name := candidate[:dotPos]
	return name != "" && !strings.ContainsAny(name, "/\\")
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// tryUnwrapQuoted tries to extract content from a quoted string.
func tryUnwrapQuoted(payload string) (string, bool) {
	trimmed := strings.TrimSpace(payload)
	if len(trimmed) < 2 {
		return "", false
	}
	first := trimmed[0]
	last := trimmed[len(trimmed)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return trimmed[1 : len(trimmed)-1], true
	}
	return "", false
}

// cleanCandidate strips leading/trailing wrapping chars.
func cleanCandidate(raw string) string {
	s := raw
	for len(s) > 0 {
		switch s[0] {
		case '`', '"', '\'', '[', '{', '(':
			s = s[1:]
		default:
			goto doneLeading
		}
	}
doneLeading:
	for len(s) > 0 {
		switch s[len(s)-1] {
		case '`', '"', '\'', '\\', '}', ')', ']', ',':
			s = s[:len(s)-1]
		default:
			return s
		}
	}
	return s
}

// normalizeMediaSource strips file:// prefix.
func normalizeMediaSource(src string) string {
	if rest, ok := strings.CutPrefix(src, "file://"); ok {
		return rest
	}
	return src
}

func isLikelyLocalPath(candidate string) bool {
	return strings.HasPrefix(candidate, "/") ||
		strings.HasPrefix(candidate, "./") ||
		strings.HasPrefix(candidate, "../") ||
		strings.HasPrefix(candidate, "~") ||
		strings.HasPrefix(candidate, "file://") ||
		strings.HasPrefix(candidate, "\\\\")
}

// stripAudioTag strips [[audio_as_voice]] and [[voice]] directives.
func stripAudioTag(text string) (string, bool) {
	cleaned, directives := stripInlineDirectives(text)
	audioAsVoice := false
	for _, d := range directives {
		if d == "audio_as_voice" || d == "voice" {
			audioAsVoice = true
		}
	}
	return cleaned, audioAsVoice
}

// stripInlineDirectives parses and strips all [[...]] inline directives.
// Returns cleaned text and a list of directive keys.
func stripInlineDirectives(text string) (string, []string) {
	var keys []string
	var result strings.Builder
	result.Grow(len(text))
	rest := text

	for {
		start := strings.Index(rest, "[[")
		if start < 0 {
			result.WriteString(rest)
			break
		}
		endIdx := strings.Index(rest[start+2:], "]]")
		if endIdx < 0 {
			// No closing ]] — keep rest as-is.
			result.WriteString(rest)
			break
		}
		inner := rest[start+2 : start+2+endIdx]
		// Parse key=value or just key.
		key := inner
		if eqPos := strings.IndexByte(inner, '='); eqPos >= 0 {
			key = inner[:eqPos]
		}
		key = strings.TrimSpace(key)
		result.WriteString(rest[:start])
		keys = append(keys, key)
		rest = rest[start+2+endIdx+2:]
	}

	return result.String(), keys
}

// collapseWhitespace normalizes whitespace:
// - Trims trailing whitespace on each line
// - Collapses consecutive blank lines into one newline
// - Collapses consecutive spaces/tabs into a single space
func collapseWhitespace(input string) string {
	var out strings.Builder
	out.Grow(len(input))
	trailingWS := 0
	newlineCount := 0
	prevSpace := false

	for _, ch := range input {
		switch ch {
		case '\n':
			trailingWS = 0
			prevSpace = false
			newlineCount++
			if newlineCount <= 1 {
				out.WriteByte('\n')
			}
		case ' ', '\t':
			newlineCount = 0
			if !prevSpace {
				trailingWS++
			}
			prevSpace = true
		default:
			if trailingWS > 0 || prevSpace {
				out.WriteByte(' ')
				trailingWS = 0
			}
			prevSpace = false
			newlineCount = 0
			out.WriteRune(ch)
		}
	}
	return out.String()
}
