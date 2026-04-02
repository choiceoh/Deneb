package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// GmailParams holds parsed input for the gmail tool.
type GmailParams struct {
	Action      string  `json:"action"`
	Query       string  `json:"query"`
	MessageID   string  `json:"message_id"`
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
	MemStore     *memory.Store    // nil = no memory recall in pipeline
	MemEmbed     *memory.Embedder // nil = FTS-only memory search
}

// ToolGmail implements the gmail tool for structured Gmail operations via native API.
func ToolGmail(deps GmailPipelineDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p GmailParams
		if err := jsonutil.UnmarshalInto("gmail params", input, &p); err != nil {
			return "", err
		}

		client, err := gmail.GetClient()
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
		case "send":
			return gmailSend(ctx, client, p)
		case "reply":
			return gmailReply(ctx, client, p)
		case "label":
			return gmailLabel(ctx, client, p)
		case "analyze":
			return gmailAnalyze(ctx, client, deps, p)
		default:
			return fmt.Sprintf("알 수 없는 gmail 액션: %q. 지원: inbox, search, read, send, reply, label, analyze", p.Action), nil
		}
	}
}

// --- inbox: structured inbox summary ---

func gmailInbox(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	max := clampGmailMax(p.Max, 10)

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
		msgs, err := client.Search(ctx, "is:unread", max)
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
	max := clampGmailMax(p.Max, 10)

	msgs, err := client.Search(ctx, p.Query, max)
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
		max := clampGmailMax(p.Max, 5)
		var err error
		messages, err = client.Search(ctx, query, max)
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
		MemStore:    deps.MemStore,
		MemEmbed:    deps.MemEmbed,
		// LocalClient/LocalModel not available in tool context — pipeline
		// will fall back to single-LLM analysis (stage 1 skipped).
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
func clampGmailMax(max, defaultMax int) int {
	if max <= 0 {
		return defaultMax
	}
	if max > 50 {
		return 50
	}
	return max
}
