package mailbody

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Suffix/prefix signal analysis and single-line classifiers of the mail-body
// cleaner — split from cleaner.go (pure move).

func bodyPrepSuffixStartsSignatureBlock(lines []string) bool {
	seen := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeBusinessListLine(line) || bodyPrepLooksLikeBusinessSentenceLine(line) || bodyPrepLooksLikeFinancialDocumentLine(line) {
			return false
		}
		if bodyPrepLooksLikeMeetingDetailLine(line) {
			return false
		}
		seen++
		if bodyPrepLooksLikeSignatureLine(line) || bodyPrepLooksLikeSignatureLeadLine(line) || bodyPrepTrailingSignoffRE.MatchString(line) || bodyPrepTailNameRE.MatchString(line) || bodyPrepMobileSignatureRE.MatchString(line) {
			return true
		}
		if seen >= 3 {
			return false
		}
	}
	return false
}

func bodyPrepSuffixStartsBusinessBody(lines []string) bool {
	seen := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		seen++
		if bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line) {
			return false
		}
		if bodyPrepLooksLikeBusinessListLine(line) || bodyPrepLooksLikeBusinessSentenceLine(line) || bodyPrepLooksLikeFinancialDocumentLine(line) {
			return true
		}
		if seen >= 3 {
			return false
		}
	}
	return false
}

func bodyPrepSuffixHasMeetingDetail(lines []string, maxLines int) bool {
	seen := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeMeetingDetailLine(line) {
			return true
		}
		seen++
		if maxLines > 0 && seen >= maxLines {
			return false
		}
	}
	return false
}

func bodyPrepSuffixHasBoilerplateSignal(lines []string) bool {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeFooterLine(line) || bodyPrepLooksLikeSignatureLine(line) || bodyPrepLooksLikeReplyHeaderLine(line) {
			return true
		}
	}
	return false
}

func bodyPrepPrefixHasFinancialDocument(lines []string, maxLines int) bool {
	limit := len(lines)
	if maxLines > 0 && limit > maxLines {
		limit = maxLines
	}
	for i := 0; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeFinancialDocumentLine(line) {
			return true
		}
	}
	return false
}

func bodyPrepSuffixSignatureSignalCount(lines []string) int {
	count := 0
	limit := len(lines)
	if limit > bodyPrepSignatureTailLines {
		limit = bodyPrepSignatureTailLines
	}
	for i := 0; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeSignatureLine(line) || bodyPrepLooksLikeTrailingNoiseLine(line) {
			count++
		}
	}
	return count
}

func bodyPrepSuffixReplyHeaderSignalCount(lines []string) int {
	return bodyPrepLocalReplyHeaderSignalCount(lines, bodyPrepSignatureTailLines)
}

func bodyPrepLocalReplyHeaderSignalCount(lines []string, maxLines int) int {
	count := 0
	limit := len(lines)
	if maxLines > 0 && limit > maxLines {
		limit = maxLines
	}
	for i := 0; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeReplyHeaderLine(line) {
			count++
		}
	}
	return count
}

func bodyPrepLooksLikeHeadNoiseLine(line string) bool {
	for _, re := range bodyPrepHeadNoiseREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeFooterLine(line string) bool {
	for _, re := range bodyPrepFooterREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeSeparatorLine(line string) bool {
	return bodyPrepSeparatorRE.MatchString(line) || bodyPrepHTMLSeparatorRE.MatchString(line)
}

func bodyPrepLooksLikeAttachmentMetaLine(line string) bool {
	if bodyPrepAttachmentLeadRE.MatchString(line) {
		return true
	}
	if bodyPrepBareAttachmentNameRE.MatchString(line) {
		return true
	}
	for _, re := range bodyPrepAttachmentMetaREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeReplyHeaderLine(line string) bool {
	for _, re := range bodyPrepReplyHeaderREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeStrongReplyBoundaryLine(line string) bool {
	for _, re := range bodyPrepStrongReplyBoundaryREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeTrailingNoiseLine(line string) bool {
	if bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line) || bodyPrepMobileSignatureRE.MatchString(line) || bodyPrepTailNameRE.MatchString(line) || bodyPrepKoreanTitleTailRE.MatchString(line) {
		return true
	}
	for _, re := range bodyPrepTrailingNoiseREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeSignatureLine(line string) bool {
	if bodyPrepLooksLikeBusinessListLine(line) {
		return false
	}
	if bodyPrepLooksLikeBusinessSentenceLine(line) {
		return false
	}
	if bodyPrepHTMLSignatureRE.MatchString(line) {
		return true
	}
	if bodyPrepKoreanTitleTailRE.MatchString(line) {
		return true
	}
	if utf8.RuneCountInString(line) > 90 {
		return false
	}
	for _, re := range bodyPrepContactREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func normalizeBodyPrep(body string) string {
	body = strings.TrimSpace(body)
	body = bodyPrepBlankLineRE.ReplaceAllString(body, "\n\n")
	return body
}

func visibleBodyPrepRunes(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

func bodyPrepNonBlankLineCount(lines []string) int {
	n := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
