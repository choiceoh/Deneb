package mailbody

import (
	"regexp"
	"strings"
)

const (
	bodyPrepMinPrefixVisible   = 30
	bodyPrepSignatureTailLines = 80
	bodyPrepHeadNoiseMaxLines  = 8
)

var (
	bodyPrepBlankLineRE = regexp.MustCompile(`\n{3,}`)
	bodyPrepContactREs  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:T|Tel|M|Mob|Mobile|HP|H\.P|F|Fax|E|Email|Mail|Web|Homepage|Addr|Address|Factory)\s*(?:[:.)_-]|\s+)`),
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
		regexp.MustCompile(`(?i)(?:본\s*e-?mail|this\s+e-?mail).{0,160}(?:첨부|attachment|수신|recipient|intended|solely|confidential)`),
		regexp.MustCompile(`(?i)\b(?:confidential|privileged|intended\s+recipient|intended\s+only|intended\s+solely|not\s+the\s+intended\s+recipient|received\s+this\s+(?:email|message)\s+in\s+error|unauthori[sz]ed|disclaimer|delete\s+this\s+email|virus\s+scanned)\b`),
		regexp.MustCompile(`(?i)(?:발신전용|회신\s*불가|자동\s*발송|\bdo\s*not\s*reply\b|\bno-?reply\b|auto(?:matically)?\s*generated|automated\s+(?:message|email))`),
		regexp.MustCompile(`(?i)(?:수신\s*거부|구독\s*취소|메일\s*수신을\s*원치|광고성\s*정보|개인정보처리방침|이용약관|\bunsubscribe\b|unsubscription|privacy\s+policy|terms\s+of\s+use|manage\s+preferences)`),
		regexp.MustCompile(`(?i)(?:all\s+rights\s+reserved|copyright|ⓒ|©|before\s+printing|think\s+about\s+the\s+environment|환경을\s*생각|인쇄하기\s*전)`),
		regexp.MustCompile(`(?i)(?:sent\s+with|protected\s+by|scanned\s+by|virus-free|avast|ahnlab|v3|메일보안)`),
		regexp.MustCompile(`(?i)(?:feedback\s*&\s*support|we'?re\s+here\s+to\s+help|start\s+building)`),
		regexp.MustCompile(`(?i)(?:if\s+you\s+have\s+any\s+questions,?\s+visit\s+our\s+support\s+site|something\s+wrong\s+with\s+the\s+email\?|view\s+in\s+browser|you'?re\s+receiving\s+this\s+email\s+because\s+you\s+made\s+a\s+purchase|partners\s+with\s+stripe)`),
		regexp.MustCompile(`(?i)(?:share\s+feedback\s+on|manage\s+notification\s+settings|you\s+are\s+receiving\s+this\s+because|unsubscribe\s+from\s+this\s+thread)`),
		regexp.MustCompile(`(?i)(?:if\s+you\s+believe\s+you\s+are\s+getting\s+this\s+email\s+in\s+error|to\s+learn\s+more\s+about\s+link|one\s+wilton\s+park)`),
		regexp.MustCompile(`(?i)(?:newsletter\s+has\s+been\s+prepared|does\s+not\s+include\s+the\s+opinion)`),
	}
	bodyPrepReplyHeaderREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*-{2,}\s*original\s+message\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*-{2,}\s*forwarded\s+message\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*원본\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*전달된\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:邮件原件|原始邮件|转发邮件)\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:from|sender|sent|to|cc|subject|title|date)\s*:`),
		regexp.MustCompile(`^\s*(?:보낸\s*사람|보내는\s*사람|발신자|보낸\s*날짜|보낸\s*일시|받는\s*사람|받은\s*사람|수신자|참조|제목|날짜)\s*[:：]`),
		regexp.MustCompile(`^\s*(?:发件人|寄件者|发送时间|发送日期|收件人|抄送|主题|日期)\s*[:：]`),
	}
	bodyPrepInlineReplyHeaderRE    = regexp.MustCompile(`(?i)\s+(?:date|from|to|cc|subject)\s*:\s+`)
	bodyPrepStrongReplyBoundaryREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*-{2,}\s*(?:original|forwarded)\s+(?:message|mail)\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:원본|전달된)\s*메시지\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}\s*(?:邮件原件|原始邮件|转发邮件)\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*-{2,}\s*original\s*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*-{2,}\s*original\s*$`),
		regexp.MustCompile(`(?i)^\s*message\s*-{2,}\s*$`),
		regexp.MustCompile(`^\s*-{2,}.*�.*-{2,}\s*$`),
		regexp.MustCompile(`(?i)^\s*on\s+.{3,240}\bwrote\s*:\s*$`),
		regexp.MustCompile(`^\s*.{3,240}<[^>]+@[^>]+>.*(?:작성|씀|写道)\s*:\s*$`),
		regexp.MustCompile(`^\s*\d{4}년\s*\d{1,2}월\s*\d{1,2}일.{0,180}(?:작성|씀)\s*:\s*$`),
	}
	bodyPrepTrailingNoiseREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:cid:|\[cid|\[image|<image|\blogo\b)`),
		regexp.MustCompile(`(?i)(?:^|\s)(?:https?://|www\.)\S*(?:facebook|instagram|youtube|linkedin|twitter|x\.com|blog)\S*\s*$`),
	}
	bodyPrepTrailingSignoffRE  = regexp.MustCompile(`^\s*(?:(?:[가-힣](?:\s*[가-힣]){1,3}|[가-힣]{2,4})\s*)?(?:드림|올림|배상)[\s,.!！。]*$`)
	bodyPrepMobileSignatureRE  = regexp.MustCompile(`(?i)^\s*(?:sent\s+from\s+my|sent\s+from\s+outlook\s+for|나의\s+.+에서\s+보냄|iPhone에서\s+보냄|Galaxy에서\s+보냄|Android에서\s+보냄|.{0,40}iPhone)\s*$`)
	bodyPrepShortNameRE        = regexp.MustCompile(`^\s*(?:[가-힣]\s*[가-힣]{1,3}|[A-Z][a-z]+(?:\s+[A-Z][a-z]+){0,2})\s*$`)
	bodyPrepTailNameRE         = regexp.MustCompile(`^\s*(?:[가-힣](?:\s*[가-힣]){1,3}|[가-힣]{2,4}\s*/\s*[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+){0,2}|[가-힣]{2,4}\s+[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+){0,3}|[가-힣]{2,4}\s+[A-Z]{2,}(?:\s+[A-Z]{2,}){1,3}|[A-Z][a-z]+(?:\s+[A-Z][a-z]+){1,2})\s*$`)
	bodyPrepKoreanTitleTailRE  = regexp.MustCompile(`^\s*(?:감사실|[가-힣]\s*[가-힣]\s*[가-힣]\s+(?:감사|공인회계사)(?:\s|/|$).*|.*공인회계사.*)\s*$`)
	bodyPrepHTMLSeparatorRE    = regexp.MustCompile(`(?i)^\s*<hr\b[^>]*>\s*$`)
	bodyPrepHTMLBlankRE        = regexp.MustCompile(`(?i)^\s*(?:<o:p>\s*</o:p>|<o:p>\s*(?:&nbsp;|\s)*\s*</o:p>|<br\s*/?>)\s*$`)
	bodyPrepHTMLMetaRE         = regexp.MustCompile(`(?i)^\s*<meta\b[^>]*>\s*$`)
	bodyPrepHTMLWrapperRE      = regexp.MustCompile(`(?i)^\s*</?(?:mailplughtml|html|head|body)\b[^>]*>\s*$`)
	bodyPrepHTMLInlineTagRE    = regexp.MustCompile(`(?i)</?(?:div|span|p|br|a|img|table|tbody|tr|td|font|strong|em|b|i|ul|ol|li|center)\b[^>]*>`)
	bodyPrepHTMLBreakTagRE     = regexp.MustCompile(`(?i)<br\s*/?>`)
	bodyPrepHTMLImageOnlyRE    = regexp.MustCompile(`(?i)^\s*<img\b[^>]*>\s*$`)
	bodyPrepHTMLTagRE          = regexp.MustCompile(`(?i)<[^>]+>`)
	bodyPrepHTMLSignatureRE    = regexp.MustCompile(`(?i)<span\b[^>]*\bshowField\(`)
	bodyPrepSpaceBeforePunctRE = regexp.MustCompile(`\s+([.,;:!?])`)
	bodyPrepThinForwardRE      = regexp.MustCompile(`(?i)(?:전달|아래|하기|원문|메일|below|forward|fyi|see\s+below)`)
	bodyPrepThinShareRE        = regexp.MustCompile(`(?i)(?:참조|참고|송부|공유|자료|attached)`)
	bodyPrepAttachmentLeadRE   = regexp.MustCompile(`(?i)^\s*(?:대용량\s*)?(?:파일\s*첨부|첨부\s*파일|첨부)(?:\s*총)?\s*\(?\s*\d+\s*개\s*\)?(?:\s*\(?[0-9][0-9.,]*\s*(?:b|kb|mb|gb|tb)\)?)?(?:.*다운로드\s*기간\s*[:：].*)?\s*$`)
	bodyPrepAttachmentHeadRE   = regexp.MustCompile(`(?i)^\s*(?:대용량\s*)?(?:파일\s*첨부|첨부\s*파일|첨부파일|첨부)(?:\s|$|\()`)
	bodyPrepAttachmentBodyRE   = regexp.MustCompile(`(?i)(?:[가-힣A-Za-z0-9()/·\s]{1,30}님\s+안녕하(?:세요|십니까)|안녕하(?:세요|십니까)|업무에\s+고생|수신\s*[:：]|발신\s*[:：]|\bDear\s+[A-Za-z가-힣]|\bHi[,\s]+[A-Za-z가-힣]|\bHello[,\s]+[A-Za-z가-힣])`)
	bodyPrepAttachmentMetaREs  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*\([0-9][0-9.,]*\s*(?:b|kb|mb|gb|tb)\)\s*$`),
		regexp.MustCompile(`^\s*다운로드\s*기간\s*[:：]`),
		regexp.MustCompile(`^\s*~\s*\d{4}[/-]\d{1,2}[/-]\d{1,2}\s*$`),
		regexp.MustCompile(`^\s*기한이\s*있는\s*파일은\s*\d+\s*일\s*보관`),
		regexp.MustCompile(`^\s*\((?:대용량\s*)?첨부\s*파일은\s*\d+\s*일간\s*보관\)\s*$`),
		regexp.MustCompile(`(?i)\.(?:zip|7z|rar|pdf|xlsx?|docx?|pptx?|hwp|hwpx|dwg|dxf|jpg|jpeg|png|gif|heic|csv|txt|eml|msg)(?:\s*$|\s*\(|\s*-|\s+[0-9]|<)`),
	}
	bodyPrepBareAttachmentNameRE = regexp.MustCompile(`(?i)^\s*(?:image|attachment|file|첨부)\d{0,4}\s*$`)
	bodyPrepBusinessListLeadRE   = regexp.MustCompile(`^\s*(?:[-*•]\s*|[0-9]{1,2}[.)]\s+|[가-하][.)]\s+)`)
	bodyPrepBusinessSentenceRE   = regexp.MustCompile(`(?:입니다|있습니다|없습니다|했습니다|하였습니다|드립니다|부탁|요청|확인|검토|진행|회신|공유|참고|첨부|발생|필요|상황|의견|문의|제공|협의|일정|납부|고지서|미납|입금|계좌|세금계산서|발행|계약서|회계|비용|처리|준비|등록|접수|현장|공사|금액|대납|임대인|한전)`)
	bodyPrepFinancialDocRE       = regexp.MustCompile(`(?i)(?:invoice|receipt|refund|refunded|credit\s+note|amount\s+paid|total\s+credit|subtotal|\bVAT\b|payment|issued|card|american\s+express|[$€£₩]\s*\d)`)
	bodyPrepReceiptVendorRE      = regexp.MustCompile(`(?i)^\s*anthropic,\s*pbc\b(?:\s*[\(<].*)?$`)
	bodyPrepReceiptSupportRE     = regexp.MustCompile(`(?i)\s+questions\?\s+.*$`)
	bodyPrepReceiptInlineREs     = []*regexp.Regexp{
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
	bodyPrepMeetingDetailRE          = regexp.MustCompile(`(?i)(?:microsoft\s+teams|teams\.microsoft|zoom\.us|google\s+meet|meet\.google|join\s*:|meeting\s+id|passcode|회의\s*(?:참가|링크)|미팅\s*(?:참가|링크))`)
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
			if start := bodyPrepForwardedBodyStart(lines, cut); start > cut && start < len(lines) && (bodyPrepLinesVisibleEnough(lines[start:]) || bodyPrepSuffixHasPreservedReplyBody(lines[cut:])) {
				result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history-header", Lines: bodyPrepNonBlankLineCount(lines[:start])})
				lines = compactBodyPrepBlankLines(lines[start:])
				lines = stripBodyPrepForwardedHeadArtifacts(lines, &result)
				continue
			} else if bodyPrepSuffixHasPreservedReplyBody(lines[cut:]) {
				break
			} else {
				result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
				lines = compactBodyPrepBlankLines(lines[:cut])
			}
		} else {
			if bodyPrepSuffixHasPreservedReplyBody(lines[cut:]) {
				break
			}
			result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history", Lines: bodyPrepNonBlankLineCount(lines[cut:])})
			lines = compactBodyPrepBlankLines(lines[:cut])
		}
		break
	}

	lines, removed = stripBodyPrepSignatureBeforeReplyHistory(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "signature", Lines: removed})
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

	lines, removed = stripBodyPrepReplyArtifactsPreservingBody(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "history-header", Lines: removed})
	}

	lines, removed = stripBodyPrepTrailingNoiseLines(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "tail", Lines: removed})
	}

	lines, removed = stripBodyPrepDecorativeSeparatorLines(lines)
	if removed > 0 {
		result.HiddenBlocks = append(result.HiddenBlocks, HiddenBlock{Kind: "separator", Lines: removed})
	}

	result.Body = normalizeBodyPrep(strings.Join(lines, "\n"))
	result.CleanRunes = visibleBodyPrepRunes(result.Body)
	return result
}
