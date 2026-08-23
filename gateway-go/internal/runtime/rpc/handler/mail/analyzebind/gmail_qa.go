// gmail_qa.go — miniapp.mail.ask RPC: follow-up Q&A about one email.
//
// The operator reads an analysis in the Mini App and wants to drill in
// ("이게 무슨 뜻이지?", "그래서 뭘 해야 하지?") without leaving the mail view.
// This answers that question grounded in the email body + its cached
// analysis + related projects. The actual LLM call (Ask) is an ephemeral,
// isolated run wired in the server layer — nothing is persisted to the main
// session, so this is a side Q&A, not a revival of the removed chat surface.
//
// Stateless multi-turn: the client accumulates prior {q,a} turns and resends
// them, so this handler holds no per-conversation state.
package analyzebind

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail/gmailops"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// gmailAsk answers a follow-up question about a specific email. It assembles
// the grounding context (email + cached analysis + projects) and forwards to
// the Ask callback, which runs the ephemeral LLM. Returns the answer text.
func gmailAsk(deps GmailAnalyzeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID       string   `json:"id"`
		Question string   `json:"question"`
		History  []QATurn `json:"history,omitempty"`
	}
	type out struct {
		Answer string `json:"answer"`
	}
	return bindOptional(func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if strings.TrimSpace(p.ID) == "" {
			return gmailops.RPCMissingParam("id").Response(req.ID)
		}
		if strings.TrimSpace(p.Question) == "" {
			return gmailops.RPCMissingParam("question").Response(req.ID)
		}
		if deps.Ask == nil {
			return gmailops.RPCUnavailable("mail Q&A not configured").Response(req.ID)
		}

		client, err := deps.Client()
		if err != nil {
			return gmailops.RPCWrapUnavailable("gmail client unavailable", err).Response(req.ID)
		}
		msg, err := client.GetMessage(ctx, p.ID)
		if err != nil {
			return mapGmailError(req.ID, "gmail get failed", err)
		}
		if msg == nil {
			return gmailops.RPCNotFound("message " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}

		mailContext := buildMailQAContext(msg, deps)

		answer, err := deps.Ask(ctx, mailContext, p.History, p.Question)
		if err != nil {
			return gmailops.RPCWrapUnavailable("mail Q&A failed", err).Response(req.ID)
		}
		if strings.TrimSpace(answer) == "" {
			return gmailops.RPCUnavailable("Q&A returned empty result").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, out{Answer: answer})
	})
}

// buildMailQAContext assembles the grounding context: the email itself
// (FormatEmailForAnalysis truncates the body) plus any cached analysis and
// its related projects. Best-effort — a mail with no cached analysis still
// gets a body-only context so Q&A works pre-analysis too.
func buildMailQAContext(msg *gmail.MessageDetail, deps GmailAnalyzeDeps) string {
	var sb strings.Builder
	sb.WriteString("## 이메일\n")
	sb.WriteString(mailanalysis.FormatEmailForAnalysis(msg))

	if deps.Cache != nil {
		if rec, err := deps.Cache.Load(msg.ID); err == nil && rec != nil {
			if strings.TrimSpace(rec.Analysis) != "" {
				sb.WriteString("\n\n## 분석\n")
				sb.WriteString(rec.Analysis)
			}
			if len(rec.RelatedProjects) > 0 {
				sb.WriteString("\n\n## 관련 프로젝트\n")
				for _, path := range rec.RelatedProjects {
					sb.WriteString("- ")
					sb.WriteString(path)
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}
