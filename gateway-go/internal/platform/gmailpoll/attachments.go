// attachments.go — selective attachment reading for the autonomous mail
// analysis pipeline.
//
// The autonomous analysis otherwise reads only the email body, so the documents
// that matter most — 견적서·계약서·세금계산서 arriving as PDF/image attachments —
// never reach the analysis (or the deal extractor). Reading ALL of them would
// pile noise (logos, signatures, boilerplate) into the prompt, so this gate has
// a local LLM judge which attachments are worth reading:
//
//  1. Heuristic pre-filter — skip tiny inline bits and non-document types
//     (mirrors dropbox_archive's isArchivable, plus images for scanned docs).
//  2. Bounded extraction — each candidate is extracted once (page/char bounded
//     by the extractor + the caps here) so the judge sees real content, not just
//     an opaque filename.
//  3. LLM relevance gate — a local-model call picks the subset worth injecting.
//     Unambiguous business documents (견적서/계약서/세금계산서 등, by filename) are
//     force-included so a flaky tiny-model verdict never silently drops a real
//     deal document; and if the judge call fails outright, all pre-filtered
//     candidates are injected rather than dropped (best-effort INCLUDE, since the
//     executive wants the documents read).
//
// Selected attachments' text is injected into the analysis input up to a generous
// per-document cap (a multi-page 견적서/계약서 fits in full); anything longer is
// injected up to the cap and reported in Truncated so the analysis can note the
// original runs longer. Best-effort throughout: extraction failure for a file
// drops just that file; the analysis proceeds with whatever was read.
//
// Security note: attachment content is untrusted (it could carry prompt-
// injection text). The same is already true of the email body and the
// interactive attachment tool; the autonomous analysis is a read-only synthesis
// with no tool execution, so the blast radius is a misleading summary, not an
// action. Keep it that way — do not give this path tools that act on the
// extracted text.
package gmailpoll

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonschema"
)

const (
	// minAttachmentSize skips tiny inline bits (tracker pixels, signature glyphs).
	minAttachmentSize = 2048
	// maxAttachmentSize skips oversized attachments. Kept aligned with the LMTP
	// parser's per-part cap so larger business PDFs can be analyzed without
	// injecting partial, truncated files.
	maxAttachmentSize = 32 * 1024 * 1024
	// maxAttachmentCandidates caps how many attachments are extracted+judged per
	// mail, bounding OCR cost on a pathological many-attachment mail.
	maxAttachmentCandidates = 6
	// attachmentSnippetChars bounds the per-candidate snippet shown to the judge.
	attachmentSnippetChars = 600
	// attachmentInjectChars bounds each selected attachment's injected text.
	// Sized to fit a multi-page 견적서/계약서 in full (≈6-10 pages of Korean
	// business text) so the autonomous analysis reads the actual document, not
	// just its cover page. Documents longer than this are injected up to the cap
	// and flagged as partially-included (see attachmentSelection.Truncated).
	attachmentInjectChars = 12000
	// attachmentInjectTotalChars caps the combined injected attachment text across
	// all selected attachments on one mail, bounding the per-cycle prompt size.
	attachmentInjectTotalChars = 30000
	// attachmentExtractBudget bounds the fetch+OCR phase so a slow multi-page scan
	// can never starve the analysis. Mirrors the sibling stage-1 extractors'
	// bounded contexts; on timeout the gate judges whatever it extracted so far.
	attachmentExtractBudget = 90 * time.Second
	// attachmentJudgeBudget bounds the relevance decision on a fresh context
	// derived from the parent (not the extract budget), so slow extraction can't
	// starve the cheap judgment and waste the extraction work.
	attachmentJudgeBudget = 20 * time.Second
)

// attachmentExtractExts are the filename extensions worth extracting. Images
// are included (scanned 견적서/명세서 photos) on top of dropbox_archive's set.
var attachmentExtractExts = []string{
	".pdf", ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".hwp", ".hwpx", ".csv",
	".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tif", ".tiff",
}

// attachmentSelection is the gate's output: the text section to append to the
// analysis input, and the filenames that exceeded the injection cap (only a
// bounded prefix made it in, so the analysis can flag that the original is longer).
type attachmentSelection struct {
	Injected  string   // "## 첨부 내용" section, or "" when nothing selected
	Truncated []string // filenames whose text was clipped to the inject cap
}

// extractedAttachment pairs a candidate with its bounded extracted text.
type extractedAttachment struct {
	att  gmail.AttachmentInfo
	text string
}

