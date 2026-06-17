package mailbody

import (
	"html"
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
		regexp.MustCompile(`(?i)^\s*(?:web|website|homepage|site|url)\s*[:：]|\bwww\.[A-Z0-9.\-]+\.[A-Z]{2,}\b`),
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
	bodyPrepSignatureLeadRE = regexp.MustCompile(`(?i)(?:[가-힣]{2,4}|[A-Z][a-z]+)\s*(?:[/|·-]\s*)?.*(?:[가-힣A-Za-z0-9]+(?:팀|실|센터|본부|파트|부서|부문)|담당|\b(?:group|team|dept|department)\b)`)
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
		regexp.MustCompile(`(?i)(?:feedback\s*&\s*support|we'?re\s+here\s+to\s+help|start\s+building)`),
	}
	bodyPrepReplyHeaderREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*-{2,}\s*original\s+message\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*-{2,}\s*forwarded\s+message\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*원본\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*전달된\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:邮件原件|原始邮件|转发邮件)\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:from|sent|to|cc|subject|date)\s*:`),
		regexp.MustCompile(`^\s*(?:보낸\s*사람|보내는\s*사람|발신자|보낸\s*날짜|보낸\s*일시|받는\s*사람|수신자|참조|제목|날짜)\s*[:：]`),
		regexp.MustCompile(`^\s*(?:发件人|寄件者|发送时间|发送日期|收件人|抄送|主题|日期)\s*[:：]`),
	}
	bodyPrepStrongReplyBoundaryREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*-{2,}\s*(?:original|forwarded)\s+(?:message|mail)\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:원본|전달된)\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:邮件原件|原始邮件|转发邮件)\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*on\s+.{3,240}\bwrote\s*:\s*$`),
		regexp.MustCompile(`^\s*.{3,240}<[^>]+@[^>]+>.*(?:작성|씀|写道)\s*:\s*$`),
		regexp.MustCompile(`^\s*\d{4}년\s*\d{1,2}월\s*\d{1,2}일.{0,180}(?:작성|씀)\s*:\s*$`),
	}
	bodyPrepTrailingNoiseREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:cid:|\[cid|\[image|<image|\blogo\b)`),
		regexp.MustCompile(`(?i)(?:^|\s)(?:https?://|www\.)\S*(?:facebook|instagram|youtube|linkedin|twitter|x\.com|blog)\S*\s*$`),
	}
	bodyPrepTrailingSignoffRE = regexp.MustCompile(`^\s*(?:(?:[가-힣]{2,4}\s*)?(?:드림|올림|배상))[\s,.!！。]*$`)
	bodyPrepMobileSignatureRE = regexp.MustCompile(`(?i)^\s*(?:sent\s+from\s+my|sent\s+from\s+outlook\s+for|나의\s+.+에서\s+보냄|iPhone에서\s+보냄|Galaxy에서\s+보냄|Android에서\s+보냄).*$`)
	bodyPrepHTMLSeparatorRE   = regexp.MustCompile(`(?i)^\s*<hr\b[^>]*>\s*$`)
	bodyPrepHTMLBlankRE       = regexp.MustCompile(`(?i)^\s*(?:<o:p>\s*</o:p>|<o:p>\s*(?:&nbsp;|\s)*\s*</o:p>|<br\s*/?>)\s*$`)
	bodyPrepHTMLMetaRE        = regexp.MustCompile(`(?i)^\s*<meta\b[^>]*>\s*$`)
	bodyPrepThinForwardRE     = regexp.MustCompile(`(?i)(?:전달|참조|참고|송부|공유|자료|아래|below|forward|fyi|attached|see\s+below)`)
	bodyPrepAttachmentLeadRE  = regexp.MustCompile(`(?i)^\s*(?:대용량\s*)?(?:파일\s*첨부|첨부\s*파일)\s*\d+\s*개\s*$`)
	bodyPrepAttachmentMetaREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*\([0-9][0-9.,]*\s*(?:b|kb|mb|gb|tb)\)\s*$`),
		regexp.MustCompile(`^\s*다운로드\s*기간\s*[:：]`),
		regexp.MustCompile(`^\s*\((?:대용량\s*)?첨부\s*파일은\s*\d+\s*일간\s*보관\)\s*$`),
		regexp.MustCompile(`(?i)\.(?:zip|7z|rar|pdf|xlsx?|docx?|pptx?|hwp|hwpx|dwg|dxf|jpg|jpeg|png|gif|heic|csv|txt|eml|msg)(?:\s*$|\s*\(|\s*-)`),
	}
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
	lines, removed = stripBodyPrepHeadReplyHeaderBlock(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history-header", Lines: removed})
	}
	lines, removed = stripBodyPrepHeadAttachmentBlock(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "attachment", Lines: removed})
	}
	lines, removed = stripBodyPrepHeadReplyHeaderBlock(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history-header", Lines: removed})
	}
	lines, removed = stripBodyPrepTrailingNoiseLines(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "tail", Lines: removed})
	}
	lines = compactBodyPrepBlankLines(lines)

	cut := bodyPrepReplyHistoryCutLine(lines)
	if cut >= 0 && bodyPrepCutLeavesVisiblePrefix(lines, cut) {
		if bodyPrepLooksLikeThinForwardWrapper(lines[:cut]) {
			if start := bodyPrepForwardedBodyStart(lines, cut); start > cut && start < len(lines) && bodyPrepLinesVisibleEnough(lines[start:]) {
				result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history-header", Lines: bodyPrepNonBlankLineCount(lines[:start])})
				lines = compactBodyPrepBlankLines(lines[start:])
			} else {
				result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
				lines = compactBodyPrepBlankLines(lines[:cut])
			}
		} else {
			result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
			lines = compactBodyPrepBlankLines(lines[:cut])
		}
	}

	cut = bodyPrepTailNoiseCutLine(lines)
	if cut >= 0 && bodyPrepCutLeavesVisiblePrefix(lines, cut) {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "boilerplate", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
		lines = compactBodyPrepBlankLines(lines[:cut])
	}

	cut = bodyPrepSignatureCutLine(lines)
	if cut >= 0 && bodyPrepCutLeavesVisiblePrefix(lines, cut) {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "signature", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
		lines = compactBodyPrepBlankLines(lines[:cut])
	}

	result.Body = normalizeBodyPrep(strings.Join(lines, "\n"))
	result.CleanRunes = visibleBodyPrepRunes(result.Body)
	return result
}

func splitBodyPrepLines(s string) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		lines = append(lines, normalizeBodyPrepLine(line))
	}
	return lines
}

func normalizeBodyPrepLine(line string) string {
	line = html.UnescapeString(line)
	line = strings.ReplaceAll(line, "\u00a0", " ")
	line = strings.TrimRight(line, " \t\u200b\u200c\u200d\ufeff")
	if strings.Trim(strings.TrimSpace(line), "\u200b\u200c\u200d\ufeff") == "" || bodyPrepHTMLBlankRE.MatchString(line) {
		return ""
	}
	return line
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
		return lines, 0
	}
	return lines[cut:], bodyPrepNonBlankLineCount(lines[:cut])
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
		if bodyPrepLooksLikeSeparatorLine(line) && bodyPrepSuffixHasBoilerplateSignal(lines[i+1:]) {
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
			if cut := bodyPrepReplyCutLine(lines, i); bodyPrepCutLeavesVisiblePrefix(lines, cut) {
				return cut
			}
			continue
		}
		if !bodyPrepLooksLikeReplyHeaderLine(line) {
			continue
		}
		if bodyPrepLocalReplyHeaderSignalCount(lines[i:], 8) >= 2 {
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
		if (bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line)) && bodyPrepSuffixStartsSignatureBlock(lines[i+1:]) {
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

func bodyPrepLinesVisibleEnough(lines []string) bool {
	body := strings.TrimSpace(strings.Join(lines, "\n"))
	return visibleBodyPrepRunes(body) >= bodyPrepMinPrefixVisible
}

func bodyPrepLooksLikeThinForwardWrapper(lines []string) bool {
	nonblank := 0
	var text []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || bodyPrepLooksLikeSeparatorLine(line) {
			continue
		}
		nonblank++
		text = append(text, line)
	}
	if nonblank == 0 || nonblank > 3 {
		return false
	}
	return bodyPrepThinForwardRE.MatchString(strings.Join(text, " "))
}

func bodyPrepForwardedBodyStart(lines []string, cut int) int {
	i := cut
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" || bodyPrepLooksLikeSeparatorLine(line) || bodyPrepLooksLikeReplyHeaderLine(line) || bodyPrepHTMLMetaRE.MatchString(line) {
			i++
			continue
		}
		break
	}
	return i
}

func bodyPrepSuffixStartsSignatureBlock(lines []string) bool {
	seen := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		seen++
		if bodyPrepLooksLikeSignatureLine(line) || bodyPrepLooksLikeSignatureLeadLine(line) || bodyPrepMobileSignatureRE.MatchString(line) {
			return true
		}
		if seen >= 3 {
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
