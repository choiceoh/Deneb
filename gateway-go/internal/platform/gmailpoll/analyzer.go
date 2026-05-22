package gmailpoll

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// DefaultPrompt is the default email analysis prompt.
//
// This is the single-email, no-tools analysis path (autonomous Gmail poll).
// It mirrors the email-analysis skill's lens, scoped to what one email allows:
// summary, stakeholders, risk/deadline flags, next steps.
const DefaultPrompt = `다음 이메일을 업무 관점에서 분석해주세요:

1. 핵심 요약 — 발신자가 무엇을 요청/통보하는지 2-3문장
2. 이해관계자 — 핵심 인물과 직책·역할, 의사결정권자
3. 중요도 — 높음/보통/낮음 (판단 근거 한 줄)
4. 리스크·기한 — 결제 기한, 마감일, 금액, 미해결 이슈가 있으면 ⚠️로 표시
5. 다음 단계 — 필요한 조치 1-3개, 가능하면 담당자 명시

간결하게 핵심만, 한국어로 작성해주세요.`

const analysisSystemPrompt = "당신은 업무 메일 분석 어시스턴트입니다. 제공된 이메일을 업무 관점에서 분석합니다 — 맥락, 이해관계자, 리스크·기한, 다음 단계."

const (
	llmMaxTokens = 1024
	maxBodyChars = 8000
)

// loadPrompt reads the analysis prompt from the configured file path.
// Falls back to the default prompt if the file doesn't exist.
func loadPrompt(promptFile string) string {
	if promptFile == "" {
		return DefaultPrompt
	}

	// Expand ~ to home directory.
	if strings.HasPrefix(promptFile, "~/") {
		home, _ := os.UserHomeDir()
		promptFile = home + promptFile[1:]
	}

	data, err := os.ReadFile(promptFile)
	if err != nil {
		return DefaultPrompt
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return DefaultPrompt
	}
	return content
}

// FormatEmailForAnalysis builds the user message from email details.
func FormatEmailForAnalysis(msg *gmail.MessageDetail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\n", msg.From)
	fmt.Fprintf(&sb, "To: %s\n", msg.To)
	if msg.CC != "" {
		fmt.Fprintf(&sb, "CC: %s\n", msg.CC)
	}
	fmt.Fprintf(&sb, "Subject: %s\n", msg.Subject)
	fmt.Fprintf(&sb, "Date: %s\n", msg.Date)
	sb.WriteString("\n--- 본문 ---\n")

	body := msg.Body
	// Truncate very long bodies to keep within LLM context.
	if len(body) > maxBodyChars {
		body = body[:maxBodyChars] + "\n\n... (본문 생략)"
	}
	sb.WriteString(body)

	return sb.String()
}

// AnalyzeEmail sends an email to the LLM for analysis and returns the result.
func AnalyzeEmail(ctx context.Context, client *llm.Client, model, prompt string, msg *gmail.MessageDetail) (string, error) {
	userContent := prompt + "\n\n" + FormatEmailForAnalysis(msg)

	req := llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", userContent)},
		System:    llm.SystemString(analysisSystemPrompt),
		MaxTokens: llmMaxTokens,
		Stream:    true,
	}

	events, err := client.StreamChat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("LLM 호출 실패: %w", err)
	}

	var sb strings.Builder
	for ev := range events {
		if ctx.Err() != nil {
			break
		}
		switch ev.Type {
		case "content_block_delta":
			var delta llm.ContentBlockDelta
			if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
				sb.WriteString(delta.Delta.Text)
			}
		case "error":
			var errInfo struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(ev.Payload, &errInfo) == nil && errInfo.Message != "" {
				return "", fmt.Errorf("LLM 스트림 오류: %s", errInfo.Message)
			}
			return "", fmt.Errorf("LLM 스트림 오류")
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "", fmt.Errorf("LLM 응답이 비어있습니다")
	}
	return result, nil
}

// formatReport builds the Telegram notification message for an analyzed email.
// Uses HTML formatting for Telegram parse mode.
func formatReport(msg *gmail.MessageDetail, analysis string) string {
	var sb strings.Builder
	sb.WriteString("📬 <b>새 메일 분석</b>\n\n")
	fmt.Fprintf(&sb, "<b>From:</b> %s\n", html.EscapeString(msg.From))
	fmt.Fprintf(&sb, "<b>Subject:</b> %s\n", html.EscapeString(msg.Subject))
	sb.WriteString("\n")
	sb.WriteString(html.EscapeString(analysis))
	return sb.String()
}
