package groupware

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// approvalAttachMaxFiles caps how many attachments the autonomous analyzer
	// downloads+OCRs per document — OCR dominates latency on the radar path.
	approvalAttachMaxFiles = 3
	// approvalAttachInjectRunes bounds each selected attachment's injected text.
	approvalAttachInjectRunes = 12000
	// approvalAttachInjectTotalRunes caps combined injected attachment text.
	approvalAttachInjectTotalRunes = 24000
)

// approvalAttachmentBudget bounds the whole download+OCR phase so a slow scan
// cannot starve the analysis LLM call. Package var so tests can shrink it.
var approvalAttachmentBudget = 90 * time.Second

// ApprovalAttachmentRef is one row from the Amaranth read body's attachment
// listing ("첨부 (N건 · 내용 미열람)" numbered list). Index is 1-based.
type ApprovalAttachmentRef struct {
	Index int
	Name  string
}

// ApprovalAttachmentEvidence keeps trusted attachment identity separate from
// untrusted extracted text. Callers can render provenance labels without ever
// reparsing delimiter-like strings from the attachment body.
type ApprovalAttachmentEvidence struct {
	Index int
	Name  string
	Text  string
}

var (
	approvalAttachListHeader = regexp.MustCompile(`(?m)^첨부\s*\((\d+)\s*건`)
	approvalAttachListItem   = regexp.MustCompile(`(?m)^(\d+)\.\s+(.+?)(?:\s+·\s+\S+)?\s*$`)
	approvalAttachDocExts    = []string{
		".pdf", ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".hwp", ".hwpx", ".csv",
	}
	approvalAttachImageExts = []string{
		".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tif", ".tiff",
	}
	// businessDocSignals force-include 견적/계약/세금계산서-class docs even when the
	// list is long. Mirrors mailanalysis so the two pipelines stay aligned.
	approvalBusinessDocSignals = []string{
		"견적", "계약", "세금계산서", "계산서", "거래명세", "명세서", "발주", "수주", "주문",
		"제안", "사양", "단가", "대여", "대차", "invoice", "quote", "quotation",
		"contract", "estimate", "purchase",
	}
	// imageWorthSignals: scanned receipts / site photos that matter for spend
	// approvals. Images without these stay unread (OCR is expensive noise).
	approvalImageWorthSignals = []string{
		"영수증", "지출", "명세", "견적", "계약", "세금", "계산서", "사진대지", "청구",
	}
)

// ParseApprovalAttachmentList extracts numbered attachment rows from a groupware
// approval read body. Returns nil when the listing is absent or empty.
func ParseApprovalAttachmentList(body string) []ApprovalAttachmentRef {
	if !approvalAttachListHeader.MatchString(body) {
		return nil
	}
	// Prefer the trailing "첨부 (N건 …)" block — earlier body text may use "1." lists.
	idx := strings.LastIndex(body, "첨부 (")
	if idx < 0 {
		idx = strings.LastIndex(body, "첨부(")
	}
	if idx < 0 {
		return nil
	}
	block := body[idx:]
	var out []ApprovalAttachmentRef
	seen := map[int]struct{}{}
	for _, m := range approvalAttachListItem.FindAllStringSubmatch(block, -1) {
		if len(m) < 3 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		name := strings.TrimSpace(m[2])
		if name == "" {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, ApprovalAttachmentRef{Index: n, Name: name})
	}
	return out
}

// SelectApprovalAttachmentsForAnalysis picks which listed files to download for
// autonomous analysis. Prefer business-document filenames, then other office/PDF
// files, then image receipts — capped at approvalAttachMaxFiles. Fail-open on
// empty/unknown: returns nil (body-only analysis).
func SelectApprovalAttachmentsForAnalysis(refs []ApprovalAttachmentRef) []ApprovalAttachmentRef {
	if len(refs) == 0 {
		return nil
	}
	var business, docs, images []ApprovalAttachmentRef
	for _, r := range refs {
		switch {
		case isApprovalBusinessDoc(r.Name):
			business = append(business, r)
		case hasAnyExt(r.Name, approvalAttachDocExts):
			docs = append(docs, r)
		case hasAnyExt(r.Name, approvalAttachImageExts) && hasAnySignal(r.Name, approvalImageWorthSignals):
			images = append(images, r)
		}
	}
	out := make([]ApprovalAttachmentRef, 0, approvalAttachMaxFiles)
	for _, group := range [][]ApprovalAttachmentRef{business, docs, images} {
		for _, r := range group {
			if len(out) >= approvalAttachMaxFiles {
				return out
			}
			out = append(out, r)
		}
	}
	return out
}

