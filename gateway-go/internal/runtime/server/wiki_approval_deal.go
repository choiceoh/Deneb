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
)

// fileApprovalCost extracts quote-verified cost facts (품목 라인아이템 또는
// 반복 경비 + 총액) from one analyzed approval document, computes exact deltas
// against the ledger's prior prices, and files the record. Returns the delta
// lines (nil when nothing comparable) so the caller can append them to the
// analysis — deterministic 단가 비교 independent of the model's narrative.
func (s *Server) fileApprovalCost(ctx context.Context, docID, title, date, body string) []string {
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
	if ref, ok := s.wikiStore.UniqueProjectInText(title); ok {
		related = []string{ref.Path}
	}
	input := wiki.DealPageInput{
		Counterparty:    counterparty,
		DocType:         facts.DocKind,
		Amount:          facts.Amount.Value,
		Date:            strings.TrimSpace(date),
		Items:           items,
		SourceRef:       "approval:" + strings.TrimSpace(docID),
		RelatedProjects: related,
		LineItems:       lineItems,
		ExpenseKind:     facts.ExpenseKind,
	}

	// Deltas must read the ledger BEFORE the upsert tees the new record
	// (DetectRequote's contract — otherwise the new row is its own previous).
	deltas := s.wikiStore.PriceDeltaLines(input)

	relPath, created, err := s.wikiStore.UpsertDealPage(input, time.Now())
	if err != nil {
		s.logger.Warn("결재→원장: 거래 페이지 저장 실패", "docId", docID, "counterparty", counterparty, "error", err)
		return deltas
	}
	s.logger.Info("결재→원장: 결재 비용 파일링", "docId", docID, "path", relPath,
		"created", created, "lineItems", len(lineItems), "expenseKind", facts.ExpenseKind)
	return deltas
}
