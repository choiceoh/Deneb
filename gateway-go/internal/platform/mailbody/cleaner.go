package mailbody

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	bodyPrepMinPrefixVisible   = 30
	bodyPrepSignatureTailLines = 20
	bodyPrepHeadNoiseMaxLines  = 8
)

var (
	bodyPrepBlankLineRE = regexp.MustCompile(`\n{3,}`)
	bodyPrepContactREs  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:T|Tel|M|Mob|Mobile|HP|H\.P|F|Fax|E|Email|Mail|Web|Homepage|Addr|Address)\s*[:.)-]`),
		regexp.MustCompile(`(?i)\b(?:tel|mobile|phone|fax|e-?mail|email|www|homepage|site|address)\b`),
		regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`),
		regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`),
		regexp.MustCompile(`\b0\d{1,2}[-.\s]?\d{3,4}[-.\s]?\d{4}\b`),
		regexp.MustCompile(`(?:대표|상무|전무|이사|부장|차장|과장|대리|팀장|실장|책임|선임|연구원)`),
		regexp.MustCompile(`(?:소속|부서|직급|직책|담당|팀명|회사명)\s*[:：]`),
		regexp.MustCompile(`(?i)\b(?:manager|director|ceo|cto|cfo)\b`),
		regexp.MustCompile(`(?:주식회사|\(주\)|㈜)`),
		regexp.MustCompile(`(?i)\b(?:inc\.?|ltd\.?|corp\.?|co\.,?\s*ltd)\b`),
		regexp.MustCompile(`(?:사업자\s*(?:등록)?\s*번호|법인\s*(?:등록)?\s*번호|통신판매(?:업)?\s*(?:신고|번호)|대표전화|대표\s*번호|우편번호|주소\s*:)`),
		regexp.MustCompile(`(?:서울|경기|인천|부산|대구|광주|대전|울산|세종|강원|충북|충남|전북|전남|경북|경남|제주).{0,50}(?:로|길)\s*\d`),
	}
	bodyPrepClosingRE       = regexp.MustCompile(`(?i)^\s*(감사합니다|감사드립니다|고맙습니다|수고하세요|수고하십시오|best\s+regards|kind\s+regards|regards|sincerely|thanks|thank\s+you)[\s,.!！。]*$`)
	bodyPrepSeparatorRE     = regexp.MustCompile(`^\s*[-_=*─━]{3,}\s*$`)
	bodyPrepSignatureLeadRE = regexp.MustCompile(`(?i)(?:[가-힣]{2,4}|[A-Z][a-z]+)\s*(?:[/|·-]\s*)?.*(?:[가-힣A-Za-z0-9]+(?:팀|실|센터|본부|파트|부서|부문)|담당|group|team|dept|department)`)
	bodyPrepHeadNoiseREs    = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(외부\s*(?:발신|메일)|외부에서\s*발송|주의.{0,20}(?:외부|발신|메일)|external.{0,30}(?:sender|email|originated)|outside.{0,30}(?:organization|sender))`),
		regexp.MustCompile(`(?i)(보안\s*주의|피싱|스팸|링크를\s*클릭|첨부파일을\s*열기|caution|warning|security\s*notice)`),
	}
	bodyPrepFooterREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:본\s*(?:메일|전자우편)|이\s*(?:메일|전자우편)).{0,120}(?:기밀|비밀|수신자|무단|복사|배포|전재|삭제|법적|오발송|잘못\s*수신)`),
		regexp.MustCompile(`(?i)\b(?:confidential|privileged|intended\s+recipient|intended\s+only|intended\s+solely|not\s+the\s+intended\s+recipient|received\s+this\s+(?:email|message)\s+in\s+error|unauthori[sz]ed|disclaimer|delete\s+this\s+email|virus\s+scanned)\b`),
		regexp.MustCompile(`(?i)(?:발신전용|회신\s*불가|자동\s*발송|\bdo\s*not\s*reply\b|\bno-?reply\b|auto(?:matically)?\s*generated|automated\s+(?:message|email))`),
		regexp.MustCompile(`(?i)(?:수신\s*거부|구독\s*취소|메일\s*수신을\s*원치|광고성\s*정보|개인정보처리방침|이용약관|\bunsubscribe\b|privacy\s+policy|terms\s+of\s+use|manage\s+preferences)`),
		regexp.MustCompile(`(?i)(?:all\s+rights\s+reserved|copyright|ⓒ|©|before\s+printing|think\s+about\s+the\s+environment|환경을\s*생각|인쇄하기\s*전)`),
		regexp.MustCompile(`(?i)(?:sent\s+with|protected\s+by|scanned\s+by|virus-free|avast|ahnlab|v3|메일보안)`),
	}
	bodyPrepReplyHeaderREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*-{2,}\s*original\s+message\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*원본\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:from|sent|to|cc|subject|date)\s*:`),
		regexp.MustCompile(`^\s*(?:보낸\s*사람|보낸\s*날짜|받는\s*사람|참조|제목|날짜)\s*:`),
	}
	bodyPrepTrailingNoiseREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:cid:|\[cid|\[image|<image|\blogo\b)`),
		regexp.MustCompile(`(?i)(?:^|\s)(?:https?://|www\.)\S*(?:facebook|instagram|youtube|linkedin|twitter|x\.com|blog)\S*\s*$`),
	}
	bodyPrepTrailingSignoffRE = regexp.MustCompile(`^\s*(?:(?:[가-힣]{2,4}\s*)?(?:드림|올림|배상))[\s,.!！。]*$`)
	bodyPrepMobileSignatureRE = regexp.MustCompile(`(?i)^\s*(?:sent\s+from\s+my|sent\s+from\s+outlook\s+for|나의\s+.+에서\s+보냄|iPhone에서\s+보냄|Galaxy에서\s+보냄|Android에서\s+보냄).*$`)
)