// SelectApprovalAttachmentsForQuestion prioritizes files explicitly named (or
// numbered) by the user's question, then fills the bounded set with the normal
// business-document selection and remaining list order. Filling is unconditional:
// a general clause question may be answered only by a non-heuristic fourth file.
func SelectApprovalAttachmentsForQuestion(refs []ApprovalAttachmentRef, question string, maxFiles int) []ApprovalAttachmentRef {
	if maxFiles <= 0 || len(refs) == 0 {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(question))
	out := make([]ApprovalAttachmentRef, 0, min(maxFiles, len(refs)))
	seen := make(map[int]struct{}, len(refs))
	appendRef := func(ref ApprovalAttachmentRef) {
		if len(out) >= maxFiles {
			return
		}
		if _, ok := seen[ref.Index]; ok {
			return
		}
		seen[ref.Index] = struct{}{}
		out = append(out, ref)
	}
	for _, ref := range refs {
		name := strings.ToLower(strings.TrimSpace(ref.Name))
		base := strings.TrimSuffix(name, strings.ToLower(filepath.Ext(name)))
		numbered := []string{
			fmt.Sprintf("%d번", ref.Index),
			fmt.Sprintf("%d번째", ref.Index),
			fmt.Sprintf("첨부 %d", ref.Index),
			fmt.Sprintf("첨부파일 %d", ref.Index),
		}
		if (name != "" && strings.Contains(q, name)) || (utf8.RuneCountInString(base) >= 2 && strings.Contains(q, base)) || hasAnySignal(q, numbered) {
			appendRef(ref)
		}
	}
	for _, ref := range SelectApprovalAttachmentsForAnalysis(refs) {
		appendRef(ref)
	}
	for _, ref := range refs {
		appendRef(ref)
	}
	return out
}

// LoadApprovalAttachmentsForAnalysis downloads+extracts the selected attachments
// and returns a "## 첨부 내용" section ready to append to the analysis user
// prompt. Never errors: missing credentials, empty lists, or per-file failures
// yield "" so analysis proceeds body-only.
func LoadApprovalAttachmentsForAnalysis(ctx context.Context, cfg Config, docID, body string) string {
	selected := SelectApprovalAttachmentsForAnalysis(ParseApprovalAttachmentList(body))
	evidence := LoadApprovalAttachmentEvidence(ctx, cfg, docID, selected)
	if len(evidence) == 0 {
		return ""
	}
	chunks := make([]string, 0, len(evidence))
	for _, item := range evidence {
		chunks = append(chunks, fmt.Sprintf("### 📎 %s\n%s", item.Name, item.Text))
	}
	return "\n\n## 첨부 내용\n\n" + strings.Join(chunks, "\n\n")
}

// LoadApprovalAttachmentEvidence downloads and extracts a caller-selected,
// bounded list while preserving trusted ref identity as separate fields.
func LoadApprovalAttachmentEvidence(ctx context.Context, cfg Config, docID string, selected []ApprovalAttachmentRef) []ApprovalAttachmentEvidence {
	return loadApprovalAttachmentEvidence(ctx, cfg, docID, selected, "")
}

// LoadApprovalAttachmentEvidenceForQuestion keeps the bounded excerpt around a
// question match instead of always taking the start of a long extracted file.
func LoadApprovalAttachmentEvidenceForQuestion(
	ctx context.Context,
	cfg Config,
	docID string,
	selected []ApprovalAttachmentRef,
	question string,
) []ApprovalAttachmentEvidence {
	return loadApprovalAttachmentEvidence(ctx, cfg, docID, selected, question)
}

