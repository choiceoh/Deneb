// approval_facts.go — quote-verified cost extraction for 전자결재 documents.
//
// The mail fact layer (deal_facts.go) captures document-level commercial terms
// from quotes/contracts arriving by mail. Approval documents (발주품의·지출
// 결의·계약품의) are the other half of the price memory: they carry the cost
// the operator actually approved, usually as an item table (품목·규격·수량·
// 단가·금액) or as a recurring expense (주유비·식대·월 용역료). This extractor
// captures both shapes under the same verbatim-quote gate as deal_facts.go so
// hallucinated prices can never enter the ledger.
package mailanalysis

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

const approvalCostMaxTokens = 1024

const approvalCostExtractorSystem = `당신은 전자결재 문서(발주품의·지출결의·계약품의 등)에서 비용 정보를 "원문 인용과 함께" 뽑는 추출기입니다.
각 숫자 항목은 반드시 원문에서 그대로 복사한 인용(quote)을 함께 제출합니다. 인용할 문장이 없으면 그 항목은 비웁니다.
반드시 JSON으로만 응답하세요.`

const approvalCostExtractorPrompt = `다음 전자결재 문서에서 비용 정보를 추출해주세요.

JSON 응답 형식:
{
  "isCost": true,
  "counterparty": "거래처/공급사명 (문서에 없으면 빈 문자열)",
  "docKind": "발주품의|지출결의|계약품의|견적품의|기타",
  "amount": {"value": "총액 — 공급가액(부가세 제외) 우선 (예: 5,000,000원)", "quote": "그 금액이 나온 원문 그대로"},
  "expenseKind": "반복 경비 카테고리 (예: 주유비, 식대, 차량유지비, 용역비 — 품목 구매/발주면 빈 문자열)",
  "lineItems": [
    {"name": "품목/용역명", "spec": "규격/사양", "qty": "수량", "unitPrice": "단가", "amount": "금액", "quote": "그 행의 원문 그대로"}
  ]
}

추출 기준:
- isCost: 비용 지출·구매·발주·계약 문서면 true. 휴가·인사·규정·단순 보고 문서면 false (다른 필드는 전부 비움)
- quote는 아래 원문에서 **한 글자도 바꾸지 말고 그대로** 복사 (요약·의역 금지)
- 각 항목의 숫자는 자기 quote 안에 있는 숫자만 사용 (합산·환산·추정 금지)
- 금액은 공급가액(부가세 제외)을 우선하고, 원문에 공급가액 구분이 없으면 있는 금액을 그대로 쓰되 value 끝에 "(VAT포함)"을 붙임
- lineItems는 품목 표의 행 단위 (최대 8행; 더 많으면 금액 큰 순)
- 반복 경비(주유비·식대·통행료·월 정산 용역료 등)는 lineItems 대신 expenseKind에 카테고리를 적음

## 결재 문서
%s`

// ApprovalCostLine is one extracted 품목/용역 row with its verbatim source row.
type ApprovalCostLine struct {
	Name      string `json:"name"`
	Spec      string `json:"spec"`
	Qty       string `json:"qty"`
	UnitPrice string `json:"unitPrice"`
	Amount    string `json:"amount"`
	Quote     string `json:"quote"`
}

// ApprovalCostFacts is the quote-verified cost extraction of one approval
// document. After verification every surviving figure is backed by a verbatim
// quote from the document.
type ApprovalCostFacts struct {
	IsCost       bool               `json:"isCost"`
	Counterparty string             `json:"counterparty"`
	DocKind      string             `json:"docKind"`
	Amount       QuotedFact         `json:"amount"`
	ExpenseKind  string             `json:"expenseKind"`
	LineItems    []ApprovalCostLine `json:"lineItems"`
}

// Empty reports whether nothing filable survived: no total, no item rows.
func (f *ApprovalCostFacts) Empty() bool {
	return f == nil || (!f.IsCost) || (f.Amount.Value == "" && len(f.LineItems) == 0)
}

