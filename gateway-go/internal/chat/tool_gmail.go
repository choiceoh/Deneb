package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// gmailParams holds parsed input for the gmail tool.
type gmailParams struct {
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

func gmailToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Gmail action to perform",
				"enum":        []string{"inbox", "search", "read", "send", "reply", "label"},
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Gmail search query (supports Gmail operators like from:, to:, subject:, newer_than:, is:unread, has:attachment, etc.)",
			},
			"message_id": map[string]any{
				"type":        "string",
				"description": "Email message or thread ID (for read, reply, label actions)",
			},
			"to": map[string]any{
				"type":        "string",
				"description": "Recipient email or contact alias (auto-resolved from KV store gmail.contacts.<alias>)",
			},
			"cc": map[string]any{
				"type":        "string",
				"description": "CC recipient(s), comma-separated",
			},
			"bcc": map[string]any{
				"type":        "string",
				"description": "BCC recipient(s), comma-separated",
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "Email subject line",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Email body text (plain text or HTML if html=true)",
			},
			"html": map[string]any{
				"type":        "boolean",
				"description": "Send body as HTML (default: false)",
				"default":     false,
			},
			"max": map[string]any{
				"type":        "number",
				"description": "Maximum results (for inbox/search, default: 10, max: 50)",
				"default":     10,
				"minimum":     1,
				"maximum":     50,
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30, max: 60)",
				"default":     30,
				"minimum":     1,
				"maximum":     60,
			},
			"label_action": map[string]any{
				"type":        "string",
				"description": "Label sub-action (for label action)",
				"enum":        []string{"list", "add", "remove"},
			},
			"label_name": map[string]any{
				"type":        "string",
				"description": "Label name (for label add/remove)",
			},
		},
		"required": []string{"action"},
	}
}

// toolGmail implements the gmail tool for structured Gmail operations via native API.
func toolGmail() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p gmailParams
		if err := jsonutil.UnmarshalInto("gmail params", input, &p); err != nil {
			return "", err
		}

		client, err := gmail.GetClient()
		if err != nil {
			return fmt.Sprintf("Gmail 인증 정보를 찾을 수 없습니다: %s\n~/.deneb/credentials/에 gmail_client.json과 gmail_token.json을 설정하세요.", err), nil
		}

		switch p.Action {
		case "inbox":
			return gmailInbox(client, p)
		case "search":
			return gmailSearch(client, p)
		case "read":
			return gmailRead(client, p)
		case "send":
			return gmailSend(client, p)
		case "reply":
			return gmailReply(client, p)
		case "label":
			return gmailLabel(client, p)
		default:
			return fmt.Sprintf("알 수 없는 gmail 액션: %q. 지원: inbox, search, read, send, reply, label", p.Action), nil
		}
	}
}

// --- inbox: structured inbox summary ---

func gmailInbox(client *gmail.Client, p gmailParams) (string, error) {
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
		msgs, err := client.Search("is:unread", max)
		unreadCh <- result{msgs, err}
	}()
	go func() {
		defer wg.Done()
		msgs, err := client.Search("is:important is:unread", 5)
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

func gmailSearch(client *gmail.Client, p gmailParams) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query는 search 액션에 필수입니다")
	}
	max := clampGmailMax(p.Max, 10)

	msgs, err := client.Search(p.Query, max)
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

func gmailRead(client *gmail.Client, p gmailParams) (string, error) {
	if p.MessageID == "" {
		return "", fmt.Errorf("message_id는 read 액션에 필수입니다")
	}

	msg, err := client.GetMessage(p.MessageID)
	if err != nil {
		return "", err
	}

	return gmail.FormatMessage(msg), nil
}

// --- send: email with contact alias resolution ---

func gmailSend(client *gmail.Client, p gmailParams) (string, error) {
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

	msgID, err := client.Send(to, cc, bcc, p.Subject, p.Body, p.HTML)
	if err != nil {
		return fmt.Sprintf("발송 실패: %s", err), nil
	}

	// Auto-learn contact after successful send.
	learnContact(to)

	return fmt.Sprintf("✉️ 메일 발송 완료 → %s (ID: %s)", to, msgID), nil
}

// --- reply ---

func gmailReply(client *gmail.Client, p gmailParams) (string, error) {
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

	msgID, err := client.Reply(p.MessageID, to, p.Body, p.HTML)
	if err != nil {
		return fmt.Sprintf("답장 실패: %s", err), nil
	}

	return fmt.Sprintf("↩️ 답장 발송 완료 (ID: %s)", msgID), nil
}

// --- label management ---

func gmailLabel(client *gmail.Client, p gmailParams) (string, error) {
	action := p.LabelAction
	if action == "" {
		action = "list"
	}

	switch action {
	case "list":
		labels, err := client.ListLabels()
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
		if err := client.ModifyLabels(p.MessageID, []string{p.LabelName}, nil); err != nil {
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
		if err := client.ModifyLabels(p.MessageID, nil, []string{p.LabelName}); err != nil {
			return fmt.Sprintf("라벨 제거 실패: %s", err), nil
		}
		return fmt.Sprintf("🏷️ 라벨 '%s' 제거 완료", p.LabelName), nil

	default:
		return fmt.Sprintf("알 수 없는 label 액션: %q. 지원: list, add, remove", action), nil
	}
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
		_ = store.set(key, email)
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