type CleanResult struct {
	Body         string
	HiddenBlocks []HiddenBlock
	RawRunes     int
	CleanRunes   int
}

type HiddenBlock struct {
	Kind  string
	Lines int
}

// CleanForAnalysis removes trailing signature/contact blocks from the LLM input
// only. It does not extract facts, summarize, or mutate the stored mail body.
func CleanForAnalysis(body string) string {
	return CleanForDisplay(body).Body
}

// CleanForDisplay builds the default human-readable body shown in the native
// mail UI. The original body should still be kept by callers for "raw/original"
// viewing; this only returns a cleaner default reading surface.
func CleanForDisplay(body string) CleanResult {
	trimmed := strings.TrimSpace(body)
	result := CleanResult{RawRunes: visibleBodyPrepRunes(trimmed)}
	if trimmed == "" {
		return result
	}
	lines := splitBodyPrepLines(trimmed)
	var removed int
	lines, removed = stripBodyPrepHeadNoise(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "header", Lines: removed})
	}
	lines, removed = stripBodyPrepTrailingNoiseLines(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "tail", Lines: removed})
	}
	lines = compactBodyPrepBlankLines(lines)
	cleaned := strings.Join(lines, "\n")

	cut := bodyPrepTailNoiseCutLine(lines)
	cutKind := "boilerplate"
	if cut < 0 {
		cut = bodyPrepSignatureCutLine(lines)
		cutKind = "signature"
	}
	if cut < 0 {
		result.Body = normalizeBodyPrep(cleaned)
		result.CleanRunes = visibleBodyPrepRunes(result.Body)
		return result
	}
	prefix := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	if visibleBodyPrepRunes(prefix) < bodyPrepMinPrefixVisible {
		result.Body = normalizeBodyPrep(cleaned)
		result.CleanRunes = visibleBodyPrepRunes(result.Body)
		return result
	}
	result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: cutKind, Lines: bodyPrepNonBlankLineCount(lines[cut:])})
	result.Body = normalizeBodyPrep(prefix)
	result.CleanRunes = visibleBodyPrepRunes(result.Body)
	return result
}

func splitBodyPrepLines(s string) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		lines = append(lines, strings.TrimRight(line, " \t"))
	}
	return lines
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
		if line == "" || bodyPrepSeparatorRE.MatchString(line) || bodyPrepLooksLikeHeadNoiseLine(line) {
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
	prefix := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	if visibleBodyPrepRunes(prefix) < bodyPrepMinPrefixVisible {
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

func bodyPrepTailNoiseCutLine(lines []string) int {
	if len(lines) == 0 {
		return -1
	}
	for i := bodyPrepTailStart(lines); i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeFooterLine(line) {
			if cut := bodyPrepFooterCutLineWithSignatureLead(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
				return cut
			}
			continue
		}
		if bodyPrepSeparatorRE.MatchString(line) && bodyPrepSuffixHasBoilerplateSignal(lines[i+1:]) {
			if bodyPrepCutLeavesVisiblePrefix(lines, i) {
				return i
			}
			continue
		}
		if bodyPrepLooksLikeReplyHeaderLine(line) && bodyPrepSuffixReplyHeaderSignalCount(lines[i:]) >= 2 {
			if cut := bodyPrepReplyCutLine(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
				return cut
			}
			continue
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
		if bodyPrepClosingRE.MatchString(line) && bodyPrepSuffixHasSignatureSignal(lines[i+1:]) {
			if bodyPrepCutLeavesVisiblePrefix(lines, i) {
				return i
			}
			continue
		}
		if bodyPrepLooksLikeSignatureLine(line) && bodyPrepSuffixSignatureSignalCount(lines[i:]) >= 2 {
			if cut := bodyPrepSignatureCutLineWithLead(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
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
		if bodyPrepSeparatorRE.MatchString(line) || bodyPrepLooksLikeReplyHeaderLine(line) {
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
	for j := i - 1; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if line == "" {
			continue
		}
		if bodyPrepLooksLikeSignatureLeadLine(line) || bodyPrepClosingRE.MatchString(line) {
			return j
		}
		break
	}
	return i
}

func bodyPrepCutLeavesVisiblePrefix(lines []string, cut int) bool {
	if cut <= 0 || cut > len(lines) {
		return false
	}
	prefix := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	return visibleBodyPrepRunes(prefix) >= bodyPrepMinPrefixVisible
}

func bodyPrepSuffixHasSignatureSignal(lines []string) bool {
	return bodyPrepSuffixSignatureSignalCount(lines) > 0
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
		if bodyPrepLooksLikeSignatureLine(line) {
			count++
		}
	}
	return count
}

func bodyPrepSuffixReplyHeaderSignalCount(lines []string) int {
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

func bodyPrepLooksLikeReplyHeaderLine(line string) bool {
	for _, re := range bodyPrepReplyHeaderREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeTrailingNoiseLine(line string) bool {
	if bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line) || bodyPrepMobileSignatureRE.MatchString(line) {
		return true
	}
	for _, re := range bodyPrepTrailingNoiseREs {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func bodyPrepLooksLikeSignatureLeadLine(line string) bool {
	if utf8.RuneCountInString(line) > 60 {
		return false
	}
	return bodyPrepSignatureLeadRE.MatchString(line)
}

func bodyPrepLooksLikeSignatureLine(line string) bool {
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
