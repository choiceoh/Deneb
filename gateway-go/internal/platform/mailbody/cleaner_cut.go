package mailbody

import (
	"strings"
	"unicode/utf8"
)

// Cut-line decisions (tail noise, reply history, signatures) and reply-body
// preservation of the mail-body cleaner — split from cleaner.go (pure move).

func bodyPrepTailNoiseCutLine(lines []string) int {
	if len(lines) == 0 {
		return -1
	}
	for i := bodyPrepTailStart(lines); i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeFinancialDocumentLine(line) {
			continue
		}
		if bodyPrepLooksLikeMeetingDetailLine(line) {
			continue
		}
		if bodyPrepLooksLikeFooterLine(line) {
			if cut := bodyPrepFooterCutLineWithSignatureLead(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
				return cut
			}
			continue
		}
		if bodyPrepLooksLikeSeparatorLine(line) && bodyPrepSuffixHasBoilerplateSignal(lines[i+1:]) {
			if bodyPrepSuffixHasPreservedReplyBody(lines[i:]) {
				continue
			}
			if bodyPrepCutLeavesVisiblePrefix(lines, i) {
				return i
			}
			continue
		}
		if bodyPrepLooksLikeReplyHeaderLine(line) && bodyPrepSuffixReplyHeaderSignalCount(lines[i:]) >= 2 {
			if bodyPrepSuffixHasPreservedReplyBody(lines[i:]) {
				continue
			}
			if cut := bodyPrepReplyCutLine(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
				return cut
			}
			continue
		}
	}
	return -1
}

func bodyPrepReplyHistoryCutLine(lines []string) int {
	if len(lines) == 0 {
		return -1
	}
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeStrongReplyBoundaryLine(line) {
			if bodyPrepSuffixHasPreservedReplyBody(lines[i:]) {
				continue
			}
			if cut := bodyPrepReplyCutLine(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
				return cut
			}
			continue
		}
		if !bodyPrepLooksLikeReplyHeaderLine(line) {
			continue
		}
		if bodyPrepLocalReplyHeaderSignalCount(lines[i:], 8) >= 2 {
			if bodyPrepSuffixHasPreservedReplyBody(lines[i:]) {
				continue
			}
			if cut := bodyPrepReplyCutLine(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
				return cut
			}
		}
	}
	return -1
}

func bodyPrepSignatureCutLine(lines []string) int {
	if len(lines) == 0 {
		return -1
	}
	for i := bodyPrepTailStart(lines); i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if (bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line)) && bodyPrepSuffixHasMeetingDetail(lines[i+1:], 6) {
			continue
		}
		if (bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line)) && bodyPrepSuffixStartsSignatureBlock(lines[i+1:]) {
			if bodyPrepSuffixHasPreservedReplyBody(lines[i+1:]) {
				continue
			}
			if bodyPrepCutLeavesUsablePrefix(lines, i) {
				return i
			}
			continue
		}
		if bodyPrepSuffixStartsBusinessBody(lines[i+1:]) {
			continue
		}
		if (bodyPrepLooksLikeSignatureLine(line) || bodyPrepLooksLikeTrailingNoiseLine(line)) && bodyPrepSuffixSignatureSignalCount(lines[i:]) >= 2 {
			if cut := bodyPrepSignatureCutLineWithLead(lines, i); bodyPrepCutLeavesUsablePrefix(lines, cut) {
				if bodyPrepSuffixHasPreservedReplyBody(lines[cut:]) {
					continue
				}
				return cut
			}
			continue
		}
	}
	return -1
}

func bodyPrepTailStart(lines []string) int {
	nonblank := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonblank++
		}
	}
	if nonblank == 0 {
		return 0
	}

	startOrdinal := nonblank - bodyPrepSignatureTailLines
	if startOrdinal < 0 {
		startOrdinal = 0
	}

	seen := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if seen >= startOrdinal {
			return i
		}
		seen++
	}
	return len(lines) - 1
}

func bodyPrepReplyCutLine(lines []string, i int) int {
	for j := i - 1; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeSeparatorLine(line) || bodyPrepLooksLikeReplyHeaderLine(line) {
			return j
		}
		break
	}
	return i
}

