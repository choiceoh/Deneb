// groupware_approvals_qa.go — grounded follow-up Q&A for one approval.
package handlerminiapp

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	approvalQAMaxHistoryTurns  = 8
	approvalQAMaxTurnRunes     = 2000
	approvalQAMaxQuestionRunes = 2000
	approvalQAMaxTitleRunes    = 300
	approvalQAMaxBodyRunes     = 24000
	approvalQAMaxAnalysisRunes = 12000
)

// GroupwareApprovalsAskMethods is split from GroupwareApprovalsMethods because
// Ask is chat-backed and can only be wired in the late registration phase.
// Get and Ask are required; cached analysis is best-effort and optional.
func GroupwareApprovalsAskMethods(deps GroupwareApprovalsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Get == nil || deps.Ask == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.groupware.approvals.ask": groupwareApprovalsAsk(deps),
	}
}

func groupwareApprovalsAsk(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		DocID string `json:"docId"`
		// Title is a display hint retained for wire compatibility. Q&A never
		// trusts it as evidence; only the fetched body's `제목:` field does.
		Title    string   `json:"title,omitempty"`
		Folder   string   `json:"folder,omitempty"`
		Question string   `json:"question"`
		History  []QATurn `json:"history,omitempty"`
	}
	type out struct {
		Answer string `json:"answer"`
	}
	return bindAuthenticated[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		docID := strings.TrimSpace(p.DocID)
		if docID == "" {
			return rpcerr.MissingParam("docId").Response(req.ID)
		}
		question := strings.TrimSpace(p.Question)
		if question == "" {
			return rpcerr.MissingParam("question").Response(req.ID)
		}
		if utf8.RuneCountInString(question) > approvalQAMaxQuestionRunes {
			return rpcerr.InvalidRequest(fmt.Sprintf("question must be at most %d characters", approvalQAMaxQuestionRunes)).Response(req.ID)
		}
		folder := strings.ToLower(strings.TrimSpace(p.Folder))
		if folder != "" {
			if _, ok := approvalFolders[folder]; !ok {
				return rpcerr.InvalidRequest("folder must be pending|done|cc|total|all").Response(req.ID)
			}
		}

		body, err := deps.Get(ctx, docID, folder)
		if err != nil {
			return rpcerr.WrapDependencyFailed("get groupware approval for Q&A", err).Response(req.ID)
		}
		rawBody := strings.TrimSpace(body)

		// Only the reader-authored `제목:` metadata field is authoritative.
		// Client title and cached analysis title may both originate from an
		// earlier client request, so neither may enter the grounding envelope.
		title := approvalQATitleFromBody(rawBody)
		body = groupware.SelectApprovalEvidenceContext(ctx, rawBody, question, approvalQAMaxBodyRunes)
		analysis := ""
		if deps.Cache != nil {
			// Cache corruption or a version-skew miss must not block body-only Q&A.
			if rec, loadErr := deps.Cache.Load(docID); loadErr == nil && rec != nil {
				analysis = truncateApprovalQARunes(strings.TrimSpace(rec.Analysis), approvalQAMaxAnalysisRunes)
			}
		}

		answer, err := deps.Ask(ctx, docID, title, body, analysis, boundApprovalQAHistory(p.History), question)
		if err != nil {
			return rpcerr.WrapDependencyFailed("answer groupware approval question", err).Response(req.ID)
		}
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return rpcerr.Unavailable("approval Q&A returned empty result").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, out{Answer: answer})
	})
}

func approvalQATitleFromBody(body string) string {
	for i, raw := range strings.Split(body, "\n") {
		if i >= 12 {
			break
		}
		line := strings.TrimSpace(raw)
		if line == "본문" || strings.HasPrefix(line, "본문:") || strings.HasPrefix(line, "본문：") {
			break
		}
		for _, prefix := range []string{"제목:", "제목："} {
			if strings.HasPrefix(line, prefix) {
				return truncateApprovalQARunes(strings.TrimSpace(strings.TrimPrefix(line, prefix)), approvalQAMaxTitleRunes)
			}
		}
	}
	return ""
}

func boundApprovalQAHistory(history []QATurn) []QATurn {
	if len(history) > approvalQAMaxHistoryTurns {
		history = history[len(history)-approvalQAMaxHistoryTurns:]
	}
	out := make([]QATurn, 0, len(history))
	for _, turn := range history {
		q := truncateApprovalQARunes(strings.TrimSpace(turn.Q), approvalQAMaxTurnRunes)
		a := truncateApprovalQARunes(strings.TrimSpace(turn.A), approvalQAMaxTurnRunes)
		if q == "" && a == "" {
			continue
		}
		out = append(out, QATurn{Q: q, A: a})
	}
	return out
}

func truncateApprovalQARunes(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	marker := []rune("…(truncated)")
	if max <= len(marker) {
		return string([]rune(text)[:max])
	}
	runes := []rune(text)
	return string(runes[:max-len(marker)]) + string(marker)
}
