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
	bodyPrepSignatureTailLines = 30
	bodyPrepHeadNoiseMaxLines  = 8
)

var (
	bodyPrepBlankLineRE = regexp.MustCompile(`\n{3,}`)
	bodyPrepContactREs  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:T|Tel|M|Mob|Mobile|HP|H\.P|F|Fax|E|Email|Mail|Web|Homepage|Addr|Address|Factory)\s*[:.)_-]`),
		regexp.MustCompile(`(?i)\b(?:tel|mobile|phone|fax|e-?mail|email|www|homepage|site|address)\b`),
		regexp.MustCompile(`(?:전화|연락처|휴대폰|모바일|웹사이트|홈페이지)\s*(?:[:：(]|$)`),
		regexp.MustCompile(`(?i)^\s*(?:web|website|homepage|site|url)\s*[:：]|\bwww\.[A-Z0-9.\-]+\.[A-Z]{2,}\b`),
		regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`),
		regexp.MustCompile(`\b0\d{1,2}[-.\s]?\d{3,4}[-.\s]?\d{4}\b`),
		regexp.MustCompile(`(?:대표|상무|전무|이사|부장|차장|과장|대리|주임|팀장|실장|책임|선임|연구원)`),
		regexp.MustCompile(`(?:소속|부서|직급|직책|담당|팀명|회사명)\s*[:：]`),
		regexp.MustCompile(`(?i)\b(?:manager|director|ceo|cto|cfo|specialist|engineer|coordinator|assistant|clerk|sales|logistics)\b`),
		regexp.MustCompile(`(?i)\b(?:business|international|marine|industry|project|execution)\s+(?:division|group|department|team)\b`),
		regexp.MustCompile(`(?:주식회사|\(주\)|\(유\)|㈜)`),
		regexp.MustCompile(`(?i)\b(?:inc\.?|ltd\.?|corp\.?|co\.,?\s*ltd)\b`),
		regexp.MustCompile(`(?:사업자\s*(?:등록)?\s*번호|법인\s*(?:등록)?\s*번호|통신판매(?:업)?\s*(?:신고|번호)|대표전화|대표\s*번호|우편번호|주소\s*:)`),
		regexp.MustCompile(`(?:서울|경기|인천|부산|대구|광주|대전|울산|세종|강원|충북|충남|전북|전남|경북|경남|제주).{0,50}(?:로|길)\s*\d`),
	}
	bodyPrepClosingRE       = regexp.MustCompile(`(?i)^\s*(감사합니다|감사드립니다|고맙습니다|수고하세요|수고하십시오|best|best\s+regards(?:\s*&\s*thanks\s*so\s*much)?|kind\s+regards|regards|sincerely|yours\s+sincerely|yours\s+faithfully|thanks|thank\s+you)[\s,.!！。]*$`)
	bodyPrepSeparatorRE     = regexp.MustCompile(`^\s*(?:[-_=*─━]{3,}|[-_=*─━\s]+아\s*래\s*[-_=*─━\s]+)\s*$`)
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
		regexp.MustCompile(`(?i)(?:if\s+you\s+have\s+any\s+questions,?\s+visit\s+our\s+support\s+site|something\s+wrong\s+with\s+the\s+email\?|view\s+in\s+browser|you'?re\s+receiving\s+this\s+email\s+because\s+you\s+made\s+a\s+purchase|partners\s+with\s+stripe)`),
		regexp.MustCompile(`(?i)(?:share\s+feedback\s+on|manage\s+notification\s+settings|you\s+are\s+receiving\s+this\s+because|unsubscribe\s+from\s+this\s+thread)`),
	}
	bodyPrepReplyHeaderREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*-{2,}\s*original\s+message\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*-{2,}\s*forwarded\s+message\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*원본\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*전달된\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:邮件原件|原始邮件|转发邮件)\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:from|sent|to|cc|subject|date)\s*:`),
		regexp.MustCompile(`^\s*(?:보낸\s*사람|보내는\s*사람|발신자|보낸\s*날짜|보낸\s*일시|받는\s*사람|받은\s*사람|수신자|참조|제목|날짜)\s*[:：]`),
		regexp.MustCompile(`^\s*(?:发件人|寄件者|发送时间|发送日期|收件人|抄送|主题|日期)\s*[:：]`),
	}
	bodyPrepInlineReplyHeaderRE    = regexp.MustCompile(`(?i)\s+(?:date|from|to|cc|subject)\s*:\s+`)
	bodyPrepStrongReplyBoundaryREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*-{2,}\s*(?:original|forwarded)\s+(?:message|mail)\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:원본|전달된)\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:邮件原件|原始邮件|转发邮件)\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*-{2,}\s*original\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*on\s+.{3,240}\bwrote\s*:\s*$`),
		regexp.MustCompile(`^\s*.{3,240}<[^>]+@[^>]+>.*(?:작성|씀|写道)\s*:\s*$`),
		regexp.MustCompile(`^\s*\d{4}년\s*\d{1,2}월\s*\d{1,2}일.{0,180}(?:작성|씀)\s*:\s*$`),
	}
	bodyPrepTrailingNoiseREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:cid:|\[cid|\[image|<image|\blogo\b)`),
		regexp.MustCompile(`(?i)(?:^|\s)(?:https?://|www\.)\S*(?:facebook|instagram|youtube|linkedin|twitter|x\.com|blog)\S*\s*$`),
	}
	bodyPrepTrailingSignoffRE = regexp.MustCompile(`^\s*(?:(?:[가-힣](?:\s*[가-힣]){1,3}|[가-힣]{2,4})\s*)?(?:드림|올림|배상)[\s,.!！。]*$`)
	bodyPrepMobileSignatureRE = regexp.MustCompile(`(?i)^\s*(?:sent\s+from\s+my|sent\s+from\s+outlook\s+for|나의\s+.+에서\s+보냄|iPhone에서\s+보냄|Galaxy에서\s+보냄|Android에서\s+보냄|.{0,40}iPhone)\s*$`)
	bodyPrepShortNameRE       = regexp.MustCompile(`^\s*(?:[가-힣]\s*[가-힣]{1,3}|[A-Z][a-z]+(?:\s+[A-Z][a-z]+){0,2})\s*$`)
	bodyPrepTailNameRE        = regexp.MustCompile(`^\s*(?:[가-힣](?:\s*[가-힣]){1,3}|[가-힣]{2,4}\s*/\s*[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+){0,2}|[가-힣]{2,4}\s+[A-Z]{2,}(?:\s+[A-Z]{2,}){1,3}|[A-Z][a-z]+(?:\s+[A-Z][a-z]+){1,2})\s*$`)
	bodyPrepHTMLSeparatorRE   = regexp.MustCompile(`(?i)^\s*<hr\b[^>]*>\s*$`)
	bodyPrepHTMLBlankRE       = regexp.MustCompile(`(?i)^\s*(?:<o:p>\s*</o:p>|<o:p>\s*(?:&nbsp;|\s)*\s*</o:p>|<br\s*/?>)\s*$`)
	bodyPrepHTMLMetaRE        = regexp.MustCompile(`(?i)^\s*<meta\b[^>]*>\s*$`)
	bodyPrepHTMLWrapperRE     = regexp.MustCompile(`(?i)^\s*</?(?:mailplughtml|html|head|body)\b[^>]*>\s*$`)
	bodyPrepHTMLSignatureRE   = regexp.MustCompile(`(?i)<span\b[^>]*\bshowField\(`)
	bodyPrepThinForwardRE     = regexp.MustCompile(`(?i)(?:전달|아래|하기|원문|메일|below|forward|fyi|see\s+below)`)
	bodyPrepThinShareRE       = regexp.MustCompile(`(?i)(?:참조|참고|송부|공유|자료|attached)`)
	bodyPrepAttachmentLeadRE  = regexp.MustCompile(`(?i)^\s*(?:대용량\s*)?(?:파일\s*첨부|첨부\s*파일|첨부)(?:\s*총)?\s*\(?\s*\d+\s*개\s*\)?(?:\s*\(?[0-9][0-9.,]*\s*(?:b|kb|mb|gb|tb)\)?)?(?:.*다운로드\s*기간\s*[:：].*)?\s*$`)
	bodyPrepAttachmentHeadRE  = regexp.MustCompile(`(?i)^\s*(?:대용량\s*)?(?:파일\s*첨부|첨부\s*파일|첨부파일|첨부)(?:\s|$|\()`)
	bodyPrepAttachmentBodyRE  = regexp.MustCompile(`(?i)(?:[가-힣A-Za-z0-9()/·\s]{1,30}님\s+안녕하(?:세요|십니까)|안녕하(?:세요|십니까)|업무에\s+고생|수신\s*[:：]|발신\s*[:：]|\bDear\s+[A-Za-z가-힣]|\bHi[,\s]+[A-Za-z가-힣]|\bHello[,\s]+[A-Za-z가-힣])`)
	bodyPrepAttachmentMetaREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*\([0-9][0-9.,]*\s*(?:b|kb|mb|gb|tb)\)\s*$`),
		regexp.MustCompile(`^\s*다운로드\s*기간\s*[:：]`),
		regexp.MustCompile(`^\s*~\s*\d{4}[/-]\d{1,2}[/-]\d{1,2}\s*$`),
		regexp.MustCompile(`^\s*기한이\s*있는\s*파일은\s*\d+\s*일\s*보관`),
		regexp.MustCompile(`^\s*\((?:대용량\s*)?첨부\s*파일은\s*\d+\s*일간\s*보관\)\s*$`),
		regexp.MustCompile(`(?i)\.(?:zip|7z|rar|pdf|xlsx?|docx?|pptx?|hwp|hwpx|dwg|dxf|jpg|jpeg|png|gif|heic|csv|txt|eml|msg)(?:\s*$|\s*\(|\s*-|\s+[0-9]|<)`),
	}
	bodyPrepBusinessListLeadRE = regexp.MustCompile(`^\s*(?:[-*•]\s*|[0-9]{1,2}[.)]\s+|[가-하][.)]\s+)`)
	bodyPrepBusinessSentenceRE = regexp.MustCompile(`(?:입니다|있습니다|없습니다|했습니다|하였습니다|드립니다|부탁|요청|확인|검토|진행|회신|공유|참고|첨부|발생|필요|상황|의견|문의|제공|협의|일정|납부|고지서|미납|입금|계좌|세금계산서|발행|계약서|회계|비용|처리|준비|등록|접수|현장|공사|금액|대납|임대인|한전)`)
	bodyPrepFinancialDocRE     = regexp.MustCompile(`(?i)(?:invoice|receipt|refund|refunded|credit\s+note|amount\s+paid|total\s+credit|subtotal|\bVAT\b|payment|issued|card|american\s+express|[$€£₩]\s*\d)`)
	bodyPrepReceiptVendorRE    = regexp.MustCompile(`(?i)^\s*anthropic,\s*pbc\b(?:\s*[\(<].*)?$`)
	bodyPrepReceiptSupportRE   = regexp.MustCompile(`(?i)\s+questions\?\s+.*$`)
	bodyPrepReceiptInlineREs   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\s*\(?invoice\s+illustration\s*(?:\[[^\]]*\]|\([^)]*\)|<[^>]*>)?`),
		regexp.MustCompile(`(?i)\s*download\s+(?:invoice|receipt|credit\s+note)\s*(?:\([^)]*\)|<[^>]*>)?`),
		regexp.MustCompile(`(?i)\s*view\s+(?:updated\s+)?(?:invoice|receipt|credit\s+note)\s*(?:\([^)]*\)|<[^>]*>|\S+)?`),
	}
	bodyPrepInlineFooterLeadRE       = regexp.MustCompile(`(?i)\s+(?:상기\s*메일은|본\s*(?:메일|전자우편)은|this\s+(?:message|email)\s+is\s+confidential|this\s+(?:message|email).{0,80}intended\s+only)`)
	bodyPrepInlineClosingSignatureRE = regexp.MustCompile(`(?:감사합니다|감사드립니다|고맙습니다)[\s,.!！。]*(?:[가-힣]\s*){2,4}\s+[A-Z][A-Za-z]`)
	bodyPrepInlineEnglishSignatureRE = regexp.MustCompile(`(?i)(?:best|kind)\s+regards[\s,.!！。]+[A-Z][a-z]+(?:\s+[A-Z][a-z]+){0,2}\s+(?:manager|director|specialist|engineer|clerk|team|department|division)\b`)
	bodyPrepPrefixCompanyRE          = regexp.MustCompile(`(?i)(?:co\.,?\s*ltd|company|energy|주식회사|\(주\)|\(유\)|㈜|[가-힣]{2,4}\s*(?:이사|부장|차장|과장|대리|주임|책임|선임))`)
	bodyPrepPrefixRoleRE             = regexp.MustCompile(`(?i)(?:manager|director|clerk|division|group|team|dept|department|senior|junior|overseas|sales|project|execution)`)
	bodyPrepPrefixAddressRE          = regexp.MustCompile(`(?i)(?:korea|china|gwangju|seoul|buk-gu|district|road|ro\b|gil\b|beon-gil)`)
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
	lines, removed = stripBodyPrepHeadReceiptVendorBlock(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "header", Lines: removed})
	}
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
	lines, removed = stripBodyPrepTrailingNoiseLines(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "tail", Lines: removed})
	}
	lines = compactBodyPrepBlankLines(lines)

	for pass := 0; pass < 3; pass++ {
		cut := bodyPrepReplyHistoryCutLine(lines)
		if cut < 0 || !bodyPrepCutLeavesVisiblePrefix(lines, cut) {
			break
		}
		if bodyPrepLooksLikeForwardPrefix(lines[:cut]) {
			if start := bodyPrepForwardedBodyStart(lines, cut); start > cut && start < len(lines) && bodyPrepLinesVisibleEnough(lines[start:]) {
				result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history-header", Lines: bodyPrepNonBlankLineCount(lines[:start])})
				lines = compactBodyPrepBlankLines(lines[start:])
				lines = stripBodyPrepForwardedHeadArtifacts(lines, &result)
				continue
			} else {
				result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
				lines = compactBodyPrepBlankLines(lines[:cut])
			}
		} else {
			result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
			lines = compactBodyPrepBlankLines(lines[:cut])
		}
		break
	}

	lines, removed = stripBodyPrepTrailingNoiseLines(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "tail", Lines: removed})
	}

	lines, removed = stripBodyPrepInlineTailNoise(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "boilerplate", Lines: removed})
	}

	cut := bodyPrepTailNoiseCutLine(lines)
	if cut >= 0 && bodyPrepCutLeavesVisiblePrefix(lines, cut) {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "boilerplate", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
		lines = compactBodyPrepBlankLines(lines[:cut])
	}

	cut = bodyPrepSignatureCutLine(lines)
	if cut >= 0 && bodyPrepCutLeavesUsablePrefix(lines, cut) {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "signature", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
		lines = compactBodyPrepBlankLines(lines[:cut])
	}

	lines, removed = stripBodyPrepDecorativeSeparatorLines(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "separator", Lines: removed})
	}

	result.Body = normalizeBodyPrep(strings.Join(lines, "\n"))
	result.CleanRunes = visibleBodyPrepRunes(result.Body)
	return result
}

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
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = normalizeBodyPrepLine(line)
		lines = append(lines, splitBodyPrepInlineReplyHeaders(line)...)
	}
	return lines
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
	if bodyPrepLooksLikeFinancialDocumentLine(line) {
		line = cleanBodyPrepFinancialLine(line)
	}
	line = strings.TrimRight(line, " \t\u200b\u200c\u200d\ufeff")
	if strings.Trim(strings.TrimSpace(line), "\u200b\u200c\u200d\ufeff") == "" || bodyPrepHTMLBlankRE.MatchString(line) || bodyPrepHTMLWrapperRE.MatchString(line) {
		return ""
	}
	return line
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
		if bodyPrepLooksLikeFinancialDocumentLine(line) {
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
			if bodyPrepCutLeavesUsablePrefix(lines, i) {
				return i
			}
			continue
		}
		if bodyPrepSuffixStartsBusinessBody(lines[i+1:]) {
			continue
		}
		if bodyPrepLooksLikeSignatureLine(line) && bodyPrepSuffixSignatureSignalCount(lines[i:]) >= 2 {
			if cut := bodyPrepSignatureCutLineWithLead(lines, i); bodyPrepCutLeavesUsablePrefix(lines, cut) {
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
		if bodyPrepLooksLikeSignatureLeadLine(line) || bodyPrepClosingRE.MatchString(line) || bodyPrepTailNameRE.MatchString(line) || bodyPrepLooksLikeSignatureSpacerLine(line) {
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

func bodyPrepLooksLikeSignatureishPrefixLine(line string) bool {
	if bodyPrepHTMLSignatureRE.MatchString(line) {
		return true
	}
	if bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line) || bodyPrepMobileSignatureRE.MatchString(line) {
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
		if bodyPrepLooksLikeBusinessListLine(line) || bodyPrepLooksLikeBusinessSentenceLine(line) || bodyPrepLooksLikeFinancialDocumentLine(line) {
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
	if bodyPrepClosingRE.MatchString(line) || bodyPrepTrailingSignoffRE.MatchString(line) || bodyPrepMobileSignatureRE.MatchString(line) || bodyPrepTailNameRE.MatchString(line) {
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
