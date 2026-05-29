// gmail_qa.go — miniapp.gmail.ask RPC: follow-up Q&A about one email.
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

package handlerminiapp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// QATurn is one prior question/answer turn in a mail Q&A thread. The client
// accumulates these and re-sends them so the backend stays stateless and the
// Q&A never touches the main session transcript.
type QATurn struct {
	Q string `json:"q"`
	A string `json:"a"`
}

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
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if strings.TrimSpace(p.Question) == "" {
			return rpcerr.MissingParam("question").Response(req.ID)
		}
		if deps.Ask == nil {
			return rpcerr.Unavailable("mail Q&A not configured").Response(req.ID)
		}

		client, err := deps.Client()
		if err != nil {
			return rpcerr.WrapUnavailable("gmail client unavailable", err).Response(req.ID)
		}
		msg, err := client.GetMessage(ctx, p.ID)
		if err != nil {
			return mapGmailError(req.ID, "gmail get failed", err)
		}
		if msg == nil {
			return rpcerr.NotFound("message " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}

		mailContext := buildMailQAContext(msg, deps)

		answer, err := deps.Ask(ctx, mailContext, p.History, p.Question)
		if err != nil {
			return rpcerr.WrapUnavailable("mail Q&A failed", err).Response(req.ID)
		}
		if strings.TrimSpace(answer) == "" {
			return rpcerr.Unavailable("Q&A returned empty result").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, out{Answer: answer})
	}
}

// buildMailQAContext assembles the grounding context: the email itself
// (FormatEmailForAnalysis truncates the body) plus any cached analysis and
// its related projects. Best-effort — a mail with no cached analysis still
// gets a body-only context so Q&A works pre-analysis too.
func buildMailQAContext(msg *gmail.MessageDetail, deps GmailAnalyzeDeps) string {
	var sb strings.Builder
	sb.WriteString("## 이메일\n")
	sb.WriteString(gmailpoll.FormatEmailForAnalysis(msg))

	if deps.Cache != nil {
		if rec, err := deps.Cache.load(msg.ID); err == nil && rec != nil {
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
