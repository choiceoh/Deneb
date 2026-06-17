package gmailpoll

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

const PromptIDAutoMailAnalysis = "mail.auto.analysis"

// DefaultPrompt is the editable default instruction block for single-mail
// analysis. The pipeline owns the source/context insertion; operators edit this
// stance text from the native Prompt Corner.
const DefaultPrompt = `카카오메일 알림으로 도착한 새 메일을 업무 관점에서 깊이 분석한다.
업무 무관한 광고·뉴스레터·자동 알림이면 길게 분석하지 말고 "참고"로 짧게 끝낸다.

분석은 고정 양식 채우기가 아니라 판단이다. 그래도 다음 관점은 반드시 확인한다.
- 이 메일이 왜 지금 왔는지: 직전 합의, 바뀐 조건, 이어지는 요청을 이전 메일 맥락과 관련 기억으로 연결한다.
- 발신자·수신자·회사·프로젝트 관계: 누가 어떤 입장에서 말하고 있고, 실무/결정 라인이 어떻게 움직이는지 본다.
- 프로젝트 맥락: 현재 어디까지 진행됐고, 이 메일이 그 흐름에서 어떤 의미인지 설명한다.
- 숫자와 조건: 단가·수량·금액·납기·결제기한·마감·승인 조건이 있으면 원문 수치를 보존하고, 이전 맥락과 달라진 값이 있을 때만 비교한다.
- 첨부파일 내용이 주어졌으면 본문보다 첨부 원문 수치를 우선해 반영한다.
- 마지막은 추측이 아닌 구체적인 다음 행동으로 끝낸다.

보고는 먼저 사람이 바로 읽을 수 있는 텍스트로 출력한다. 메일 전체 본문을 그대로 전달하지 않는다.
중요도는 "긴급", "확인 필요", "참고" 중 하나가 드러나게 쓰되, 장식용 이모지는 쓰지 않는다.
기한·금액처럼 놓치면 손해가 큰 경고에만 ⚠️를 드물게 쓸 수 있다.
한국어로 간결하게 쓰고, 근거가 필요한 판단에는 메일 문구나 이전 맥락을 짧게 붙여 사실과 추측을 구분한다.`

// emojiRestraint is appended to every mail-analysis system prompt to stop the
// model decorating analyses/reports with emoji. A single ⚠️ for a genuine
// deadline is fine; section/bullet/heading emoji are not.
const emojiRestraint = "이모지는 최소화하세요: 기한·금액처럼 놓치면 안 되는 경고에만 ⚠️ 정도를 드물게 쓰고, 섹션 제목이나 항목마다 장식용 이모지(📌✨🔥📊🙂 등)를 붙이지 마세요."

const analysisSystemPrompt = "당신은 업무 메일 분석 어시스턴트입니다. 제공된 이메일을 업무 관점에서 분석합니다 — 맥락, 이해관계자, 리스크·기한, 다음 단계. " +
	"모든 섹션 제목·라벨은 한국어로 쓰세요 ('Primary Analysis', 'Summary', 'Action Items' 같은 영문 라벨 금지). " +
	emojiRestraint

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
		body = textutil.TruncateBytes(body, maxBodyChars) + "\n\n... (본문 생략)"
	}
	sb.WriteString(body)

	return sb.String()
}

// AnalyzeEmail sends an email to the LLM for analysis and returns the result.
// thinkingKwarg is the model's chat_template_kwargs off-switch (e.g. dsv4's
// "thinking"); "" for models without one. Without it, "disabled" degrades to a
// no-op reasoning_effort on dual-mode vLLM models, which then exhaust the budget
// on reasoning and return empty.
func AnalyzeEmail(ctx context.Context, client *llm.Client, model, prompt, thinkingKwarg string, msg *gmail.MessageDetail) (string, error) {
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
		Thinking: &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: thinkingKwarg},
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
			// Skip thinking_delta: vLLM/step3.7 forces a <think> block via its
			// chat template even though thinking is disabled above, and the
			// OpenAI-translated stream carries that reasoning in .Delta.Text. The
			// delta type is the reliable signal — matching the multi-stage path's
			// collectStreamText (pipeline.go), which already filters it.
			if json.Unmarshal(ev.Payload, &delta) == nil &&
				delta.Delta.Type != "thinking_delta" && delta.Delta.Text != "" {
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

	result := strings.TrimSpace(sanitizeAnalysisLeak(sb.String()))
	if result == "" {
		return "", fmt.Errorf("LLM 응답이 비어있습니다")
	}
	return result, nil
}
