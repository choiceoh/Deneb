package gmailpoll

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const defaultPrompt = `다음 이메일을 분석하여 간결하게 요약해주세요:
1. 발신자와 주요 내용 요약 (2-3문장)
2. 중요도 판단 (높음/보통/낮음)
3. 필요한 조치 사항이 있다면 명시

간결하고 핵심만 전달해주세요.`

const analysisSystemPrompt = "당신은 이메일 분석 어시스턴트입니다. 사용자가 제공하는 이메일을 분석하고 요약합니다."

const (
	llmMaxTokens = 1024
	llmTimeout   = 60 // seconds
)

// loadPrompt reads the analysis prompt from the configured file path.
// Falls back to the default prompt if the file doesn't exist.
func loadPrompt(promptFile string) string {
	if promptFile == "" {
		return defaultPrompt
	}

	// Expand ~ to home directory.
	if strings.HasPrefix(promptFile, "~/") {
		home, _ := os.UserHomeDir()
		promptFile = home + promptFile[1:]
	}

	data, err := os.ReadFile(promptFile)
	if err != nil {
		return defaultPrompt
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return defaultPrompt
	}
	return content
}

// formatEmailForAnalysis builds the user message from email details.
func formatEmailForAnalysis(msg *gmail.MessageDetail) string {
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
	if len(body) > 8000 {
		body = body[:8000] + "\n\n... (본문 생략)"
	}
	sb.WriteString(body)

	return sb.String()
}

// analyzeEmail sends an email to the LLM for analysis and returns the result.
func analyzeEmail(ctx context.Context, client *llm.Client, model, prompt string, msg *gmail.MessageDetail) (string, error) {
	userContent := prompt + "\n\n" + formatEmailForAnalysis(msg)

	req := llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", userContent)},
		System:    llm.SystemString(analysisSystemPrompt),
		MaxTokens: llmMaxTokens,
		Stream:    true,
	}

	events, err := client.StreamChatOpenAI(ctx, req)
	if err != nil {
		return "", fmt.Errorf("LLM 호출 실패: %w", err)
	}

	var sb strings.Builder
	for ev := range events {
		if ctx.Err() != nil {
			break
		}
		if ev.Type == "content_block_delta" {
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.Payload, &delta) == nil {
				sb.WriteString(delta.Delta.Text)
			}
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "", fmt.Errorf("LLM 응답이 비어있습니다")
	}
	return result, nil
}

// formatReport builds the Telegram notification message for an analyzed email.
func formatReport(msg *gmail.MessageDetail, analysis string) string {
	var sb strings.Builder
	sb.WriteString("📬 새 메일 분석\n\n")
	fmt.Fprintf(&sb, "From: %s\n", msg.From)
	fmt.Fprintf(&sb, "Subject: %s\n", msg.Subject)
	sb.WriteString("\n")
	sb.WriteString(analysis)
	return sb.String()
}