// gateAndExtractAttachments reads the relevant attachments of a mail, judged by
// a local LLM. Returns an empty selection (and never errors) whenever the deps
// aren't wired, there are no document attachments, or any step fails — the
// analysis then proceeds body-only exactly as before.
func gateAndExtractAttachments(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail) attachmentSelection {
	var none attachmentSelection
	if deps.AttachmentExtractFn == nil || deps.LocalClient == nil || deps.AttachmentBytesFn == nil || msg == nil {
		return none
	}

	candidates := attachmentCandidates(msg.Attachments)
	if len(candidates) == 0 {
		return none
	}

	// Bound extraction (fetch + OCR) so a slow multi-page scan can't eat the
	// analysis budget. The judge gets its OWN budget from the parent ctx below,
	// so even if extraction consumes all of ectx the judgment still runs on what
	// was extracted — extraction work is never wasted.
	ectx, ecancel := context.WithTimeout(ctx, attachmentExtractBudget)
	defer ecancel()

	// Extract each candidate once (bounded), so the judge sees real content.
	extracted := make([]extractedAttachment, 0, len(candidates))
	for _, att := range candidates {
		if ectx.Err() != nil {
			break // extraction budget spent — judge on what we have
		}
		data, err := deps.AttachmentBytesFn(ectx, msg.ID, att.AttachmentID)
		if err != nil || len(data) == 0 {
			continue
		}
		text := strings.TrimSpace(deps.AttachmentExtractFn(ectx, data, att.Filename, att.MimeType))
		if text == "" {
			continue
		}
		extracted = append(extracted, extractedAttachment{att: att, text: text})
	}
	if len(extracted) == 0 {
		return none
	}

	// Judge on a fresh budget from the parent ctx, decoupled from extraction, so
	// slow extraction can never starve the (cheap) relevance decision.
	jctx, jcancel := context.WithTimeout(ctx, attachmentJudgeBudget)
	defer jcancel()
	picks := judgeAttachments(jctx, deps, msg, extracted)
	if len(picks) == 0 {
		return none
	}

	sel := buildAttachmentSelection(extracted, picks)
	gateLogger(deps).Info("mail attachment gate: injected",
		"id", msg.ID, "candidates", len(candidates), "extracted", len(extracted),
		"selected", len(picks), "truncated", len(sel.Truncated))
	return sel
}

// gateLogger returns the deps logger or the default — keeps the gate's Debug
// observability non-fatal when no logger is wired.
func gateLogger(deps PipelineDeps) *slog.Logger {
	if deps.Logger != nil {
		return deps.Logger
	}
	return slog.Default()
}

// attachmentCandidates applies the heuristic pre-filter: document/image type and
// above the tiny-inline-bit threshold, capped in count.
func attachmentCandidates(atts []gmail.AttachmentInfo) []gmail.AttachmentInfo {
	out := make([]gmail.AttachmentInfo, 0, len(atts))
	for _, att := range atts {
		if att.Truncated {
			continue
		}
		if att.Size < minAttachmentSize || att.Size > maxAttachmentSize || att.AttachmentID == "" {
			continue
		}
		if !isExtractableAttachment(att) {
			continue
		}
		out = append(out, att)
		if len(out) >= maxAttachmentCandidates {
			break
		}
	}
	return out
}

func isExtractableAttachment(att gmail.AttachmentInfo) bool {
	lower := strings.ToLower(att.Filename)
	for _, ext := range attachmentExtractExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	// Fall back to MIME for filenames without a useful extension.
	mime := strings.ToLower(att.MimeType)
	return strings.HasPrefix(mime, "image/") || strings.Contains(mime, "pdf") ||
		strings.Contains(mime, "officedocument") || strings.Contains(mime, "msword") ||
		strings.Contains(mime, "ms-excel") || strings.Contains(mime, "spreadsheetml")
}

// attachGateResult is the local-model judgment.
type attachGateResult struct {
	Selections []attachGateItem `json:"selections"`
}

type attachGateItem struct {
	Index    int  `json:"index"`
	Relevant bool `json:"relevant"`
}

// attachGateSchema is the strict json_schema for attachGateResult, derived from
// the Go type (jsonschema.For) — guided decoding guarantees a real integer index
// + boolean relevant per selection (no "0"/"true" string drift).
var attachGateSchema = jsonschema.For[attachGateResult]("attachment_selections")

const attachGateSystem = "당신은 업무 메일 분석을 돕는 첨부 선별기입니다. " +
	"메일 내용에 비추어, 분석에 본문으로 읽을 가치가 있는 첨부만 고릅니다. " +
	"견적서·계약서·세금계산서·거래명세서·발주서·제안서·사양서처럼 업무 판단에 필요한 문서는 relevant=true. " +
	"로고·서명 이미지·약관·홍보물·수신거부 안내·반복 푸터처럼 노이즈는 relevant=false. " +
	"애매하면 relevant=true로 둔다 — 업무 문서를 놓치는 것이 노이즈를 넣는 것보다 나쁘다."

