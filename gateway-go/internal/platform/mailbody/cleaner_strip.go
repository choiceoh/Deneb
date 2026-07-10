package mailbody

import (
	"html"
	"strings"
	"unicode/utf8"
)

// Line-level normalization and head/tail strip passes of the mail-body
// cleaner — split from cleaner.go (pure move).

func stripBodyPrepForwardedHeadArtifacts(lines []string, result *CleanResult) []string {
	var removed int
	lines, removed = stripBodyPrepHeadAttachmentBlock(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "attachment", Lines: removed})
	}
	lines, removed = stripBodyPrepHeadInlineAttachmentPrefix(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "attachment", Lines: removed})
	}
	lines, removed = stripBodyPrepHeadReplyHeaderBlock(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history-header", Lines: removed})
	}
	return compactBodyPrepBlankLines(lines)
}

func splitBodyPrepLines(s string) []string {
	raw := strings.Split(expandBodyPrepSoftBreaks(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = normalizeBodyPrepLine(line)
		lines = append(lines, splitBodyPrepInlineReplyHeaders(line)...)
	}
	return lines
}

func expandBodyPrepSoftBreaks(s string) string {
	for _, marker := range []string{"\u200b", "\u200c", "\u200d", "\ufeff"} {
		s = strings.ReplaceAll(s, marker, "\n")
	}
	return s
}

func splitBodyPrepInlineReplyHeaders(line string) []string {
	if utf8.RuneCountInString(line) < 180 {
		return []string{line}
	}
	matches := bodyPrepInlineReplyHeaderRE.FindAllStringIndex(line, -1)
	if len(matches) < 2 {
		return []string{line}
	}
	out := make([]string, 0, len(matches)+1)
	start := 0
	for _, match := range matches {
		if match[0] <= start {
			continue
		}
		part := strings.TrimSpace(line[start:match[0]])
		if part != "" {
			out = append(out, part)
		}
		start = match[0] + 1
	}
	if tail := strings.TrimSpace(line[start:]); tail != "" {
		out = append(out, tail)
	}
	if len(out) < 3 {
		return []string{line}
	}
	return out
}

func normalizeBodyPrepLine(line string) string {
	line = html.UnescapeString(line)
	line = strings.ReplaceAll(line, "\u00a0", " ")
	line = cleanBodyPrepInlineHTMLLine(line)
	if bodyPrepLooksLikeFinancialDocumentLine(line) {
		line = cleanBodyPrepFinancialLine(line)
	}
	line = strings.TrimRight(line, " \t\u200b\u200c\u200d\ufeff")
	if strings.Trim(strings.TrimSpace(line), "\u200b\u200c\u200d\ufeff") == "" || bodyPrepHTMLBlankRE.MatchString(line) || bodyPrepHTMLWrapperRE.MatchString(line) {
		return ""
	}
	return line
}

func cleanBodyPrepInlineHTMLLine(line string) string {
	if !bodyPrepHTMLInlineTagRE.MatchString(line) {
		return line
	}
	if bodyPrepHTMLImageOnlyRE.MatchString(line) {
		return ""
	}
	line = bodyPrepHTMLBreakTagRE.ReplaceAllString(line, " ")
	line = bodyPrepHTMLTagRE.ReplaceAllString(line, " ")
	line = strings.Join(strings.Fields(line), " ")
	return bodyPrepSpaceBeforePunctRE.ReplaceAllString(line, "$1")
}

func cleanBodyPrepFinancialLine(line string) string {
	for _, re := range bodyPrepReceiptInlineREs {
		line = re.ReplaceAllString(line, " ")
	}
	line = bodyPrepReceiptSupportRE.ReplaceAllString(line, "")
	return strings.Join(strings.Fields(line), " ")
}

func stripBodyPrepHeadNoise(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	limit := len(lines)
	if limit > bodyPrepHeadNoiseMaxLines {
		limit = bodyPrepHeadNoiseMaxLines
	}
	cut := 0
	for cut < limit {
		line := strings.TrimSpace(lines[cut])
		if line == "" || bodyPrepLooksLikeSeparatorLine(line) || bodyPrepLooksLikeHeadNoiseLine(line) {
			cut++
			continue
		}
		break
	}
	if cut == 0 || cut >= len(lines) {
		return lines, 0
	}
	return lines[cut:], bodyPrepNonBlankLineCount(lines[:cut])
}

func stripBodyPrepHeadAttachmentBlock(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || !bodyPrepAttachmentLeadRE.MatchString(strings.TrimSpace(lines[start])) {
		return lines, 0
	}

	cut := start
	for cut < len(lines) {
		line := strings.TrimSpace(lines[cut])
		if line == "" || bodyPrepLooksLikeAttachmentMetaLine(line) {
			cut++
			continue
		}
		break
	}
	if cut <= start || cut >= len(lines) {
		if cut > start && cut >= len(lines) {
			return nil, bodyPrepNonBlankLineCount(lines[:cut])
		}
		return lines, 0
	}
	return lines[cut:], bodyPrepNonBlankLineCount(lines[:cut])
}

func stripBodyPrepHeadInlineAttachmentPrefix(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) {
		return lines, 0
	}
	line := strings.TrimSpace(lines[start])
	if !bodyPrepAttachmentHeadRE.MatchString(line) {
		return lines, 0
	}
	match := bodyPrepAttachmentBodyRE.FindStringIndex(line)
	if match == nil || match[0] <= 0 {
		return lines, 0
	}
	prefix := strings.TrimSpace(line[:match[0]])
	suffix := strings.TrimSpace(line[match[0]:])
	if visibleBodyPrepRunes(prefix) < 20 || visibleBodyPrepRunes(suffix) < 12 {
		return lines, 0
	}
	if !bodyPrepLooksLikeAttachmentMetaLine(prefix) && !bodyPrepAttachmentHeadRE.MatchString(prefix) {
		return lines, 0
	}
	out := append([]string{}, lines...)
	out[start] = suffix
	return out, 1
}

