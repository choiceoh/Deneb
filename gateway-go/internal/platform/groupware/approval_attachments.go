package groupware

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
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

// LoadApprovalAttachmentsForAnalysis downloads+extracts the selected attachments
// and returns a "## 첨부 내용" section ready to append to the analysis user
// prompt. Never errors: missing credentials, empty lists, or per-file failures
// yield "" so analysis proceeds body-only.
func LoadApprovalAttachmentsForAnalysis(ctx context.Context, cfg Config, docID, body string) string {
	docID = strings.TrimSpace(docID)
	if docID == "" || strings.TrimSpace(cfg.User) == "" || cfg.Password == "" {
		return ""
	}
	selected := SelectApprovalAttachmentsForAnalysis(ParseApprovalAttachmentList(body))
	if len(selected) == 0 {
		return ""
	}
	actx, cancel := context.WithTimeout(ctx, approvalAttachmentBudget)
	defer cancel()

	var chunks []string
	total := 0
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
		remaining := approvalAttachInjectTotalRunes - total
		if remaining <= 0 {
			break
		}
		limit := approvalAttachInjectRunes
		if limit > remaining {
			limit = remaining
		}
		clipped := truncateApprovalAttachRunes(text, limit)
		chunks = append(chunks, fmt.Sprintf("### 📎 %s\n%s", ref.Name, clipped))
		total += utf8.RuneCountInString(clipped)
	}
	if len(chunks) == 0 {
		return ""
	}
	return "\n\n## 첨부 내용\n\n" + strings.Join(chunks, "\n\n")
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
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "\n…(truncated)"
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
