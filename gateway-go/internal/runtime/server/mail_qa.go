// mail_qa.go — wiring for miniapp.gmail.ask (mail follow-up Q&A).
//
// The handler (handlerminiapp/gmail_qa.go) assembles the grounding context
// from the email + cached analysis; this layer runs the actual LLM via the
// chat handler's SendSync as an EPHEMERAL, ISOLATED run:
//
//   - mail context → system prompt (grounding, set once)
//   - prior {q,a} turns + new question → message list (PrebuiltMessages,
//     replacing normal transcript assembly)
//   - EphemeralUser/Assistant → nothing persists to the main session
//
// So this is a side Q&A scoped to the mail view, not a revival of the
// removed Mini App chat surface (#1704).

package server

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

const mailQASystemPrompt = `당신은 사용자가 보고 있는 이메일에 대한 후속 질문에 답하는 비서다.
주어진 이메일 본문과 분석, 관련 프로젝트 맥락에만 근거해 한국어로 간결하게 답하라.
맥락에 없는 내용은 추측하지 말고 모른다고 답하라. 불필요한 서두 없이 핵심만 전달하라.`

// mailQAMaxTokens caps the follow-up answer. Q&A answers should be short and
// focused, not essay-length.
const mailQAMaxTokens = 1536

// makeMailQAAsk returns the Ask callback wired into GmailAnalyzeDeps. Returns
// nil when chatHandler isn't ready, in which case the handler skips
// registering miniapp.gmail.ask entirely.
func (s *Server) makeMailQAAsk() func(context.Context, string, []handlerminiapp.QATurn, string) (string, error) {
	if s.chatHandler == nil {
		return nil
	}
	return func(ctx context.Context, mailContext string, history []handlerminiapp.QATurn, question string) (string, error) {
		msgs := make([]llm.Message, 0, len(history)*2+1)
		for _, t := range history {
			if strings.TrimSpace(t.Q) != "" {
				msgs = append(msgs, llm.NewTextMessage("user", t.Q))
			}
			if strings.TrimSpace(t.A) != "" {
				msgs = append(msgs, llm.NewTextMessage("assistant", t.A))
			}
		}
		msgs = append(msgs, llm.NewTextMessage("user", question))

		maxTok := mailQAMaxTokens
		// Fresh ephemeral session key per call — PrebuiltMessages carry the
		// full context, so no session state is reused; Ephemeral* keep the
		// transcript empty. model="" → chat handler's default.
		res, err := s.chatHandler.SendSync(ctx, "mail-qa:"+shortid.New("m"), "", "", &chat.SyncOptions{
			Messages:           msgs,
			SystemPrompt:       mailQASystemPrompt + "\n\n" + mailContext,
			ToolPreset:         "conversation",
			MaxTokens:          &maxTok,
			EphemeralUser:      true,
			EphemeralAssistant: true,
		})
		if err != nil {
			return "", err
		}
		return res.Text, nil
	}
}