func bodyPrepFooterCutLineWithSignatureLead(lines []string, i int) int {
	cut := i
	signals := 0
	for j := i - 1; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeSignatureLine(line) {
			signals++
			cut = j
			continue
		}
		if signals > 0 && (bodyPrepLooksLikeSignatureLeadLine(line) || bodyPrepClosingRE.MatchString(line)) {
			cut = j
			continue
		}
		break
	}
	if signals >= 2 {
		return cut
	}
	return i
}

func bodyPrepSignatureCutLineWithLead(lines []string, i int) int {
	cut := i
	for j := i - 1; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeSignatureLeadLine(line) || bodyPrepLooksLikeTrailingNoiseLine(line) || bodyPrepClosingRE.MatchString(line) || bodyPrepTailNameRE.MatchString(line) || bodyPrepLooksLikeSignatureSpacerLine(line) {
			cut = j
			continue
		}
		break
	}
	return cut
}

func bodyPrepCutLeavesVisiblePrefix(lines []string, cut int) bool {
	if cut <= 0 || cut > len(lines) {
		return false
	}
	prefix := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	return visibleBodyPrepRunes(prefix) >= bodyPrepMinPrefixVisible
}

func bodyPrepCutLeavesUsablePrefix(lines []string, cut int) bool {
	if bodyPrepCutLeavesVisiblePrefix(lines, cut) {
		return true
	}
	if cut <= 0 || cut > len(lines) {
		return false
	}
	prefix := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	if bodyPrepThinForwardRE.MatchString(prefix) || bodyPrepThinShareRE.MatchString(prefix) {
		return true
	}
	if visibleBodyPrepRunes(prefix) < 12 {
		return false
	}
	for _, raw := range lines[:cut] {
		line := strings.TrimSpace(raw)
		if line == "" || bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line) {
			continue
		}
		if bodyPrepLooksLikeBusinessSentenceLine(line) {
			return true
		}
	}
	return false
}

func bodyPrepLinesVisibleEnough(lines []string) bool {
	body := strings.TrimSpace(strings.Join(lines, "\n"))
	return visibleBodyPrepRunes(body) >= bodyPrepMinPrefixVisible
}

func bodyPrepLooksLikeForwardPrefix(lines []string) bool {
	content := 0
	signals := 0
	var contentText []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || bodyPrepLooksLikeSeparatorLine(line) {
			continue
		}
		if bodyPrepLooksLikeSignatureishPrefixLine(line) {
			signals++
			continue
		}
		content++
		contentText = append(contentText, line)
	}

	contentBody := strings.Join(contentText, " ")
	if bodyPrepContentHasBusinessSentence(contentText) {
		return false
	}
	if content > 0 && content <= 3 && visibleBodyPrepRunes(contentBody) <= 120 {
		if bodyPrepThinForwardRE.MatchString(contentBody) {
			return true
		}
		if content == 1 && bodyPrepThinShareRE.MatchString(contentBody) {
			return true
		}
	}
	if content == 1 && signals >= 2 && visibleBodyPrepRunes(contentBody) <= 20 {
		return true
	}
	if content == 0 && signals >= 2 {
		return true
	}
	return false
}

