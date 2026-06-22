package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

// deal_question.go — the "질문 카드" loop. When mail analysis files a NEW deal it
// can't place, Deneb asks the one thing it genuinely can't infer and that the
// operator must decide anyway — which team owns it (the 부서 segment of the
// project code; also the executive's delegation call) — as a work-feed card with
// one-tap answers. Answering settles the card and records the team onto the
// deal's wiki page: the "불확실 → 질문 → 기록" closed loop the system prompt asks for
// but, lacking a surface, never actually ran.
//
// Why a NEW deal page is the trigger (not model-judged uncertainty): UpsertDealPage
// returns created=true only the first time a counterparty's deal page is minted,
// so the question fires exactly once per new deal — deterministic, no guessing
// whether the LLM "felt unsure", and never a repeat ask.

// dealQuestionSource tags the card so the work-feed action handler knows to route
// its answer back here.
const dealQuestionSource = "deal_question"

// dealQuestionActionPrefix prefixes answer action IDs ("dept:pl1"), so the handler
// can tell a deal-question answer from an ordinary ack.
const dealQuestionActionPrefix = "dept:"

// deptOptions are the answer buttons: each maps a team label to the 부서 code
// segment of the project-code scheme (domain/wiki/code.go). "none" lets the
// operator dismiss a misfired deal without recording a team.
var deptOptions = []struct{ code, label string }{
	{"pl1", "1팀"},
	{"pl2", "2팀"},
	{"pl3", "3팀"},
	{"nde", "남도에코"},
	{"pl0", "내 직할"},
	{"none", "딜 아님"},
}

// appendDealQuestionCard posts a "which team owns this new deal?" question to the
// work feed. Best-effort: a feed failure must never break mail analysis.
func (s *Server) appendDealQuestionCard(deal *gmailpoll.DealInfo, dealPagePath string) {
	nf := s.nativeWorkFeedStore()
	if nf == nil || deal == nil {
		return
	}
	actions := make([]workfeed.Action, 0, len(deptOptions))
	for _, o := range deptOptions {
		actions = append(actions, workfeed.Action{
			ID:    dealQuestionActionPrefix + o.code,
			Kind:  workfeed.ActionAck, // ack-kind settles + removes the card when tapped
			Label: o.label,
		})
	}
	if _, err := nf.Append(workfeed.Item{
		Source:   dealQuestionSource,
		Title:    "새 거래: " + strings.TrimSpace(deal.Counterparty),
		Summary:  "어느 팀이 담당할까요?",
		Body:     dealQuestionBody(deal),
		RefType:  "wiki",
		RefID:    dealPagePath, // the deal page the answer gets recorded onto
		Status:   "unread",
		Question: true, // render with inline answer chips (the dept options below)
		Actions:  actions,
	}); err != nil {
		s.logger.Warn("deal question 카드 생성 실패", "counterparty", deal.Counterparty, "error", err)
	}
}

func dealQuestionBody(deal *gmailpoll.DealInfo) string {
	var b strings.Builder
	b.WriteString("새 거래처에서 문서가 왔는데, 어느 팀이 담당할지 몰라 정하지 못했어요.\n\n")
	if d := strings.TrimSpace(deal.DocType); d != "" {
		b.WriteString("- 문서: " + d + "\n")
	}
	if a := strings.TrimSpace(deal.Amount); a != "" {
		b.WriteString("- 금액: " + a + "\n")
	}
	if su := strings.TrimSpace(deal.Summary); su != "" {
		b.WriteString("- 요약: " + su + "\n")
	}
	b.WriteString("\n담당 팀을 눌러주시면 거래 페이지에 기록하고, 다음부터는 다시 묻지 않을게요.")
	return b.String()
}

// recordDealQuestionAnswer writes the operator's team answer onto the deal's wiki
// page (closing the 불확실 → 질문 → 기록 loop). Wired as WorkFeedDeps.OnAnswer; the
// handler calls it after a deal_question card's "dept:*" answer settles.
// Best-effort — a write failure is logged, not surfaced (the card is already
// settled by then).
func (s *Server) recordDealQuestionAnswer(item workfeed.Item, actionID string) {
	if s.wikiStore == nil {
		return
	}
	pagePath := strings.TrimSpace(item.RefID)
	if pagePath == "" {
		return
	}
	code := strings.TrimPrefix(actionID, dealQuestionActionPrefix)
	// "딜 아님": the operator says this isn't a real deal — record nothing, just log.
	if code == "none" {
		s.logger.Info("deal question: 딜 아님 응답", "path", pagePath)
		return
	}
	page, err := s.wikiStore.ReadPage(pagePath)
	if err != nil || page == nil {
		s.logger.Warn("deal question 기록: 페이지 읽기 실패", "path", pagePath, "error", err)
		return
	}
	label := deptLabel(code)
	stamp := time.Now().Format("2006-01-02")
	// Append the assignment as a visible fact. The dreamer's code minting
	// (dreamer_code.go) can later fold this 부서 into the project code.
	page.Body = strings.TrimRight(page.Body, "\n") +
		fmt.Sprintf("\n\n## 담당 팀\n%s (%s, 사용자 지정)\n", label, stamp)
	if err := s.wikiStore.WritePage(pagePath, page); err != nil {
		s.logger.Warn("deal question 기록 실패", "path", pagePath, "error", err)
		return
	}
	s.logger.Info("deal question: 담당 팀 기록", "path", pagePath, "team", label)
}

func deptLabel(code string) string {
	for _, o := range deptOptions {
		if o.code == code {
			return o.label
		}
	}
	return code
}
