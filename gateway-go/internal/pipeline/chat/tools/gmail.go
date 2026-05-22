package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// GmailParams holds parsed input for the gmail tool.
type GmailParams struct {
	Action      string  `json:"action"`
	Query       string  `json:"query"`
	MessageID   string  `json:"message_id"`
	ThreadID    string  `json:"thread_id"`
	Attachment  string  `json:"attachment"`
	To          string  `json:"to"`
	CC          string  `json:"cc"`
	BCC         string  `json:"bcc"`
	Subject     string  `json:"subject"`
	Body        string  `json:"body"`
	HTML        bool    `json:"html"`
	Max         int     `json:"max"`
	Timeout     float64 `json:"timeout"`
	LabelAction string  `json:"label_action"`
	LabelName   string  `json:"label_name"`
}

// GmailPipelineDeps holds dependencies for the gmail tool including pipeline support.
type GmailPipelineDeps struct {
	LLMClient    *llm.Client
	DefaultModel string
}

// ToolGmail implements the gmail tool for structured Gmail operations via native API.
func ToolGmail(deps GmailPipelineDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p GmailParams
		if err := jsonutil.UnmarshalInto("gmail params", input, &p); err != nil {
			return "", err
		}

		client, err := gmail.DefaultClient()
		if err != nil {
			return fmt.Sprintf("Gmail 인증 정보를 찾을 수 없습니다: %s\n~/.deneb/credentials/에 gmail_client.json과 gmail_token.json을 설정하세요.", err), nil
		}

		switch p.Action {
		case "inbox":
			return gmailInbox(ctx, client, p)
		case "search":
			return gmailSearch(ctx, client, p)
		case "read":
			return gmailRead(ctx, client, p)
		case "thread":
			return gmailThread(ctx, client, p)
		case "attachment":
			return gmailAttachment(ctx, client, p)
		case "send":
			return gmailSend(ctx, client, p)
		case "reply":
			return gmailReply(ctx, client, p)
		case "label":
			return gmailLabel(ctx, client, p)
		case "analyze":
			return gmailAnalyze(ctx, client, deps, p)
		default:
			return fmt.Sprintf("알 수 없는 gmail 액션: %q. 지원: inbox, search, read, thread, attachment, send, reply, label, analyze", p.Action), nil
		}
	}
}

// --- inbox: structured inbox summary ---

