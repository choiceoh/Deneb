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
// It mirrors the email-analysis skill's *stance* (not a fixed form): the email
// decides what leads and how deep to go. The lens — why now, stakeholders,
// risk/deadline, next steps — is offered as things worth considering, not as
// mandatory sections to fill in order.
const DefaultPrompt = `다음 이메일을 업무 관점에서 깊이 읽어주세요. 이건 채워야 할
양식이 아니라 분석 자세입니다.

먼저 이 메일이 왜 지금 왔는지 생각하고, 그 사안에서 가장 중요한 것부터 풀어내세요.
다루면 좋은 것들: 발신자가 무엇을 요청·통보하는지, 핵심 인물과 의사결정권자,
결제 기한·마감·금액처럼 시간과 돈에 민감한 것(있으면 ⚠️로 표시), 그리고 추측이
아닌 구체적인 다음 행동.

무엇을 앞세우고 어떻게 묶을지는 그 메일이 정합니다. 고정된 섹션 틀에 끼워 맞추지
말고 — 어떤 메일은 한 사람의 결정이 핵심이고, 어떤 메일은 타임라인이 전부입니다 —
짧으면 짧게 복잡하면 깊게 쓰세요. 근거 있는 것만 말하고, 모르는 건 추측으로 메우지
마세요. 한국어로 작성합니다.`

const analysisSystemPrompt = "당신은 업무 메일 분석 어시스턴트입니다. 제공된 이메일을 업무 관점에서 분석합니다 — 맥락, 이해관계자, 리스크·기한, 다음 단계."

const (
	// Roomy budget: the local vLLM (step3.7) is the analysis model, and a tight
	// allowance combined with its default extended thinking left no room for the
	// answer text (empty stream → "LLM 응답이 비어있습니다"). Thinking is disabled
	// for this call (see AnalyzeEmail), so this is purely the summary budget.
	llmMaxTokens = 2048
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
		// Mail analysis is summarization, not multi-step reasoning. The local
		// vLLM (step3.7) otherwise spends the whole token budget on extended-
		// thinking blocks and streams no answer text at all, surfacing as
		// "분석을 가져오지 못했습니다" in the client. Disable thinking so the model
		// writes the summary directly — matching how the JSON extraction stages
		// (which never hit this) already behave.
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
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

	result := strings.TrimSpace(stripReasoningLeak(sb.String()))
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
