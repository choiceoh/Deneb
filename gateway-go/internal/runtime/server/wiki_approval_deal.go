// wiki_approval_deal.go — files an analyzed 전자결재 document's approved cost
// onto the deal ledger (품목 단가 기억), the approval-side counterpart of
// wiki_mail_analysis.go's fileDealFromMail. Silent and best-effort: idempotent
// by docId, failures logged only — the analysis the user sees never depends on
// filing succeeding.
package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// fileApprovalCost extracts quote-verified cost facts (품목 라인아이템 또는
// 반복 경비 + 총액) from one analyzed approval document, computes exact deltas
// against the ledger's prior prices, and files the record. Returns the delta
// lines (nil when nothing comparable) so the caller can append them to the
// analysis — deterministic 단가 비교 independent of the model's narrative.
//
// projectFile gates only the project 현재 상태 cost bullet (agent judgment);
// the deal ledger always upserts when cost facts exist.
func (s *Server) fileApprovalCost(ctx context.Context, docID, title, date, body string, projectFile bool) []string {
	if s == nil || s.wikiStore == nil || strings.TrimSpace(docID) == "" {
		return nil
	}
	_, _, localClient, localModel := s.mailAnalysisModels()
	source := strings.TrimSpace(title) + "\n\n" + body
	facts := mailanalysis.ExtractApprovalCostFacts(ctx, localClient, localModel, s.logger, source)
	if facts.Empty() {
		return nil
	}

	// The deal page is keyed by counterparty; a recurring expense without one
	// (법인카드 주유비 등) files onto its category ledger page (거래/경비-주유비).
	counterparty := facts.Counterparty
	if counterparty == "" && facts.ExpenseKind != "" {
		counterparty = "경비-" + facts.ExpenseKind
	}
	if counterparty == "" {
		s.logger.Info("결재→원장: 거래처·경비 카테고리 모두 없어 파일링 생략", "docId", docID)
		return nil
	}

	items := make([]string, 0, len(facts.LineItems))
	lineItems := make([]wiki.DealLineItem, 0, len(facts.LineItems))
	for _, li := range facts.LineItems {
		items = append(items, li.Name)
		lineItems = append(lineItems, wiki.DealLineItem{
			Name:      li.Name,
			Spec:      li.Spec,
			Qty:       li.Qty,
			UnitPrice: li.UnitPrice,
			Amount:    li.Amount,
			Quote:     li.Quote,
		})
	}
	var related []string
	if ref, ok := resolveApprovalProject(s.wikiStore, title, body); ok {
		related = []string{ref.Path}
	}
	sourceRef := "approval:" + strings.TrimSpace(docID)
	input := wiki.DealPageInput{
		Counterparty:    counterparty,
		DocType:         facts.DocKind,
		Amount:          facts.Amount.Value,
		Date:            strings.TrimSpace(date),
		Items:           items,
		SourceRef:       sourceRef,
		RelatedProjects: related,
		LineItems:       lineItems,
		ExpenseKind:     facts.ExpenseKind,
	}

	// Deltas must read the ledger BEFORE the upsert tees the new record
	// (DetectRequote's contract — otherwise the new row is its own previous).
	deltas := s.wikiStore.PriceDeltaLines(input)

	now := time.Now()
	relPath, created, err := s.wikiStore.UpsertDealPage(input, now)
	if err != nil {
		s.logger.Warn("결재→원장: 거래 페이지 저장 실패", "docId", docID, "counterparty", counterparty, "error", err)
		return deltas
	}
	s.logger.Info("결재→원장: 결재 비용 파일링", "docId", docID, "path", relPath,
		"created", created, "lineItems", len(lineItems), "expenseKind", facts.ExpenseKind)

	if projectFile {
		s.appendApprovalCostToProjects(related, title, counterparty, facts.Amount.Value, deltas, docID, now)
	}
	return deltas
}

// appendApprovalCostToProjects surfaces cost/delta glance lines on linked
// project 대표페이지 현재 상태. Separate ref from the analysis status bullet
// so both can coexist (approval: vs approval-cost:).
func (s *Server) appendApprovalCostToProjects(projects []string, title, counterparty, amount string, deltas []string, docID string, now time.Time) {
	if s == nil || s.wikiStore == nil || len(projects) == 0 {
		return
	}
	line := approvalCostStatusLine(title, counterparty, amount, deltas)
	if line == "" {
		return
	}
	ref := "approval-cost:" + strings.TrimSpace(docID)
	for _, p := range projects {
		if err := s.wikiStore.AppendProjectStatusLine(p, line, ref, now); err != nil && s.logger != nil {
			s.logger.Warn("결재→원장: 프로젝트 상태줄 기록 실패", "docId", docID, "project", p, "error", err)
		}
	}
}

func approvalCostStatusLine(title, counterparty, amount string, deltas []string) string {
	if len(deltas) > 0 {
		// First delta is the highest-signal glance (품목 단가 or 경비 중앙값).
		return textutil.TruncateRunes(approvalLogText(deltas[0]), 80, "...")
	}
	parts := make([]string, 0, 3)
	parts = append(parts, "결재 비용")
	if c := strings.TrimSpace(counterparty); c != "" {
		parts = append(parts, c)
	}
	if a := strings.TrimSpace(amount); a != "" {
		parts = append(parts, a)
	} else if t := strings.TrimSpace(title); t != "" {
		parts = append(parts, textutil.TruncateRunes(approvalLogText(t), 40, "..."))
	}
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts, " ")
}
