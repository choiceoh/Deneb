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
//  3. LLM relevance gate — a local-model call picks the subset worth injecting
//     and flags any that warrant a deeper agentic read (escalation to chat).
//
// Only the selected attachments' text is injected into the analysis input; the
// rest are dropped. Best-effort throughout: any failure yields an empty
// selection and the analysis proceeds body-only.
package gmailpoll

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

const (
	// minAttachmentSize skips tiny inline bits (tracker pixels, signature glyphs).
	minAttachmentSize = 2048
	// maxAttachmentCandidates caps how many attachments are extracted+judged per
	// mail, bounding OCR cost on a pathological many-attachment mail.
	maxAttachmentCandidates = 6
	// attachmentSnippetChars bounds the per-candidate snippet shown to the judge.
	attachmentSnippetChars = 600
	// attachmentInjectChars bounds each selected attachment's injected text.
	attachmentInjectChars = 3500
	// attachmentInjectTotalChars caps the combined injected attachment text.
	attachmentInjectTotalChars = 9000
)

// attachmentExtractTypes are the filename extensions worth extracting. Images
// are included (scanned 견적서/명세서 photos) on top of dropbox_archive's set.
var attachmentExtractExts = []string{
	".pdf", ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".hwp", ".hwpx", ".csv",
	".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tif", ".tiff",
}

// attachmentSelection is the gate's output: the text section to append to the
// analysis input, and the filenames flagged for a deeper agentic read.
type attachmentSelection struct {
	Injected   string   // "## 첨부 내용" section, or "" when nothing selected
	DeepReview []string // filenames the judge flagged for chat-agent deep review
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
	if deps.AttachmentExtractFn == nil || deps.LocalClient == nil || deps.GmailClient == nil || msg == nil {
		return none
	}

	candidates := attachmentCandidates(msg.Attachments)
	if len(candidates) == 0 {
		return none
	}

	// Extract each candidate once (bounded), so the judge sees real content.
	extracted := make([]extractedAttachment, 0, len(candidates))
	for _, att := range candidates {
		data, err := deps.GmailClient.GetAttachment(ctx, msg.ID, att.AttachmentID)
		if err != nil || len(data) == 0 {
			continue
		}
		text := strings.TrimSpace(deps.AttachmentExtractFn(ctx, data, att.Filename, att.MimeType))
		if text == "" {
			continue
		}
		extracted = append(extracted, extractedAttachment{att: att, text: text})
	}
	if len(extracted) == 0 {
		return none
	}

	picks := judgeAttachments(ctx, deps, msg, extracted)
	if len(picks) == 0 {
		return none
	}

	return buildAttachmentSelection(extracted, picks)
}

// attachmentCandidates applies the heuristic pre-filter: document/image type and
// above the tiny-inline-bit threshold, capped in count.
func attachmentCandidates(atts []gmail.AttachmentInfo) []gmail.AttachmentInfo {
	out := make([]gmail.AttachmentInfo, 0, len(atts))
	for _, att := range atts {
		if att.Size < minAttachmentSize || att.AttachmentID == "" {
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
	Index      int  `json:"index"`
	Relevant   bool `json:"relevant"`
	DeepReview bool `json:"deep_review"`
}

const attachGateSystem = "당신은 업무 메일 분석을 돕는 첨부 선별기입니다. " +
	"메일 내용에 비추어, 분석에 본문으로 읽을 가치가 있는 첨부만 고릅니다. " +
	"견적서·계약서·세금계산서·거래명세서·발주서·제안서·사양서처럼 업무 판단에 필요한 문서는 relevant=true. " +
	"로고·서명 이미지·약관·홍보물·수신거부 안내·반복 푸터처럼 노이즈는 relevant=false. " +
	"내용이 길고 조밀해 정밀 검토가 필요한 문서는 deep_review=true."

const attachGatePrompt = `메일:
제목: %s
보낸사람: %s
본문 발췌: %s

첨부 후보 (각 index, 파일명, 첫 내용 발췌):
%s

각 첨부를 분석에 읽을지 판단하라. 정확히 다음 JSON만 출력:
{"selections":[{"index":0,"relevant":true,"deep_review":false}]}`

// judgeAttachments asks the local model which extracted attachments to inject.
// Returns the set of selected indices (and their deep-review flag). On any
// failure it returns nil so the caller drops attachments rather than guessing.
func judgeAttachments(ctx context.Context, deps PipelineDeps, msg *gmail.MessageDetail, extracted []extractedAttachment) map[int]bool {
	var sb strings.Builder
	for i, e := range extracted {
		fmt.Fprintf(&sb, "[%d] %s (%s)\n%s\n\n", i, e.att.Filename, e.att.MimeType, clipChars(e.text, attachmentSnippetChars))
	}
	prompt := fmt.Sprintf(attachGatePrompt,
		msg.Subject, msg.From, clipChars(msg.Body, 1200), strings.TrimSpace(sb.String()))

	res, err := callLocalLLMJSON[attachGateResult](ctx, deps.LocalClient, deps.LocalModel, attachGateSystem, prompt, stage1MaxTokens)
	if err != nil {
		return nil
	}
	picks := make(map[int]bool, len(res.Selections))
	for _, s := range res.Selections {
		if s.Relevant && s.Index >= 0 && s.Index < len(extracted) {
			picks[s.Index] = s.DeepReview
		}
	}
	return picks
}

// buildAttachmentSelection renders the injection section from the selected
// attachments, honoring the per-attachment and total character caps.
func buildAttachmentSelection(extracted []extractedAttachment, picks map[int]bool) attachmentSelection {
	var body strings.Builder
	body.WriteString("\n\n## 첨부 내용\n")
	var deep []string
	total := 0
	wrote := false
	for i, e := range extracted {
		deepReview, ok := picks[i]
		if !ok {
			continue
		}
		if deepReview {
			deep = append(deep, e.att.Filename)
		}
		remaining := attachmentInjectTotalChars - total
		if remaining <= 0 {
			break
		}
		limit := attachmentInjectChars
		if limit > remaining {
			limit = remaining
		}
		text := clipChars(e.text, limit)
		fmt.Fprintf(&body, "\n### 📎 %s\n%s\n", e.att.Filename, text)
		total += len(text)
		wrote = true
	}
	if !wrote {
		return attachmentSelection{}
	}
	return attachmentSelection{Injected: body.String(), DeepReview: deep}
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
