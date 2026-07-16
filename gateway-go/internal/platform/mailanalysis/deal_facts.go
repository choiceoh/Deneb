// deal_facts.go — quote-verified commercial terms (사실 레이어 2단계, stage-1).
//
// The deal extractor (pipeline_extractors.go) captures the document's identity
// (거래처·문서종류·총액·기한). This extractor captures the deal's COMMERCIAL
// TERMS — 물량(용량)·단가·지급조건·하자보수·지체상금 — the figures a requote
// comparison and MW/단가 aggregation need. Because these come from RoleTiny and
// get frozen as citable facts, every field carries a mandatory verbatim quote
// and passes a deterministic Go gate (verifyDealFacts): a field survives only
// if its quote actually appears in the source AND the value's digits appear in
// its own quote. Extraction errors degrade to dropped fields, never to
// plausible-but-wrong "facts" (gateDealAmount's lesson, extended field-wise).
package mailanalysis

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

const dealFactsExtractorSystem = `당신은 업무 문서(견적서·계약서·발주서 등)에서 핵심 거래 조건을 "원문 인용과 함께" 뽑는 추출기입니다.
각 항목은 반드시 원문에서 그대로 복사한 인용(quote)을 함께 제출합니다. 인용할 문장이 없으면 그 항목은 비웁니다.
반드시 JSON으로만 응답하세요.`

const dealFactsExtractorPrompt = `다음 이메일 분석(첨부 문서 내용 포함)에서 거래 조건을 추출해주세요.

JSON 응답 형식:
{
  "capacityMW": {"value": "물량/설비용량 (예: 3.5MW, 2,940kW)", "quote": "그 수치가 나온 원문 문장 그대로"},
  "unitPrice": {"value": "단가 (예: 148원/W)", "quote": "원문 문장 그대로"},
  "paymentTerms": {"value": "지급 조건 요약 (예: 선수금 10%% / 잔금 90%%)", "quote": "원문 문장 그대로"},
  "warranty": {"value": "하자보수·보증 (예: 하자보증 3년)", "quote": "원문 문장 그대로"},
  "delayPenalty": {"value": "지체상금 (예: 1일당 1/1000)", "quote": "원문 문장 그대로"}
}

추출 기준:
- quote는 아래 원문에서 **한 글자도 바꾸지 말고 그대로** 복사 (요약·의역 금지, 그 수치가 든 한 문장이면 충분)
- 해당 조건이 원문에 없으면 value와 quote 둘 다 빈 문자열
- value의 숫자는 quote 안에 있는 숫자만 사용 (합산·환산·추정 금지)

## 분석 결과
%s`

// QuotedFact is one commercial term plus the verbatim source sentence it came
// from. The quote is the audit trail: verifyDealFacts drops the whole fact when
// the quote is missing, isn't verbatim in the source, or doesn't contain the
// value's digits.
type QuotedFact struct {
	Value string `json:"value"`
	Quote string `json:"quote"`
}

// DealFacts are the quote-verified commercial terms of a deal document. Every
// field is optional; after verification a field is either trustworthy or
// zeroed.
//
// Stays on plain json_object like dealExtract (see that type's doc): ten
// free-text string fields are exactly the xgrammar whitespace-explosion shape,
// so no strict json_schema here.
type DealFacts struct {
	CapacityMW   QuotedFact `json:"capacityMW"`
	UnitPrice    QuotedFact `json:"unitPrice"`
	PaymentTerms QuotedFact `json:"paymentTerms"`
	Warranty     QuotedFact `json:"warranty"`
	DelayPenalty QuotedFact `json:"delayPenalty"`
}

// Empty reports whether no field survived (or was ever filled).
func (f *DealFacts) Empty() bool {
	return f == nil ||
		(f.CapacityMW.Value == "" && f.UnitPrice.Value == "" && f.PaymentTerms.Value == "" &&
			f.Warranty.Value == "" && f.DelayPenalty.Value == "")
}

// extractDealFacts runs the quote-mandatory terms extractor over the same
// source text the deal extractor read (analysis + verbatim attachment) and
// returns only the fields that survive verification, or nil when nothing does.
// Best-effort like its siblings; callers gate on a recognized deal so this
// never fires on plain mail.
func extractDealFacts(ctx context.Context, deps PipelineDeps, source string) *DealFacts {
	if deps.LocalClient == nil || deps.LocalModel == "" {
		return nil
	}
	if strings.TrimSpace(source) == "" {
		return nil
	}

	extractCtx, cancel := context.WithTimeout(ctx, stage1Timeout)
	defer cancel()

	prompt := fmt.Sprintf(dealFactsExtractorPrompt, source)
	// json_object (schema=nil) — see the DealFacts doc for the xgrammar caveat.
	facts, err := callLocalLLMJSON[DealFacts](extractCtx, deps.LocalClient, deps.LocalModel, dealFactsExtractorSystem, prompt, stage1MaxTokens, nil)
	if err != nil {
		return nil
	}
	return verifyDealFacts(&facts, source, deps.Logger)
}

// verifyDealFacts applies the deterministic quote gate to every field and
// returns the surviving set (nil when none survive). Verification is two
// substring checks, both whitespace-insensitive so OCR/LLM re-spacing never
// fails a genuinely verbatim quote:
//
//  1. the quote must appear in the source (verbatim provenance), and
//  2. every digit run of the value must appear in the quote (the value cannot
//     carry numbers its own evidence doesn't show — no 합산/환산/추정).
//
// A dropped field is Warn-logged, never silent (this codebase's repeated
// lesson); a kept field is trusted downstream as a citable fact.
func verifyDealFacts(facts *DealFacts, source string, logger *slog.Logger) *DealFacts {
	if facts == nil {
		return nil
	}
	normSource := stripSpaces(source)
	check := func(name string, f *QuotedFact) {
		verifyQuotedFact("mail→deal: 거래 조건 인용 검증 실패로 드롭", name, f, normSource, logger)
	}
	check("capacityMW", &facts.CapacityMW)
	check("unitPrice", &facts.UnitPrice)
	check("paymentTerms", &facts.PaymentTerms)
	check("warranty", &facts.Warranty)
	check("delayPenalty", &facts.DelayPenalty)
	if facts.Empty() {
		return nil
	}
	return facts
}

// spaceStripper matches every Unicode whitespace rune (quote matching must
// survive OCR/LLM re-spacing, especially around CJK).
var spaceStripper = regexp.MustCompile(`\s+`)

func stripSpaces(s string) string {
	return spaceStripper.ReplaceAllString(s, "")
}

// digitRunRe extracts digit runs after comma removal, so "2,940kW" and
// "2940kW" carry the same runs.
var digitRunRe = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// digitsCoveredBy reports whether every digit run in value appears verbatim in
// quote (both comma-normalized). A value with no digits (rare — e.g. "협의 중")
// passes; provenance is already covered by the quote-in-source check.
func digitsCoveredBy(value, quote string) bool {
	normQuote := strings.ReplaceAll(stripSpaces(quote), ",", "")
	for _, run := range digitRunRe.FindAllString(strings.ReplaceAll(value, ",", ""), -1) {
		if !strings.Contains(normQuote, run) {
			return false
		}
	}
	return true
}