func bodyPrepContentHasBusinessSentence(lines []string) bool {
	for _, line := range lines {
		if bodyPrepLooksLikeBusinessSentenceLine(line) || bodyPrepLooksLikeBusinessListLine(line) || bodyPrepLooksLikeFinancialDocumentLine(line) || bodyPrepLooksLikeMeetingDetailLine(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeSignatureishPrefixLine(line string) bool {
	if bodyPrepHTMLSignatureRE.MatchString(line) {
		return true
	}
	if bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line) || bodyPrepMobileSignatureRE.MatchString(line) {
		return true
	}
	if bodyPrepTailNameRE.MatchString(line) || bodyPrepKoreanTitleTailRE.MatchString(line) {
		return true
	}
	if bodyPrepLooksLikeTrailingNoiseLine(line) {
		return true
	}
	if utf8.RuneCountInString(line) <= 20 && bodyPrepShortNameRE.MatchString(line) {
		return true
	}
	if bodyPrepLooksLikeSignatureLine(line) || bodyPrepLooksLikeSignatureLeadLine(line) {
		return true
	}
	if utf8.RuneCountInString(line) <= 40 && bodyPrepPrefixCompanyRE.MatchString(line) {
		return true
	}
	if utf8.RuneCountInString(line) <= 80 && bodyPrepPrefixRoleRE.MatchString(line) {
		return true
	}
	if utf8.RuneCountInString(line) <= 120 && bodyPrepPrefixAddressRE.MatchString(line) {
		return true
	}
	return false
}

func bodyPrepLooksLikeBusinessListLine(line string) bool {
	return bodyPrepBusinessListLeadRE.MatchString(line)
}

func bodyPrepLooksLikeBusinessSentenceLine(line string) bool {
	return utf8.RuneCountInString(line) > 10 && bodyPrepBusinessSentenceRE.MatchString(line)
}

func bodyPrepLooksLikeFinancialDocumentLine(line string) bool {
	return bodyPrepFinancialDocRE.MatchString(line)
}

func bodyPrepLooksLikeMeetingDetailLine(line string) bool {
	return bodyPrepMeetingDetailRE.MatchString(line)
}

func bodyPrepLooksLikeSignatureLeadLine(line string) bool {
	if bodyPrepLooksLikeBusinessListLine(line) {
		return false
	}
	if utf8.RuneCountInString(line) > 60 {
		return false
	}
	return bodyPrepSignatureLeadRE.MatchString(line)
}

func bodyPrepLooksLikeSignatureSpacerLine(line string) bool {
	return line != "" && strings.Trim(line, "| \t") == ""
}

func bodyPrepForwardedBodyStart(lines []string, cut int) int {
	i := cut
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" || bodyPrepLooksLikeSeparatorLine(line) || bodyPrepLooksLikeReplyHeaderLine(line) || bodyPrepLooksLikeStrongReplyBoundaryLine(line) || bodyPrepHTMLMetaRE.MatchString(line) || bodyPrepLooksLikeAttachmentMetaLine(line) {
			i++
			continue
		}
		break
	}
	return i
}

func stripBodyPrepSignatureBeforeReplyHistory(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !bodyPrepBoundaryBeforePreservedReplyBody(lines, i) {
			continue
		}
		start := i
		signals := 0
		hasMobileSignature := false
		hasTrailingSignoff := false
		for j := i - 1; j >= 0; j-- {
			prev := strings.TrimSpace(lines[j])
			if prev == "" {
				start = j
				continue
			}
			if bodyPrepClosingRE.MatchString(prev) || bodyPrepTrailingSignoffRE.MatchString(prev) || bodyPrepLooksLikeSignatureishPrefixLine(prev) || bodyPrepLooksLikeFooterLine(prev) {
				if bodyPrepLooksLikeSignatureishPrefixLine(prev) || bodyPrepTrailingSignoffRE.MatchString(prev) {
					signals++
				}
				if bodyPrepMobileSignatureRE.MatchString(prev) {
					hasMobileSignature = true
				}
				if bodyPrepTrailingSignoffRE.MatchString(prev) {
					hasTrailingSignoff = true
				}
				start = j
				continue
			}
			break
		}
		if (signals < 2 && !hasMobileSignature && !hasTrailingSignoff) || start >= i {
			continue
		}
		out := make([]string, 0, len(lines)-(i-start))
		out = append(out, lines[:start]...)
		out = append(out, lines[i:]...)
		return compactBodyPrepBlankLines(out), bodyPrepNonBlankLineCount(lines[start:i])
	}
	return lines, 0
}

func stripBodyPrepReplyArtifactsPreservingBody(lines []string) ([]string, int) {
	if len(lines) == 0 {
		return lines, 0
	}
	out := append([]string(nil), lines...)
	removedTotal := 0
	for {
		changed := false
		for i := 0; i < len(out); i++ {
			if !bodyPrepReplyArtifactStartLine(out, i) || !bodyPrepSuffixHasPreservedReplyBody(out[i:]) {
				continue
			}
			cut := i
			for cut < len(out) {
				if strings.TrimSpace(out[cut]) == "" || bodyPrepReplyArtifactLine(out, cut) {
					cut++
					continue
				}
				break
			}
			if cut <= i || cut >= len(out) {
				continue
			}
			removedTotal += bodyPrepNonBlankLineCount(out[i:cut])
			next := make([]string, 0, len(out)-(cut-i))
			next = append(next, out[:i]...)
			next = append(next, out[cut:]...)
			out = compactBodyPrepBlankLines(next)
			changed = true
			break
		}
		if !changed {
			break
		}
	}
	if removedTotal == 0 {
		return lines, 0
	}
	return out, removedTotal
}

