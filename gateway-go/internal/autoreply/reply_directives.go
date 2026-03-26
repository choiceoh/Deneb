// reply_directives.go — Reply directive processing (media splitting, threading, silent).
// Mirrors src/auto-reply/reply/reply-directives.ts (49 LOC) and
// src/media/parse.ts splitMediaFromOutput (170 LOC).
//
// Key behaviors:
// - Extracts MEDIA: tokens from reply text (with fence protection)
// - Detects [[audio_as_voice]] and [[voice]] tags
// - Extracts [[reply_to_current]] and [[reply_to:<id>]] threading tags
// - Detects silent reply tokens (NO_REPLY)
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"regexp"
	"strings"
)

// ReplyDirectiveParseResult holds the result of parsing reply directives.
type ReplyDirectiveParseResult struct {
	Text           string
	MediaURLs      []string
	MediaURL       string
	ReplyToID      string
	ReplyToCurrent bool
	ReplyToTag     bool
	AudioAsVoice   bool
	IsSilent       bool
}

// --- MEDIA: token parsing (mirrors src/media/parse.ts) ---

// mediaTokenRe matches MEDIA: tokens in text.
var mediaTokenRe = regexp.MustCompile(`(?i)\bMEDIA:\s*` + "`?" + `([^\n]+)` + "`?")

// httpURLRe matches http/https URLs.
var httpURLRe = regexp.MustCompile(`^https?://\S+`)

// fenceRe matches code fence markers.
var fenceRe = regexp.MustCompile("^\\s*(?:```|~~~)")

// windowsDriveRe matches Windows drive paths.
var windowsDriveRe = regexp.MustCompile(`^[a-zA-Z]:[/\\]`)

// schemeRe matches URI schemes.
var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// hasFileExt matches file extensions.
var hasFileExtRe = regexp.MustCompile(`\.\w{1,10}$`)

// isLikelyLocalPath checks if a candidate looks like a local file path.
func isLikelyLocalPath(candidate string) bool {
	return strings.HasPrefix(candidate, "/") ||
		strings.HasPrefix(candidate, "./") ||
		strings.HasPrefix(candidate, "../") ||
		strings.HasPrefix(candidate, "~") ||
		windowsDriveRe.MatchString(candidate) ||
		strings.HasPrefix(candidate, `\\`) ||
		(!schemeRe.MatchString(candidate) && (strings.Contains(candidate, "/") || strings.Contains(candidate, `\`)))
}

// isValidMedia checks if a candidate is a valid media reference.
func isValidMedia(candidate string, allowSpaces bool) bool {
	if candidate == "" || len(candidate) > 4096 {
		return false
	}
	if !allowSpaces && strings.ContainsAny(candidate, " \t\r\n") {
		return false
	}
	if httpURLRe.MatchString(candidate) {
		return true
	}
	return isLikelyLocalPath(candidate)
}

// cleanCandidate strips surrounding quotes/brackets from a media candidate.
func cleanCandidate(raw string) string {
	s := strings.TrimLeft(raw, "`\"'[{(")
	return strings.TrimRight(s, "`\"'\\})],")
}

// normalizeMediaSource strips file:// prefix.
func normalizeMediaSource(src string) string {
	if strings.HasPrefix(src, "file://") {
		return src[7:]
	}
	return src
}

// fenceSpan tracks a fenced code block region.
type fenceSpan struct {
	start, end int
}

// parseFenceSpans finds all fenced code block regions in text.
func parseFenceSpans(text string) []fenceSpan {
	var spans []fenceSpan
	lines := strings.Split(text, "\n")
	offset := 0
	inFence := false
	fenceStart := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if inFence {
				spans = append(spans, fenceSpan{start: fenceStart, end: offset + len(line)})
				inFence = false
			} else {
				inFence = true
				fenceStart = offset
			}
		}
		offset += len(line) + 1 // +1 for newline
	}
	// Unclosed fence extends to end.
	if inFence {
		spans = append(spans, fenceSpan{start: fenceStart, end: len(text)})
	}
	return spans
}

func isInsideFence(spans []fenceSpan, offset int) bool {
	for _, s := range spans {
		if offset >= s.start && offset < s.end {
			return true
		}
	}
	return false
}

// audioAsVoiceTagRe matches [[audio_as_voice]] and [[voice]] tags.
var audioAsVoiceTagRe = regexp.MustCompile(`(?i)\[\[\s*(?:audio_as_voice|voice)\s*\]\]`)