// ExtractApprovalCostFacts runs the quote-mandatory cost extractor over one
// approval document and returns only what survives verification, or nil.
// Best-effort like extractDealFacts: any error degrades to nil, never to a
// plausible-but-wrong record.
func ExtractApprovalCostFacts(ctx context.Context, client *llm.Client, model string, logger *slog.Logger, source string) *ApprovalCostFacts {
	if client == nil || strings.TrimSpace(model) == "" || strings.TrimSpace(source) == "" {
		return nil
	}
	extractCtx, cancel := context.WithTimeout(ctx, stage1Timeout)
	defer cancel()

	prompt := fmt.Sprintf(approvalCostExtractorPrompt, source)
	// json_object (schema=nil) — free-text string fields are the xgrammar
	// whitespace-explosion shape; see the DealFacts doc.
	facts, err := callLocalLLMJSON[ApprovalCostFacts](extractCtx, client, model, approvalCostExtractorSystem, prompt, approvalCostMaxTokens, nil)
	if err != nil {
		return nil
	}
	return verifyApprovalCostFacts(&facts, source, logger)
}

// verifyApprovalCostFacts applies the deterministic quote gate (deal_facts.go
// contract) to the total and every line item, dropping what fails. Identity
// fields (counterparty·docKind·expenseKind) carry no figures and are only
// trimmed. Returns nil when nothing filable survives.
func verifyApprovalCostFacts(facts *ApprovalCostFacts, source string, logger *slog.Logger) *ApprovalCostFacts {
	if facts == nil || !facts.IsCost {
		return nil
	}
	facts.Counterparty = strings.TrimSpace(facts.Counterparty)
	facts.DocKind = strings.TrimSpace(facts.DocKind)
	facts.ExpenseKind = strings.TrimSpace(facts.ExpenseKind)
	normSource := stripSpaces(source)

	amount := facts.Amount
	verifyQuotedFact("결재→원장: 비용 인용 검증 실패로 드롭", "amount", &amount, normSource, logger)
	facts.Amount = amount

	kept := facts.LineItems[:0]
	for _, li := range facts.LineItems {
		li.Name = strings.TrimSpace(li.Name)
		li.Spec = strings.TrimSpace(li.Spec)
		li.Qty = strings.TrimSpace(li.Qty)
		li.UnitPrice = strings.TrimSpace(li.UnitPrice)
		li.Amount = strings.TrimSpace(li.Amount)
		li.Quote = strings.TrimSpace(li.Quote)
		reason := ""
		switch {
		case li.Name == "":
			reason = "품목명 없음"
		case li.Quote == "":
			reason = "인용 없음"
		case !strings.Contains(normSource, stripSpaces(li.Quote)):
			reason = "인용이 원문에 없음"
		case !digitsCoveredBy(li.Qty, li.Quote),
			!digitsCoveredBy(li.UnitPrice, li.Quote),
			!digitsCoveredBy(li.Amount, li.Quote):
			reason = "값의 숫자가 인용에 없음"
		}
		if reason != "" {
			if logger != nil {
				logger.Warn("결재→원장: 품목 행 인용 검증 실패로 드롭",
					"name", li.Name, "unitPrice", li.UnitPrice, "reason", reason)
			}
			continue
		}
		kept = append(kept, li)
	}
	facts.LineItems = kept
	if facts.Empty() {
		return nil
	}
	return facts
}

// verifyQuotedFact runs the single-fact quote gate shared by verifyDealFacts
// (mail) and verifyApprovalCostFacts (결재): the quote must appear verbatim in
// the source and the value's digits must appear in its own quote.
func verifyQuotedFact(logMsg, name string, f *QuotedFact, normSource string, logger *slog.Logger) {
	f.Value = strings.TrimSpace(f.Value)
	f.Quote = strings.TrimSpace(f.Quote)
	if f.Value == "" {
		f.Quote = ""
		return
	}
	reason := ""
	switch {
	case f.Quote == "":
		reason = "인용 없음"
	case !strings.Contains(normSource, stripSpaces(f.Quote)):
		reason = "인용이 원문에 없음"
	case !digitsCoveredBy(f.Value, f.Quote):
		reason = "값의 숫자가 인용에 없음"
	}
	if reason == "" {
		return
	}
	if logger != nil {
		logger.Warn(logMsg, "field", name, "value", f.Value, "reason", reason)
	}
	f.Value, f.Quote = "", ""
}
