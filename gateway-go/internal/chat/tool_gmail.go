package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
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

// toolGmail implements the gmail tool for structured Gmail operations via gog CLI.
func toolGmail() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p gmailParams
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid gmail params: %w", err)
		}

		if _, err := exec.LookPath("gog"); err != nil {
			return "gog CLI를 찾을 수 없습니다. Gmail 도구를 사용하려면 gog를 설치하세요.", nil
		}

		timeout := resolveGmailTimeout(p.Timeout)

		switch p.Action {
		case "inbox":
			return gmailInbox(ctx, p, timeout)
		case "search":
			return gmailSearch(ctx, p, timeout)
		case "read":
			return gmailRead(ctx, p, timeout)
		case "send":
			return gmailSend(ctx, p, timeout)
		case "reply":
			return gmailReply(ctx, p, timeout)
		case "label":
			return gmailLabel(ctx, p, timeout)
		default:
			return fmt.Sprintf("알 수 없는 gmail 액션: %q. 지원: inbox, search, read, send, reply, label", p.Action), nil
		}
	}
}

// resolveGmailTimeout returns a clamped duration from the user-supplied seconds.
func resolveGmailTimeout(secs float64) time.Duration {
	if secs <= 0 {
		return 30 * time.Second
	}
	d := time.Duration(secs * float64(time.Second))
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// --- inbox: structured inbox summary ---

func gmailInbox(ctx context.Context, p gmailParams, timeout time.Duration) (string, error) {
	max := clampGmailMax(p.Max, 10)

	// Run unread + important searches in parallel.
	type result struct {
		output string
		err    error
	}
	var wg sync.WaitGroup
	unreadCh := make(chan result, 1)
	importantCh := make(chan result, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		out, err := runGog(ctx, timeout, "", "gmail", "search", "is:unread", "--max", strconv.Itoa(max))
		unreadCh <- result{out, err}
	}()
	go func() {
		defer wg.Done()
		out, err := runGog(ctx, timeout, "", "gmail", "search", "is:important is:unread", "--max", "5")
		importantCh <- result{out, err}
	}()
	wg.Wait()

	unread := <-unreadCh
	important := <-importantCh

	var sb strings.Builder
	sb.WriteString("## 📬 받은편지함 요약\n\n")

	// Unread section.
	if unread.err != nil {
		fmt.Fprintf(&sb, "안 읽은 메일 조회 실패: %s\n\n", unread.err)
	} else {
		lines := countNonEmptyLines(unread.output)
		fmt.Fprintf(&sb, "### 안 읽은 메일 (%d건)\n\n", lines)
		if unread.output != "" {
			sb.WriteString(unread.output)
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("안 읽은 메일이 없습니다.\n\n")
		}
	}

	// Important section.
	if important.err != nil {
		fmt.Fprintf(&sb, "중요 메일 조회 실패: %s\n\n", important.err)
	} else if important.output != "" {
		sb.WriteString("### ⭐ 중요 + 안 읽은 메일\n\n")
		sb.WriteString(important.output)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// --- search: structured search results ---

func gmailSearch(ctx context.Context, p gmailParams, timeout time.Duration) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query는 search 액션에 필수입니다")
	}
	max := clampGmailMax(p.Max, 10)

	out, err := runGog(ctx, timeout, "", "gmail", "search", p.Query, "--max", strconv.Itoa(max))
	if err != nil {
		return "", err
	}
	if out == "" {
		return fmt.Sprintf("검색 결과 없음: %q", p.Query), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## 검색 결과: %s\n\n", p.Query)
	sb.WriteString(out)
	return sb.String(), nil
}

// --- read: structured email with metadata separation ---

func gmailRead(ctx context.Context, p gmailParams, timeout time.Duration) (string, error) {
	if p.MessageID == "" {
		return "", fmt.Errorf("message_id는 read 액션에 필수입니다")
	}

	out, err := runGog(ctx, timeout, "", "gmail", "messages", "get", p.MessageID)
	if err != nil {
		return "", err
	}
	if out == "" {
		return fmt.Sprintf("메일을 찾을 수 없음: %s", p.MessageID), nil
	}

	return out, nil
}

// --- send: email with contact alias resolution ---

func gmailSend(ctx context.Context, p gmailParams, timeout time.Duration) (string, error) {
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

	args := []string{"gmail", "send", "--to", to, "--subject", p.Subject}

	if p.CC != "" {
		args = append(args, "--cc", resolveRecipients(p.CC))
	}
	if p.BCC != "" {
		args = append(args, "--bcc", resolveRecipients(p.BCC))
	}

	// Use stdin for body to handle multi-line safely.
	if p.HTML {
		args = append(args, "--body-html", p.Body)
	} else {
		args = append(args, "--body-file", "-")
	}

	stdin := ""
	if !p.HTML {
		stdin = p.Body
	}

	out, err := runGog(ctx, timeout, stdin, args...)
	if err != nil {
		return fmt.Sprintf("발송 실패: %s\n%s", err, out), nil
	}

	// Auto-learn contact after successful send.
	learnContact(to)

	if out == "" {
		return fmt.Sprintf("✉️ 메일 발송 완료 → %s", to), nil
	}
	return fmt.Sprintf("✉️ 메일 발송 완료 → %s\n%s", to, out), nil
}

// --- reply ---

func gmailReply(ctx context.Context, p gmailParams, timeout time.Duration) (string, error) {
	if p.MessageID == "" {
		return "", fmt.Errorf("message_id는 reply 액션에 필수입니다")
	}
	if p.Body == "" {
		return "", fmt.Errorf("body는 reply 액션에 필수입니다")
	}

	args := []string{"gmail", "send", "--reply-to-message-id", p.MessageID}

	if p.To != "" {
		args = append(args, "--to", resolveRecipient(p.To))
	}

	if p.HTML {
		args = append(args, "--body-html", p.Body)
	} else {
		args = append(args, "--body-file", "-")
	}

	stdin := ""
	if !p.HTML {
		stdin = p.Body
	}

	out, err := runGog(ctx, timeout, stdin, args...)
	if err != nil {
		return fmt.Sprintf("답장 실패: %s\n%s", err, out), nil
	}

	if out == "" {
		return "↩️ 답장 발송 완료", nil
	}
	return fmt.Sprintf("↩️ 답장 발송 완료\n%s", out), nil
}

// --- label management ---

func gmailLabel(ctx context.Context, p gmailParams, timeout time.Duration) (string, error) {
	action := p.LabelAction
	if action == "" {
		action = "list"
	}

	switch action {
	case "list":
		out, err := runGog(ctx, timeout, "", "gmail", "labels", "list")
		if err != nil {
			return "", err
		}
		if out == "" {
			return "라벨 없음", nil
		}
		return fmt.Sprintf("## 라벨 목록\n\n%s", out), nil

	case "add":
		if p.MessageID == "" {
			return "", fmt.Errorf("message_id는 label add에 필수입니다")
		}
		if p.LabelName == "" {
			return "", fmt.Errorf("label_name은 label add에 필수입니다")
		}
		out, err := runGog(ctx, timeout, "", "gmail", "labels", "add", p.MessageID, p.LabelName)
		if err != nil {
			return fmt.Sprintf("라벨 추가 실패: %s\n%s", err, out), nil
		}
		return fmt.Sprintf("🏷️ 라벨 '%s' 추가 완료", p.LabelName), nil

	case "remove":
		if p.MessageID == "" {
			return "", fmt.Errorf("message_id는 label remove에 필수입니다")
		}
		if p.LabelName == "" {
			return "", fmt.Errorf("label_name은 label remove에 필수입니다")
		}
		out, err := runGog(ctx, timeout, "", "gmail", "labels", "remove", p.MessageID, p.LabelName)
		if err != nil {
			return fmt.Sprintf("라벨 제거 실패: %s\n%s", err, out), nil
		}
		return fmt.Sprintf("🏷️ 라벨 '%s' 제거 완료", p.LabelName), nil

	default:
		return fmt.Sprintf("알 수 없는 label 액션: %q. 지원: list, add, remove", action), nil
	}
}

// --- helpers ---

// runGog executes a gog CLI command and returns its combined output.
// If stdinData is non-empty, it is piped to the command's stdin.
func runGog(ctx context.Context, timeout time.Duration, stdinData string, args ...string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "gog", args...)
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}

	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))

	// Cap output at 32K chars.
	const maxOutput = 32000
	if len(result) > maxOutput {
		result = result[:maxOutput] + "\n\n[... 출력 생략됨]"
	}

	if err != nil {
		if result != "" {
			return result, fmt.Errorf("gog 실행 실패: %w", err)
		}
		return "", fmt.Errorf("gog 실행 실패: %w", err)
	}
	return result, nil
}

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
	// Return as-is; gog will report the error if it's not a valid address.
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
// Extracts the local part of the email as the alias key.
func learnContact(email string) {
	if !strings.Contains(email, "@") {
		return
	}
	store := getKVStore()
	local := strings.ToLower(strings.SplitN(email, "@", 2)[0])
	key := "gmail.contacts." + local
	// Only store if not already present (don't overwrite manual aliases).
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

// countNonEmptyLines counts non-empty lines in a string.
func countNonEmptyLines(s string) int {
	if s == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