const attachGatePrompt = `메일:
제목: %s
보낸사람: %s
본문 발췌: %s

첨부 후보 (각 index, 파일명, 첫 내용 발췌):
%s

각 첨부를 분석에 읽을지 판단하라. 정확히 다음 JSON만 출력:
{"selections":[{"index":0,"relevant":true}]}`

// judgeAttachments asks the local model which extracted attachments to inject and
// returns the set of selected indices. Failure-open by design: if the judge call
// fails, all pre-filtered candidates are selected (they already passed the
// document/image + size filter), and unambiguous business documents are always
// selected regardless of the judge — a flaky tiny-model verdict must never
// silently drop a 견적서/계약서. The cost of an extra noisy attachment is far
// smaller than missing a deal document the executive needs.
func judgeAttachments(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail, extracted []extractedAttachment) map[int]bool {
	picks := make(map[int]bool, len(extracted))

	var sb strings.Builder
	for i, e := range extracted {
		fmt.Fprintf(&sb, "[%d] %s (%s)\n%s\n\n", i, e.att.Filename, e.att.MimeType, clipChars(e.text, attachmentSnippetChars))
	}
	prompt := fmt.Sprintf(attachGatePrompt,
		msg.Subject, msg.From, clipChars(msg.Body, 1200), strings.TrimSpace(sb.String()))

	res, err := callLocalLLMJSON[attachGateResult](ctx, deps.LocalClient, deps.LocalModel, attachGateSystem, prompt, stage1MaxTokens, attachGateSchema)
	if err != nil {
		// Judge unavailable — include all extracted candidates rather than drop
		// them. They already cleared the heuristic pre-filter.
		for i := range extracted {
			picks[i] = true
		}
		return picks
	}
	for _, s := range res.Selections {
		if s.Relevant && s.Index >= 0 && s.Index < len(extracted) {
			picks[s.Index] = true
		}
	}
	// Force-include unambiguous business documents the tiny judge may have missed.
	for i, e := range extracted {
		if !picks[i] && isClearBusinessDoc(e.att.Filename) {
			picks[i] = true
		}
	}
	return picks
}

// attachmentDocExts is the document subset of attachmentExtractExts (images
// excluded): only real documents are force-included by filename, so a photo named
// "계약" still goes through the OCR-relevance judge instead of being injected blind.
var attachmentDocExts = []string{
	".pdf", ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".hwp", ".hwpx", ".csv",
}

// businessDocSignals mark documents the executive always wants read in full —
// 견적서/계약서/세금계산서/거래명세서/발주서 등. A document whose filename carries one
// of these is injected even if the tiny relevance judge misjudged it as noise.
var businessDocSignals = []string{
	"견적", "계약", "세금계산서", "계산서", "거래명세", "명세서", "발주", "수주", "주문",
	"제안", "사양", "단가", "invoice", "quote", "quotation", "contract", "estimate", "purchase",
}

// isClearBusinessDoc reports whether filename is an unambiguous business document
// (a document-type extension carrying a 견적/계약/세금계산서/… signal).
func isClearBusinessDoc(filename string) bool {
	lower := strings.ToLower(filename)
	isDoc := false
	for _, ext := range attachmentDocExts {
		if strings.HasSuffix(lower, ext) {
			isDoc = true
			break
		}
	}
	if !isDoc {
		return false
	}
	for _, sig := range businessDocSignals {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// buildAttachmentSelection renders the injection section from the selected
// attachments, honoring the per-attachment and total character caps (counted in
// runes throughout). A document whose text exceeds its cap is injected up to the
// cap and recorded in Truncated so the analysis can note the original runs longer.
// The "## 첨부 내용" header is assembled only when at least one attachment's text
// was injected.
func buildAttachmentSelection(extracted []extractedAttachment, picks map[int]bool) attachmentSelection {
	var chunks, truncated []string
	total := 0
	for i, e := range extracted {
		if !picks[i] {
			continue
		}
		full := strings.TrimSpace(e.text)
		fullLen := utf8.RuneCountInString(full)
		remaining := attachmentInjectTotalChars - total
		if remaining <= 0 {
			// Total budget spent before this doc — none of it is injected, so the
			// analysis should know this attachment went unread.
			truncated = append(truncated, e.att.Filename)
			continue
		}
		limit := min(attachmentInjectChars, remaining)
		text := clipChars(full, limit)
		if fullLen > limit {
			truncated = append(truncated, e.att.Filename)
		}
		chunks = append(chunks, fmt.Sprintf("### 📎 %s\n%s", e.att.Filename, text))
		total += utf8.RuneCountInString(text)
	}
	if len(chunks) == 0 {
		return attachmentSelection{}
	}
	return attachmentSelection{
		Injected:  "\n\n## 첨부 내용\n\n" + strings.Join(chunks, "\n\n") + "\n",
		Truncated: truncated,
	}
}

// clipChars truncates s to at most n runes, appending an ellipsis marker when cut.
func clipChars(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " …(생략)"
}
