package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// --- attachment: fetch + extract email attachments (PDF text, etc.) ---

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

// extractAttachmentText turns raw attachment bytes into text the model can
// read: PDFs go through pdftotext, text-like files are returned directly, and
// anything else reports metadata only.
func extractAttachmentText(ctx context.Context, att *gmail.AttachmentInfo, data []byte) string {
	lower := strings.ToLower(att.Filename)
	isPDF := strings.Contains(strings.ToLower(att.MimeType), "pdf") || strings.HasSuffix(lower, ".pdf")

	switch {
	case isPDF:
		text, err := pdfToText(ctx, data)
		if err != nil {
			return fmt.Sprintf("📎 %s (PDF, %s)\n\n⚠️ PDF 텍스트 추출 실패: %s", att.Filename, formatBytes(int64(att.Size)), err)
		}
		return fmt.Sprintf("## 📎 %s (PDF)\n\n%s", att.Filename, truncate(text, attachmentTextLimit))
	case strings.HasPrefix(att.MimeType, "text/") || isTextFile(lower):
		return fmt.Sprintf("## 📎 %s\n\n%s", att.Filename, truncate(string(data), attachmentTextLimit))
	default:
		return fmt.Sprintf("📎 %s (%s, %s) — 텍스트로 추출할 수 없는 형식입니다.", att.Filename, att.MimeType, formatBytes(int64(att.Size)))
	}
}

// pdfToText extracts text from PDF bytes via the `pdftotext` CLI (poppler).
// The PDF is piped through stdin so no temp file is needed.
func pdfToText(ctx context.Context, pdf []byte) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("pdftotext 미설치 — DGX Spark에서 `apt install poppler-utils` 실행 필요")
	}

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// `pdftotext -layout - -` reads the PDF from stdin, writes text to stdout.
	cmd := exec.CommandContext(runCtx, "pdftotext", "-layout", "-", "-")
	cmd.Stdin = bytes.NewReader(pdf)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}

	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("추출된 텍스트가 없습니다 (스캔본 PDF일 수 있음 — OCR 필요)")
	}
	return text, nil
}

// isTextFile reports whether a filename has a plain-text extension.
func isTextFile(lowerName string) bool {
	for _, ext := range []string{".txt", ".csv", ".md", ".json", ".xml", ".log", ".yaml", ".yml"} {
		if strings.HasSuffix(lowerName, ext) {
			return true
		}
	}
	return false
}