// splitMediaFromOutput extracts MEDIA: tokens from output text.
// Matches the full TS implementation: fence-aware, supports local paths,
// strips MEDIA: lines, and detects audio tags.
func splitMediaFromOutput(raw string) (text string, mediaURLs []string, mediaURL string, audioAsVoice bool) {
	trimmedRaw := strings.TrimRight(raw, " \t\r\n")
	if strings.TrimSpace(trimmedRaw) == "" {
		return "", nil, "", false
	}

	hasMediaToken := strings.Contains(strings.ToLower(trimmedRaw), "media:")
	hasAudioTag := strings.Contains(trimmedRaw, "[[")

	if !hasMediaToken && !hasAudioTag {
		return trimmedRaw, nil, "", false
	}

	var media []string
	hasFenceMarkers := strings.Contains(trimmedRaw, "```") || strings.Contains(trimmedRaw, "~~~")
	var fSpans []fenceSpan
	if hasFenceMarkers {
		fSpans = parseFenceSpans(trimmedRaw)
	}

	lines := strings.Split(trimmedRaw, "\n")
	var keptLines []string
	lineOffset := 0

	for _, line := range lines {
		// Skip MEDIA extraction inside fenced code blocks.
		if hasFenceMarkers && isInsideFence(fSpans, lineOffset) {
			keptLines = append(keptLines, line)
			lineOffset += len(line) + 1
			continue
		}

		trimmedStart := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(strings.ToUpper(trimmedStart), "MEDIA:") {
			keptLines = append(keptLines, line)
			lineOffset += len(line) + 1
			continue
		}

		matches := mediaTokenRe.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			keptLines = append(keptLines, line)
			lineOffset += len(line) + 1
			continue
		}

		foundMediaOnLine := false
		for _, m := range matches {
			payload := m[1]
			// Try each whitespace-separated part.
			parts := strings.Fields(payload)
			hasValid := false
			for _, part := range parts {
				candidate := normalizeMediaSource(cleanCandidate(part))
				if isValidMedia(candidate, false) {
					media = append(media, candidate)
					hasValid = true
				}
			}
			// Fallback: try entire payload as one path (with spaces).
			if !hasValid {
				candidate := normalizeMediaSource(cleanCandidate(strings.TrimSpace(payload)))
				if isValidMedia(candidate, true) {
					media = append(media, candidate)
					hasValid = true
				}
			}
			if hasValid {
				foundMediaOnLine = true
			}
		}

		if foundMediaOnLine {
			// Strip the MEDIA: line entirely if we extracted media from it.
			cleanedLine := mediaTokenRe.ReplaceAllString(line, "")
			cleanedLine = strings.TrimSpace(cleanedLine)
			if cleanedLine != "" {
				keptLines = append(keptLines, cleanedLine)
			}
		} else if isLikelyLocalPath(strings.TrimSpace(strings.SplitN(trimmedStart, ":", 2)[1])) {
			// Strip MEDIA: lines with local paths even when invalid.
			// They should never leak as visible text.
		} else {
			keptLines = append(keptLines, line)
		}
		lineOffset += len(line) + 1
	}

	cleanedText := strings.Join(keptLines, "\n")
	cleanedText = strings.TrimSpace(cleanedText)
	// Collapse multiple newlines.
	for strings.Contains(cleanedText, "\n\n\n") {
		cleanedText = strings.ReplaceAll(cleanedText, "\n\n\n", "\n\n")
	}

	// Detect and strip [[audio_as_voice]] tag.
	if audioAsVoiceTagRe.MatchString(cleanedText) {
		audioAsVoice = true
		cleanedText = audioAsVoiceTagRe.ReplaceAllString(cleanedText, "")
		cleanedText = strings.TrimSpace(cleanedText)
	}

	if len(media) == 0 {
		if audioAsVoice {
			return cleanedText, nil, "", true
		}
		return trimmedRaw, nil, "", false
	}

	return cleanedText, media, media[0], audioAsVoice
}

// ParseReplyDirectives parses reply directives from raw agent output text.
// Extracts MEDIA: tokens, threading tags, and silent tokens.
func ParseReplyDirectives(raw string, currentMessageID string, silentToken string) ReplyDirectiveParseResult {
	text, mediaURLs, mediaURL, audioAsVoice := splitMediaFromOutput(raw)

	// Extract reply threading tags.
	replyToID, replyToCurrent := tokens.ApplyReplyThreading(text, "")
	hasReplyTag := replyToCurrent || replyToID != ""

	if hasReplyTag {
		text = tokens.StripReplyTags(text)
	}

	// Apply current message ID for reply_to_current.
	if replyToCurrent && currentMessageID != "" {
		replyToID = currentMessageID
	}

	// Check for silent reply token.
	if silentToken == "" {
		silentToken = tokens.SilentReplyToken
	}
	isSilent := tokens.IsSilentReplyText(text, silentToken)
	if isSilent {
		text = ""
	}

	return ReplyDirectiveParseResult{
		Text:           text,
		MediaURLs:      mediaURLs,
		MediaURL:       mediaURL,
		ReplyToID:      replyToID,
		ReplyToCurrent: replyToCurrent,
		ReplyToTag:     hasReplyTag,
		AudioAsVoice:   audioAsVoice,
		IsSilent:       isSilent,
	}
}