func stripBodyPrepHeadReceiptVendorBlock(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || !bodyPrepPrefixHasFinancialDocument(lines[start:], 5) {
		return lines, 0
	}
	cut := start
	for cut < len(lines) {
		line := strings.TrimSpace(lines[cut])
		if line == "" || bodyPrepReceiptVendorRE.MatchString(line) {
			cut++
			continue
		}
		break
	}
	if cut <= start || cut >= len(lines) {
		return lines, 0
	}
	return lines[cut:], bodyPrepNonBlankLineCount(lines[:cut])
}

func stripBodyPrepDecorativeSeparatorLines(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	out := make([]string, 0, len(lines))
	removed := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line != "" && bodyPrepLooksLikeSeparatorLine(line) {
			removed++
			continue
		}
		out = append(out, raw)
	}
	if removed == 0 || bodyPrepNonBlankLineCount(out) == 0 {
		return lines, 0
	}
	return compactBodyPrepBlankLines(out), removed
}

func stripBodyPrepInlineTailNoise(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || utf8.RuneCountInString(line) < 120 {
			continue
		}
		trimmed, ok := trimBodyPrepInlineTailNoise(line)
		if !ok {
			continue
		}
		out := append([]string{}, lines...)
		out[i] = trimmed
		if !bodyPrepLinesVisibleEnough(out) {
			return lines, 0
		}
		return compactBodyPrepBlankLines(out), 1
	}
	return lines, 0
}

func trimBodyPrepInlineTailNoise(line string) (string, bool) {
	cut := -1
	if loc := bodyPrepInlineClosingSignatureRE.FindStringIndex(line); loc != nil {
		cut = loc[0]
	}
	if loc := bodyPrepInlineEnglishSignatureRE.FindStringIndex(line); loc != nil && (cut < 0 || loc[0] < cut) {
		cut = loc[0]
	}
	if loc := bodyPrepInlineFooterLeadRE.FindStringIndex(line); loc != nil && (cut < 0 || loc[0] < cut) {
		cut = loc[0]
	}
	if cut < 0 {
		return line, false
	}
	prefix := strings.TrimSpace(line[:cut])
	if visibleBodyPrepRunes(prefix) < bodyPrepMinPrefixVisible {
		return line, false
	}
	return prefix, true
}

func stripBodyPrepHeadReplyHeaderBlock(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) {
		return lines, 0
	}

	signals := 0
	limit := start + 8
	if limit > len(lines) {
		limit = len(lines)
	}
	for i := start; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || bodyPrepLooksLikeSeparatorLine(line) || bodyPrepHTMLMetaRE.MatchString(line) {
			continue
		}
		if bodyPrepLooksLikeReplyHeaderLine(line) {
			signals++
			continue
		}
		break
	}
	if signals < 2 {
		return lines, 0
	}

	cut := start
	for cut < len(lines) {
		line := strings.TrimSpace(lines[cut])
		if line == "" || bodyPrepLooksLikeSeparatorLine(line) || bodyPrepLooksLikeReplyHeaderLine(line) || bodyPrepHTMLMetaRE.MatchString(line) {
			cut++
			continue
		}
		break
	}
	if cut <= start || cut >= len(lines) {
		return lines, 0
	}
	return lines[cut:], bodyPrepNonBlankLineCount(lines[:cut])
}

func stripBodyPrepTrailingNoiseLines(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	cut := len(lines)
	for cut > 0 {
		line := strings.TrimSpace(lines[cut-1])
		if line == "" || bodyPrepLooksLikeTrailingNoiseLine(line) {
			cut--
			continue
		}
		break
	}
	if cut == len(lines) || cut == 0 {
		return lines, 0
	}
	if !bodyPrepCutLeavesUsablePrefix(lines, cut) {
		return lines, 0
	}
	return lines[:cut], bodyPrepNonBlankLineCount(lines[cut:])
}

func compactBodyPrepBlankLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		out = append(out, line)
		blank = false
	}
	return out
}