func bodyPrepReplyArtifactStartLine(lines []string, i int) bool {
	if i < 0 || i >= len(lines) {
		return false
	}
	line := bodyPrepUnquoteReplyLine(strings.TrimSpace(lines[i]))
	if line == "" {
		return false
	}
	if bodyPrepLooksLikeStrongReplyBoundaryLine(line) || bodyPrepLooksLikeSeparatorLine(line) {
		return true
	}
	if bodyPrepLooksLikeReplyHeaderLine(line) {
		return bodyPrepLocalReplyHeaderSignalCount(lines[i:], 8) >= 2
	}
	if bodyPrepLooksLikeAttachmentMetaLine(line) {
		return true
	}
	return false
}

func bodyPrepReplyArtifactLine(lines []string, i int) bool {
	if i < 0 || i >= len(lines) {
		return false
	}
	line := strings.TrimSpace(lines[i])
	if line == "" {
		return true
	}
	unquoted := bodyPrepUnquoteReplyLine(line)
	if bodyPrepLooksLikeSeparatorLine(unquoted) || bodyPrepLooksLikeReplyHeaderLine(unquoted) || bodyPrepLooksLikeStrongReplyBoundaryLine(unquoted) || bodyPrepHTMLMetaRE.MatchString(unquoted) || bodyPrepLooksLikeAttachmentMetaLine(unquoted) || bodyPrepLooksLikeHeadNoiseLine(unquoted) {
		return true
	}
	return false
}

func bodyPrepBoundaryBeforePreservedReplyBody(lines []string, i int) bool {
	if i < 0 || i >= len(lines) {
		return false
	}
	line := strings.TrimSpace(lines[i])
	if !(bodyPrepLooksLikeSeparatorLine(line) || bodyPrepLooksLikeReplyHeaderLine(line) || bodyPrepLooksLikeStrongReplyBoundaryLine(line)) {
		return false
	}
	return bodyPrepSuffixHasPreservedReplyBody(lines[i:])
}

func bodyPrepSuffixHasPreservedReplyBody(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	seenBoundary := false
	visible := 0
	contentLines := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeSeparatorLine(line) || bodyPrepLooksLikeReplyHeaderLine(line) || bodyPrepLooksLikeStrongReplyBoundaryLine(line) || bodyPrepHTMLMetaRE.MatchString(line) || bodyPrepLooksLikeAttachmentMetaLine(line) || bodyPrepLooksLikeHeadNoiseLine(line) {
			seenBoundary = true
			continue
		}
		if !seenBoundary {
			continue
		}
		content := bodyPrepUnquoteReplyLine(line)
		if content == "" || bodyPrepLooksLikeSeparatorLine(content) || bodyPrepLooksLikeReplyHeaderLine(content) || bodyPrepLooksLikeStrongReplyBoundaryLine(content) || bodyPrepHTMLMetaRE.MatchString(content) || bodyPrepLooksLikeAttachmentMetaLine(content) || bodyPrepLooksLikeHeadNoiseLine(content) {
			continue
		}
		if bodyPrepLooksLikeFooterLine(content) || bodyPrepLooksLikeSignatureLine(content) || bodyPrepClosingRE.MatchString(content) || bodyPrepTrailingSignoffRE.MatchString(content) {
			continue
		}
		contentLines++
		visible += visibleBodyPrepRunes(content)
		if bodyPrepLooksLikeBusinessSentenceLine(content) || bodyPrepLooksLikeBusinessListLine(content) || bodyPrepLooksLikeFinancialDocumentLine(content) || bodyPrepLooksLikeMeetingDetailLine(content) {
			return true
		}
		if contentLines >= 1 && visible >= 4 {
			return true
		}
	}
	return false
}

func bodyPrepUnquoteReplyLine(line string) string {
	line = strings.TrimSpace(line)
	for strings.HasPrefix(line, ">") {
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
	}
	return line
}
