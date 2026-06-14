package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// --- attachment: fetch + extract email attachments (PDF/Excel/Word/PowerPoint/image/text) ---
//
// This file owns Gmail-specific orchestration only: listing a message's
// attachments, resolving a selector, downloading bytes, and saving to disk for
// send_file. The actual byte→text extraction is delegated to the shared
// document dispatcher (extractDocument in document_extract.go), which routes to
// the per-format parsers in docparse.go. extractAttachmentText only adds the
// Gmail-facing Korean headers and error strings on top.

// attachmentTextLimit caps extracted attachment text (runes) so a large
// document never blows the model's context budget.
const attachmentTextLimit = 50000

func gmailAttachment(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	if p.MessageID == "" {
		return "", fmt.Errorf("message_id는 attachment 액션에 필수입니다")
	}

	msg, err := client.GetMessage(ctx, p.MessageID)
	if err != nil {
		return "", err
	}
	if len(msg.Attachments) == 0 {
		return "이 메일에는 첨부파일이 없습니다.", nil
	}

	// No selector → list the attachments.
	if strings.TrimSpace(p.Attachment) == "" {
		var sb strings.Builder
		fmt.Fprintf(&sb, "## 📎 첨부파일 (%d개)\n\n", len(msg.Attachments))
		for i, a := range msg.Attachments {
			fmt.Fprintf(&sb, "%d. %s — %s, %s\n", i+1, a.Filename, a.MimeType, formatBytes(int64(a.Size)))
		}
		sb.WriteString("\n내용을 보려면 attachment에 파일명 또는 번호를 지정하세요.")
		return sb.String(), nil
	}

	att := resolveAttachment(msg.Attachments, p.Attachment)
	if att == nil {
		return fmt.Sprintf("첨부파일 %q를 찾을 수 없습니다. attachment 인자 없이 호출하면 목록을 봅니다.", p.Attachment), nil
	}
	if att.AttachmentID == "" {
		return fmt.Sprintf("'%s'는 인라인 첨부라 별도 추출이 지원되지 않습니다.", att.Filename), nil
	}

	data, err := client.GetAttachment(ctx, p.MessageID, att.AttachmentID)
	if err != nil {
		return "", fmt.Errorf("첨부파일 다운로드 실패: %w", err)
	}

	// download mode → save to disk and hand the path back for send_file.
	if p.Download {
		path, err := saveAttachmentToDisk(att.Filename, data)
		if err != nil {
			return "", fmt.Errorf("첨부파일 저장 실패: %w", err)
		}
		return fmt.Sprintf("📎 첨부파일을 저장했습니다: `%s` (%s)\nsend_file 도구의 file_path 인자에 이 경로를 넘기면 사용자에게 전달됩니다.",
			path, formatBytes(int64(len(data)))), nil
	}

	return extractAttachmentText(ctx, att, data), nil
}

// resolveAttachment picks an attachment by 1-based index or by filename
// (exact match first, then case-insensitive substring).
func resolveAttachment(atts []gmail.AttachmentInfo, sel string) *gmail.AttachmentInfo {
	sel = strings.TrimSpace(sel)
	if idx, err := strconv.Atoi(sel); err == nil && idx >= 1 && idx <= len(atts) {
		return &atts[idx-1]
	}
	for i := range atts {
		if atts[i].Filename == sel {
			return &atts[i]
		}
	}
	lower := strings.ToLower(sel)
	for i := range atts {
		if strings.Contains(strings.ToLower(atts[i].Filename), lower) {
			return &atts[i]
		}
	}
	return nil
}

// extractAttachmentText turns raw attachment bytes into text the model can read,
// dispatching through the shared extractor (document_extract.go) and dressing the
// result with the Gmail-facing Korean header/error strings. The format coverage —
// PDFs (with a scanned-PDF OCR fallback), Excel/Word/PowerPoint via OOXML readers,
// images via OCR, CSV/text inline, everything else metadata only — lives in the
// shared dispatcher, so the Gmail, Dropbox, and web-fetch paths stay in lockstep.
func extractAttachmentText(ctx context.Context, att *gmail.AttachmentInfo, data []byte) string {
	r := extractDocument(ctx, data, att.Filename, att.MimeType)
	switch r.kind {
	case docPDF:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (PDF, %s)\n\n⚠️ PDF 텍스트 추출 실패: %s", att.Filename, formatBytes(int64(att.Size)), r.err)
		}
		label := "PDF"
		if r.ocr {
			label = "PDF, OCR"
		}
		return fmt.Sprintf("## 📎 %s (%s)\n\n%s", att.Filename, label, truncate(r.text, attachmentTextLimit))
	case docXLSX:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (Excel, %s)\n\n⚠️ 엑셀 읽기 실패: %s", att.Filename, formatBytes(int64(att.Size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (Excel)\n\n%s", att.Filename, truncate(r.text, attachmentTextLimit))
	case docDOCX:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (Word, %s)\n\n⚠️ Word 읽기 실패: %s", att.Filename, formatBytes(int64(att.Size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (Word)\n\n%s", att.Filename, truncate(r.text, attachmentTextLimit))
	case docPPTX:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (PowerPoint, %s)\n\n⚠️ PowerPoint 읽기 실패: %s", att.Filename, formatBytes(int64(att.Size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (PowerPoint)\n\n%s", att.Filename, truncate(r.text, attachmentTextLimit))
	case docImage:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (이미지, %s)\n\n⚠️ 이미지 OCR 실패: %s", att.Filename, formatBytes(int64(att.Size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (이미지 OCR)\n\n%s", att.Filename, truncate(r.text, attachmentTextLimit))
	case docCSV:
		// csvToMarkdown only fails on an empty CSV; fall back to the raw bytes so
		// the model still sees the content, matching the prior behavior.
		body := r.text
		if r.err != nil {
			body = string(data)
		}
		return fmt.Sprintf("## 📎 %s (CSV)\n\n%s", att.Filename, truncate(body, attachmentTextLimit))
	case docText:
		return fmt.Sprintf("## 📎 %s\n\n%s", att.Filename, truncate(r.text, attachmentTextLimit))
	default:
		return fmt.Sprintf("📎 %s (%s, %s) — 텍스트로 추출할 수 없는 형식입니다.", att.Filename, att.MimeType, formatBytes(int64(att.Size)))
	}
}

// appendAttachmentText fetches and extracts every attachment of a message and
// appends the text to detail.Body, so the analyze pipeline — which reads
// detail.Body — sees contract/invoice content, not just the cover note.
func appendAttachmentText(ctx context.Context, client *gmail.Client, detail *gmail.MessageDetail) {
	if detail == nil || len(detail.Attachments) == 0 {
		return
	}

	const maxAttachments = 10
	var sb strings.Builder
	for i := range detail.Attachments {
		if i >= maxAttachments {
			break
		}
		att := detail.Attachments[i]
		if att.AttachmentID == "" {
			continue
		}
		data, err := client.GetAttachment(ctx, detail.ID, att.AttachmentID)
		if err != nil {
			continue
		}
		sb.WriteString("\n\n")
		sb.WriteString(extractAttachmentText(ctx, &att, data))
	}
	if sb.Len() == 0 {
		return
	}
	detail.Body += "\n\n--- 첨부파일 내용 ---" + truncate(sb.String(), 80000)
}

// saveAttachmentToDisk writes attachment bytes to a temp file so the agent can
// hand the path to the send_file tool. The filename is sanitized to its base
// component to prevent path traversal.
func saveAttachmentToDisk(filename string, data []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "deneb-gmail-attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