// LoadApprovalAttachmentExtractedEvidence downloads and extracts selected files
// without clipping their text. Callers caching this form must enforce their own
// memory bound and select a bounded excerpt before model injection.
func LoadApprovalAttachmentExtractedEvidence(
	ctx context.Context,
	cfg Config,
	docID string,
	selected []ApprovalAttachmentRef,
) []ApprovalAttachmentEvidence {
	docID = strings.TrimSpace(docID)
	if docID == "" || strings.TrimSpace(cfg.User) == "" || cfg.Password == "" || len(selected) == 0 {
		return nil
	}
	actx, cancel := context.WithTimeout(ctx, approvalAttachmentBudget)
	defer cancel()

	evidence := make([]ApprovalAttachmentEvidence, 0, len(selected))
	for _, ref := range selected {
		if actx.Err() != nil {
			break
		}
		raw, err := ReadApprovalAttachment(actx, cfg, docID, fmt.Sprintf("%d", ref.Index))
		if err != nil || strings.TrimSpace(raw) == "" {
			continue
		}
		text := approvalAttachmentExtractBody(raw)
		if text == "" {
			continue
		}
		evidence = append(evidence, ApprovalAttachmentEvidence{Index: ref.Index, Name: ref.Name, Text: text})
	}
	return evidence
}

func loadApprovalAttachmentEvidence(
	ctx context.Context,
	cfg Config,
	docID string,
	selected []ApprovalAttachmentRef,
	question string,
) []ApprovalAttachmentEvidence {
	extracted := LoadApprovalAttachmentExtractedEvidence(ctx, cfg, docID, selected)
	evidence := make([]ApprovalAttachmentEvidence, 0, len(extracted))
	total := 0
	for _, item := range extracted {
		remaining := approvalAttachInjectTotalRunes - total
		if remaining <= 0 {
			break
		}
		limit := approvalAttachInjectRunes
		if limit > remaining {
			limit = remaining
		}
		clipped := SelectApprovalEvidenceContext(ctx, item.Text, question, limit)
		if clipped == "" && ctx.Err() != nil {
			break
		}
		evidence = append(evidence, ApprovalAttachmentEvidence{Index: item.Index, Name: item.Name, Text: clipped})
		total += utf8.RuneCountInString(clipped)
	}
	return evidence
}

func approvalAttachmentExtractBody(raw string) string {
	raw = strings.TrimSpace(raw)
	const marker = "추출 본문"
	if i := strings.Index(raw, marker); i >= 0 {
		body := strings.TrimSpace(raw[i+len(marker):])
		if body != "" {
			return body
		}
	}
	// Calm "no text" notes are not worth injecting.
	if strings.Contains(raw, "텍스트 추출 결과 없음") || strings.Contains(raw, "추출 미지원") {
		return ""
	}
	return raw
}

func truncateApprovalAttachRunes(s string, max int) string {
	return SelectApprovalEvidence(s, "", max)
}

const (
	approvalEvidenceOmission             = "\n…(middle truncated; question-relevant excerpt retained for approval Q&A)…\n"
	approvalEvidenceMaxQuestionTerms     = 16
	approvalEvidenceMaxQuestionTermRunes = 64
)

// SelectApprovalEvidence returns a strictly bounded excerpt. It preserves the
// beginning and end for document metadata while reserving most of the budget
// for a question-term match. With no useful match it falls back to head+tail.
func SelectApprovalEvidence(text, question string, maxRunes int) string {
	return SelectApprovalEvidenceContext(context.Background(), text, question, maxRunes)
}

// SelectApprovalEvidenceContext is SelectApprovalEvidence with request
// cancellation. A canceled request returns no evidence instead of continuing
// repeated scans over a large extracted document.
func SelectApprovalEvidenceContext(ctx context.Context, text, question string, maxRunes int) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ""
	}
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if ctx.Err() != nil {
		return ""
	}
	if len(runes) <= maxRunes {
		return text
	}
	marker := []rune(approvalEvidenceOmission)
	if maxRunes <= len(marker) {
		return string(runes[:maxRunes])
	}
	match, canceled := approvalEvidenceQuestionMatch(ctx, text, question)
	if canceled {
		return ""
	}
	if match < 0 {
		available := maxRunes - len(marker)
		head := available * 3 / 4
		tail := available - head
		return string(runes[:head]) + string(marker) + string(runes[len(runes)-tail:])
	}

	// Budget for the worst case of three disjoint ranges and two markers.
	available := maxRunes - 2*len(marker)
	if available <= 0 {
		return string(runes[:maxRunes])
	}
	headBudget := available / 5
	tailBudget := available / 5
	focusBudget := available - headBudget - tailBudget
	focusStart := match - focusBudget/3
	if focusStart < 0 {
		focusStart = 0
	}
	if focusStart+focusBudget > len(runes) {
		focusStart = len(runes) - focusBudget
	}

	ranges := []approvalEvidenceRange{
		{start: 0, end: headBudget},
		{start: focusStart, end: focusStart + focusBudget},
		{start: len(runes) - tailBudget, end: len(runes)},
	}
	merged := make([]approvalEvidenceRange, 0, len(ranges))
	for _, current := range ranges {
		if current.start < 0 {
			current.start = 0
		}
		if current.end > len(runes) {
			current.end = len(runes)
		}
		if current.start >= current.end {
			continue
		}
		if len(merged) == 0 || current.start > merged[len(merged)-1].end {
			merged = append(merged, current)
			continue
		}
		if current.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = current.end
		}
	}

	var b strings.Builder
	for i, selected := range merged {
		if i > 0 {
			b.WriteString(approvalEvidenceOmission)
		}
		b.WriteString(string(runes[selected.start:selected.end]))
	}
	return b.String()
}