func gmailInbox(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	maxResults := clampGmailMax(p.Max, 10)

	type result struct {
		msgs []gmail.MessageSummary
		err  error
	}
	var wg sync.WaitGroup
	unreadCh := make(chan result, 1)
	importantCh := make(chan result, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		msgs, err := client.Search(ctx, "is:unread", maxResults)
		unreadCh <- result{msgs, err}
	}()
	go func() {
		defer wg.Done()
		msgs, err := client.Search(ctx, "is:important is:unread", 5)
		importantCh <- result{msgs, err}
	}()
	wg.Wait()

	unread := <-unreadCh
	important := <-importantCh

	var sb strings.Builder
	sb.WriteString("## 📬 받은편지함 요약\n\n")

	if unread.err != nil {
		fmt.Fprintf(&sb, "안 읽은 메일 조회 실패: %s\n\n", unread.err)
	} else {
		count := len(unread.msgs)
		fmt.Fprintf(&sb, "### 안 읽은 메일 (%d건)\n\n", count)
		if count > 0 {
			sb.WriteString(gmail.FormatSearchResults(unread.msgs))
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("안 읽은 메일이 없습니다.\n\n")
		}
	}

	if important.err != nil {
		fmt.Fprintf(&sb, "중요 메일 조회 실패: %s\n\n", important.err)
	} else if len(important.msgs) > 0 {
		sb.WriteString("### ⭐ 중요 + 안 읽은 메일\n\n")
		sb.WriteString(gmail.FormatSearchResults(important.msgs))
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// --- search: structured search results ---

func gmailSearch(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query는 search 액션에 필수입니다")
	}
	maxResults := clampGmailMax(p.Max, 10)

	msgs, err := client.Search(ctx, p.Query, maxResults)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return fmt.Sprintf("검색 결과 없음: %q", p.Query), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## 검색 결과: %s\n\n", p.Query)
	sb.WriteString(gmail.FormatSearchResults(msgs))
	return sb.String(), nil
}

// --- read: structured email with metadata separation ---

func gmailRead(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	if p.MessageID == "" {
		return "", fmt.Errorf("message_id는 read 액션에 필수입니다")
	}

	msg, err := client.GetMessage(ctx, p.MessageID)
	if err != nil {
		return "", err
	}

	return gmail.FormatMessage(msg), nil
}

// --- thread: full conversation thread for timeline reconstruction ---

func gmailThread(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	threadID := p.ThreadID
	if threadID == "" {
		if p.MessageID == "" {
			return "", fmt.Errorf("message_id 또는 thread_id는 thread 액션에 필수입니다")
		}
		// Resolve the thread that contains the given message.
		msg, err := client.GetMessage(ctx, p.MessageID)
		if err != nil {
			return "", err
		}
		threadID = msg.ThreadID
	}

	msgs, err := client.GetThread(ctx, threadID)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "스레드에 메시지가 없습니다.", nil
	}

	// Keep the most recent N messages to bound context size.
	limit := clampGmailMax(p.Max, 10)
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## 🧵 스레드 (%d개 메시지, 오래된 순)\n\n", len(msgs))
	for i, m := range msgs {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&sb, "**[%d] From:** %s\n", i+1, m.From)
		fmt.Fprintf(&sb, "**Date:** %s\n", m.Date)
		fmt.Fprintf(&sb, "**Subject:** %s\n\n", m.Subject)
		sb.WriteString(truncateBody(m.Body, 1500))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// truncateBody caps a message body to maxRunes for thread display,
// staying on a rune boundary so Korean text is never split mid-character.
func truncateBody(body string, maxRunes int) string {
	body = strings.TrimSpace(body)
	r := []rune(body)
	if len(r) <= maxRunes {
		return body
	}
	return string(r[:maxRunes]) + "\n... (본문 생략)"
}

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

// --- send: email with contact alias resolution ---

func gmailSend(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	if p.To == "" {
		return "", fmt.Errorf("to는 send 액션에 필수입니다")
	}
	if p.Subject == "" {
		return "", fmt.Errorf("subject는 send 액션에 필수입니다")
	}
	if p.Body == "" {
		return "", fmt.Errorf("body는 send 액션에 필수입니다")
	}

	to := resolveRecipient(p.To)
	cc := ""
	if p.CC != "" {
		cc = resolveRecipients(p.CC)
	}
	bcc := ""
	if p.BCC != "" {
		bcc = resolveRecipients(p.BCC)
	}

	msgID, err := client.Send(ctx, to, cc, bcc, p.Subject, p.Body, p.HTML)
	if err != nil {
		return fmt.Sprintf("발송 실패: %s", err), nil
	}

	// Auto-learn contact after successful send.
	learnContact(to)

	return fmt.Sprintf("✉️ 메일 발송 완료 → %s (ID: %s)", to, msgID), nil
}

// --- reply ---

func gmailReply(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	if p.MessageID == "" {
		return "", fmt.Errorf("message_id는 reply 액션에 필수입니다")
	}
	if p.Body == "" {
		return "", fmt.Errorf("body는 reply 액션에 필수입니다")
	}

	to := ""
	if p.To != "" {
		to = resolveRecipient(p.To)
	}

	msgID, err := client.Reply(ctx, p.MessageID, to, p.Body, p.HTML)
	if err != nil {
		return fmt.Sprintf("답장 실패: %s", err), nil
	}

	return fmt.Sprintf("↩️ 답장 발송 완료 (ID: %s)", msgID), nil
}

// --- label management ---

func gmailLabel(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	action := p.LabelAction
	if action == "" {
		action = "list"
	}

	switch action {
	case "list":
		labels, err := client.ListLabels(ctx)
		if err != nil {
			return "", err
		}
		if len(labels) == 0 {
			return "라벨 없음", nil
		}
		return fmt.Sprintf("## 라벨 목록\n\n%s", gmail.FormatLabels(labels)), nil

	case "add":
		if p.MessageID == "" {
			return "", fmt.Errorf("message_id는 label add에 필수입니다")
		}
		if p.LabelName == "" {
			return "", fmt.Errorf("label_name은 label add에 필수입니다")
		}
		if err := client.ModifyLabels(ctx, p.MessageID, []string{p.LabelName}, nil); err != nil {
			return fmt.Sprintf("라벨 추가 실패: %s", err), nil
		}
		return fmt.Sprintf("🏷️ 라벨 '%s' 추가 완료", p.LabelName), nil

	case "remove":
		if p.MessageID == "" {
			return "", fmt.Errorf("message_id는 label remove에 필수입니다")
		}
		if p.LabelName == "" {
			return "", fmt.Errorf("label_name은 label remove에 필수입니다")
		}
		if err := client.ModifyLabels(ctx, p.MessageID, nil, []string{p.LabelName}); err != nil {
			return fmt.Sprintf("라벨 제거 실패: %s", err), nil
		}
		return fmt.Sprintf("🏷️ 라벨 '%s' 제거 완료", p.LabelName), nil

	default:
		return fmt.Sprintf("알 수 없는 label 액션: %q. 지원: list, add, remove", action), nil
	}
}

// --- analyze: LLM-based email analysis ---

func gmailAnalyze(ctx context.Context, client *gmail.Client, deps GmailPipelineDeps, p GmailParams) (string, error) {
	if deps.LLMClient == nil {
		return "LLM 클라이언트가 설정되지 않았습니다.", nil
	}

	// Determine which emails to analyze.
	var messages []gmail.MessageSummary
	if p.MessageID != "" {
		messages = []gmail.MessageSummary{{ID: p.MessageID}}
	} else {
		query := p.Query
		if query == "" {
			query = "is:unread newer_than:1h"
		}
		maxResults := clampGmailMax(p.Max, 5)
		var err error
		messages, err = client.Search(ctx, query, maxResults)
		if err != nil {
			return "", fmt.Errorf("메일 검색 실패: %w", err)
		}
		if len(messages) == 0 {
			return "분석할 메일이 없습니다.", nil
		}
	}

	// Build pipeline deps.
	pipeDeps := gmailpoll.PipelineDeps{
		GmailClient: client,
		LLMClient:   deps.LLMClient,
		MainModel:   deps.DefaultModel,
	}

	var sb strings.Builder
	for i, summary := range messages {
		detail, err := client.GetMessage(ctx, summary.ID)
		if err != nil {
			fmt.Fprintf(&sb, "⚠️ 메일 조회 실패 (ID: %s): %s\n\n", summary.ID, err)
			continue
		}

		analysis, err := gmailpoll.AnalyzeEmailPipeline(ctx, pipeDeps, detail)
		if err != nil {
			fmt.Fprintf(&sb, "⚠️ 분석 실패 (%s): %s\n\n", detail.Subject, err)
			continue
		}

		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&sb, "## 📬 %s\n", detail.Subject)
		fmt.Fprintf(&sb, "**From:** %s\n", detail.From)
		fmt.Fprintf(&sb, "**Date:** %s\n\n", detail.Date)
		sb.WriteString(analysis)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// --- helpers ---

// resolveRecipient resolves a contact alias to an email address via KV store.
// If the input already contains '@', it is returned as-is.
func resolveRecipient(to string) string {
	if strings.Contains(to, "@") {
		return to
	}
	store := getKVStore()
	key := "gmail.contacts." + strings.ToLower(strings.TrimSpace(to))
	if email, ok := store.get(key); ok && email != "" {
		return email
	}
	return to
}

// resolveRecipients resolves comma-separated recipients.
func resolveRecipients(list string) string {
	parts := strings.Split(list, ",")
	resolved := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			resolved = append(resolved, resolveRecipient(p))
		}
	}
	return strings.Join(resolved, ",")
}

// learnContact stores a recipient email in the KV store for future alias resolution.
func learnContact(email string) {
	if !strings.Contains(email, "@") {
		return
	}
	store := getKVStore()
	local := strings.ToLower(strings.SplitN(email, "@", 2)[0])
	key := "gmail.contacts." + local
	if _, ok := store.get(key); !ok {
		if err := store.set(key, email); err != nil {
			slog.Warn("gmail: failed to cache contact", "email", email, "err", err)
		}
	}
}

// clampGmailMax returns the max value clamped to [1, 50] with a given default.
func clampGmailMax(n, defaultMax int) int {
	if n <= 0 {
		return defaultMax
	}
	if n > 50 {
		return 50
	}
	return n
}