type approvalEvidenceRange struct {
	start int
	end   int
}

func approvalEvidenceQuestionMatch(ctx context.Context, text, question string) (int, bool) {
	if ctx.Err() != nil {
		return -1, true
	}
	lower := strings.ToLower(text)
	if ctx.Err() != nil {
		return -1, true
	}
	for _, term := range approvalEvidenceQuestionTerms(question) {
		if ctx.Err() != nil {
			return -1, true
		}
		byteIndex := strings.Index(lower, term)
		if byteIndex >= 0 {
			return utf8.RuneCountInString(lower[:byteIndex]), false
		}
	}
	return -1, false
}

func approvalEvidenceQuestionTerms(question string) []string {
	stop := map[string]struct{}{
		"결재": {}, "근거": {}, "내용": {}, "문서": {}, "본문": {}, "분석": {}, "첨부": {}, "파일": {},
		"무엇": {}, "무엇인지": {}, "어떻게": {}, "얼마": {}, "얼마야": {}, "언제": {}, "어디": {}, "누구": {}, "왜": {},
		"알려줘": {}, "주세요": {}, "확인": {}, "관련": {}, "대해": {}, "대한": {},
	}
	suffixes := []string{
		"으로부터", "에게서", "에서는", "인가요", "일까요", "입니까", "습니까",
		"에서", "에게", "한테", "으로", "까지", "부터", "처럼", "보다", "라도", "이나", "거나", "하고", "이며", "이고",
		"은", "는", "이", "가", "을", "를", "의", "에", "로", "와", "과", "도", "만",
	}
	type candidate struct {
		text   string
		length int
	}
	seen := make(map[string]struct{})
	candidates := make([]candidate, 0, 16)
	appendTerm := func(term string) {
		term = strings.ToLower(strings.TrimSpace(term))
		runes := []rune(term)
		if len(runes) < 2 {
			return
		}
		if len(runes) > approvalEvidenceMaxQuestionTermRunes {
			runes = runes[:approvalEvidenceMaxQuestionTermRunes]
			term = string(runes)
		}
		if _, generic := stop[term]; generic {
			return
		}
		if _, duplicate := seen[term]; duplicate {
			return
		}
		seen[term] = struct{}{}
		candidates = append(candidates, candidate{text: term, length: len(runes)})
	}
	var token strings.Builder
	flush := func() {
		raw := strings.ToLower(token.String())
		token.Reset()
		if raw == "" {
			return
		}
		appendTerm(raw)
		for _, suffix := range suffixes {
			if !strings.HasSuffix(raw, suffix) {
				continue
			}
			stem := strings.TrimSuffix(raw, suffix)
			if utf8.RuneCountInString(stem) >= 2 {
				appendTerm(stem)
			}
			break
		}
	}
	for _, r := range question {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			token.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].length > candidates[j].length
	})
	if len(candidates) > approvalEvidenceMaxQuestionTerms {
		candidates = candidates[:approvalEvidenceMaxQuestionTerms]
	}
	terms := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		terms = append(terms, candidate.text)
	}
	return terms
}

func isApprovalBusinessDoc(name string) bool {
	if !hasAnyExt(name, approvalAttachDocExts) {
		return false
	}
	return hasAnySignal(name, approvalBusinessDocSignals)
}

func hasAnyExt(name string, exts []string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func hasAnySignal(name string, signals []string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, sig := range signals {
		if strings.Contains(lower, strings.ToLower(sig)) {
			return true
		}
	}
	return false
}
