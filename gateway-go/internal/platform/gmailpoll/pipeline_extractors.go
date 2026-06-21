// pipeline_extractors.go — local-AI extractors over the finished analysis
// text: wiki fact proposals, operator action items, and deal info from
// business documents. All JSON-mode calls to the lightweight model.
package gmailpoll

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// --- wiki fact extraction (local AI) ---

const factExtractorSystem = `당신은 이메일 분석에서 위키에 기록할 만한 사실을 추출하는 추출기입니다.
사람·조직·거래·프로젝트·결정·기한·금액 등 "다음에 이 분석을 다시 볼 때 알고 싶을 사실"만 뽑습니다.
잡담·인사·일반론은 제외합니다.
반드시 JSON으로만 응답하세요.`

const factExtractorPrompt = `다음 이메일 분석에서 위키에 기록할 만한 사실을 추출해주세요.

JSON 응답 형식:
{
  "facts": [
    {"entity": "엔티티 이름 (인물·회사·프로젝트·거래)", "type": "person|org|project|deal|decision|deadline", "fact": "기록할 사실 한 문장"}
  ]
}

추출 기준:
- 새로 알게 된 구체적 사실만 (자명한 일반 정보는 제외)
- 이름·숫자·날짜 포함
- 최대 6개
- 기록할 사실이 없으면 facts 배열을 비워서 응답

## 분석 결과
%s`

const actionExtractorSystem = `당신은 이메일 분석에서 "수신자가 반드시 본인 손으로 처리해야 할 일"만 뽑는 추출기입니다.
수신자는 실무를 조직(팀·담당자)에 위임하는 고위 임원(전무·실장·대표)입니다. 부하 직원이 대신 할 수 있는 실무는 절대 넣지 말고, 임원 본인만 할 수 있는 일(최종 결재·승인, 본인이 내려야 할 의사결정, 본인 자격의 외부 약속, 본인에게 직접 온 판단 요청)만 추출합니다.
애매하면 넣지 마세요(빈 배열). 임원이 직접 할 일은 원래 적습니다 — 과다 추출보다 누락이 낫습니다.
반드시 JSON으로만 응답하세요.`

const actionExtractorPrompt = `다음 이메일 분석에서 임원 본인이 직접 처리해야 할 일만 추출해주세요.

JSON 응답 형식:
{
  "actions": [
    {"title": "할 일 (명령형 한 문장)", "dueHint": "기한·시각 단서 — 회의·통화 등 시각이 있으면 함께 (예: 내일, 3일 후, 6월 15일, 6월 15일 14:00, 내일 오후 2시 / 없으면 빈 문자열)", "priority": "high|medium|low"}
  ]
}

포함 (임원 본인만 할 수 있는 일):
- 최종 결재·승인 (본인 서명/재가가 필요한 것)
- 본인이 내려야 할 의사결정·방향 지시
- 대표·임원 자격의 외부 약속·미팅·서명
- 수신자 본인 앞으로 직접 온 판단·회신 요청

제외 (팀·담당자가 위임받아 처리할 실무 — 임원이 직접 해야 한다고 명시되지 않는 한 넣지 않음):
- 자료·문서 준비, 데이터 정리, 견적/계산 작업, 단순 회신, 실무 검토, 일정 조율, 일반 사무
- 단순 안내·참고 정보, 상대방이 할 일, 이미 끝난 일

기타:
- priority: 마감 임박·고액·계약/결재 관련만 high
- 최대 3개. 임원의 직접 할 일은 보통 0~2개다. 본인 몫이 아니면 비운다.
- 해당 없으면 actions 배열을 비워서 응답

## 분석 결과
%s`

const dealExtractorSystem = `당신은 업무 문서(견적서·계약서·세금계산서·거래명세서·발주서 등)에서 거래 정보를 뽑는 추출기입니다.
첨부 문서가 거래 문서일 때만 필드를 채우고, 일반 메일·뉴스레터·안내문이면 isDeal=false 로 응답합니다.
반드시 JSON으로만 응답하세요.`

const dealExtractorPrompt = `다음 이메일 분석(첨부 문서 내용 포함)에서 거래 정보를 추출해주세요.

JSON 응답 형식:
{
  "isDeal": true,
  "counterparty": "거래처/회사명",
  "docType": "견적서|계약서|세금계산서|거래명세서|발주서|기타",
  "amount": "총 금액 (예: 5,000,000원). 없으면 빈 문자열",
  "date": "문서 일자 (YYYY-MM-DD 또는 원문 그대로). 없으면 빈 문자열",
  "dueDate": "납기·결제 기한 (YYYY-MM-DD). 없으면 빈 문자열",
  "items": ["주요 품목/항목"],
  "summary": "거래 한 줄 요약"
}

추출 기준:
- 거래 문서가 아니면 {"isDeal": false} 만 응답
- counterparty(거래처명)를 못 찾으면 isDeal=false
- 금액·일자·기한은 원문에 있는 값만, 추측 금지
- 금액·품목·납기는 분석 요약보다 첨부 원문(## 첨부 내용)의 값을 우선해 그대로 적는다 (요약이 반올림했어도 원문 수치 보존)

## 분석 결과
%s`

// WikiFactProposal is a single fact suggested for wiki write-back.
type WikiFactProposal struct {
	Entity string `json:"entity"`
	Type   string `json:"type"`
	Fact   string `json:"fact"`
}

// wikiFactsBundle is the JSON-mode response wrapper. Local LLM JSON mode
// requires an object root; this carries the fact array.
type wikiFactsBundle struct {
	Facts []WikiFactProposal `json:"facts"`
}

// wikiFactsSchema is the strict json_schema for wikiFactsBundle — guided
// decoding pins the fact `type` to the known set. Keep in sync with the structs.
var wikiFactsSchema = json.RawMessage(`{
  "name": "wiki_facts",
  "strict": true,
  "schema": {
    "type": "object",
    "properties": {
      "facts": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "entity": {"type": "string"},
            "type": {"type": "string", "enum": ["person", "org", "project", "deal", "decision", "deadline"]},
            "fact": {"type": "string"}
          },
          "required": ["entity", "type", "fact"],
          "additionalProperties": false
        }
      }
    },
    "required": ["facts"],
    "additionalProperties": false
  }
}`)

// ActionItem is a single follow-up the operator should take, extracted from a
// mail analysis. Priority is "high"|"medium"|"low"; DueHint is a free-text
// Korean/relative due cue ("내일", "3일 후", "6월 15일") the server resolves to
// a date (empty when the mail gives no deadline).
type ActionItem struct {
	Title    string `json:"title"`
	DueHint  string `json:"dueHint"`
	Priority string `json:"priority"`
}

// actionItemsBundle is the JSON-mode response wrapper (object root required).
type actionItemsBundle struct {
	Actions []ActionItem `json:"actions"`
}

// actionItemsSchema is the strict json_schema for actionItemsBundle. The
// headline win of guided decoding here: `priority` is pinned to high|medium|low,
// so the model can't emit "urgent"/"높음"/"긴급" and silently miss the downstream
// high-priority calendar-proposal gate (normalizeActionPriority stays as a
// belt-and-suspenders backstop). Keep in sync with the structs.
var actionItemsSchema = json.RawMessage(`{
  "name": "action_items",
  "strict": true,
  "schema": {
    "type": "object",
    "properties": {
      "actions": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "title": {"type": "string"},
            "dueHint": {"type": "string"},
            "priority": {"type": "string", "enum": ["high", "medium", "low"]}
          },
          "required": ["title", "dueHint", "priority"],
          "additionalProperties": false
        }
      }
    },
    "required": ["actions"],
    "additionalProperties": false
  }
}`)

// DealInfo is a structured business-document extraction (견적서/계약서/세금계산서
// 등) from a mail attachment. All fields except Counterparty are optional.
type DealInfo struct {
	Counterparty string
	DocType      string
	Amount       string
	Date         string
	DueDate      string
	Items        []string
	Summary      string
}

// dealExtract is the local-LLM JSON-mode response. IsDeal lets the model say
// "this attachment is not a business document" without us guessing from empty
// fields.
//
// Stays on plain json_object (no strict json_schema, unlike the facts/actions
// extractors): its eight free-text string fields are the worst case for vLLM
// xgrammar's whitespace-explosion bug under strict guided decoding — the model
// degenerates into an unbounded space run as a string value and truncates the
// JSON (~⅓ of the time in live probing; maxLength/disable_any_whitespace don't
// bound it on this build). json_object is explosion-free here, and docType's
// only consumer is a wiki label (low value), so the enum guarantee isn't worth
// the regression.
type dealExtract struct {
	IsDeal       bool     `json:"isDeal"`
	Counterparty string   `json:"counterparty"`
	DocType      string   `json:"docType"`
	Amount       string   `json:"amount"`
	Date         string   `json:"date"`
	DueDate      string   `json:"dueDate"`
	Items        []string `json:"items"`
	Summary      string   `json:"summary"`
}

// extractFactsForWiki runs a local-AI extractor over the final analysis text
// and returns a pre-formatted Markdown block ready to append to the analyze
// output. The agent then writes each fact to wiki per the "분석 → 위키 갱신"
// system-prompt nudge.
//
// Best-effort: empty string when local AI is unavailable, extraction fails,
// or no qualifying facts are found.
func extractFactsForWiki(ctx context.Context, deps PipelineDeps, analysisText string) string {
	if deps.LocalClient == nil || deps.LocalModel == "" {
		return ""
	}
	if strings.TrimSpace(analysisText) == "" {
		return ""
	}

	extractCtx, cancel := context.WithTimeout(ctx, stage1Timeout)
	defer cancel()

	prompt := fmt.Sprintf(factExtractorPrompt, analysisText)
	bundle, err := callLocalLLMJSON[wikiFactsBundle](extractCtx, deps.LocalClient, deps.LocalModel, factExtractorSystem, prompt, stage1MaxTokens, wikiFactsSchema)
	if err != nil || len(bundle.Facts) == 0 {
		return ""
	}
	return renderFactsBlock(bundle.Facts)
}

// renderFactsBlock formats a slice of WikiFactProposal as the Markdown block
// appended to the analyze output. Returns "" when no fact has both an entity
// and a fact (so the analyze output stays clean if extraction yields noise).
func renderFactsBlock(facts []WikiFactProposal) string {
	var sb strings.Builder
	sb.WriteString("📝 위키 갱신 제안 (자동 추출):\n")
	rendered := 0
	for _, f := range facts {
		entity := strings.TrimSpace(f.Entity)
		fact := strings.TrimSpace(f.Fact)
		if entity == "" || fact == "" {
			continue
		}
		typ := strings.TrimSpace(f.Type)
		if typ != "" {
			fmt.Fprintf(&sb, "- **%s** (%s): %s\n", entity, typ, fact)
		} else {
			fmt.Fprintf(&sb, "- **%s**: %s\n", entity, fact)
		}
		rendered++
	}
	if rendered == 0 {
		return ""
	}
	return strings.TrimSpace(sb.String())
}

// extractActionItems runs the local-AI extractor over the final analysis text
// and returns the operator's follow-up actions. Best-effort: nil when local AI
// is unavailable, extraction fails, or nothing qualifies. Mirrors
// extractFactsForWiki — same local model, same stage-1 budget.
func extractActionItems(ctx context.Context, deps PipelineDeps, analysisText string) []ActionItem {
	if deps.LocalClient == nil || deps.LocalModel == "" {
		return nil
	}
	if strings.TrimSpace(analysisText) == "" {
		return nil
	}

	extractCtx, cancel := context.WithTimeout(ctx, stage1Timeout)
	defer cancel()

	prompt := fmt.Sprintf(actionExtractorPrompt, analysisText)
	bundle, err := callLocalLLMJSON[actionItemsBundle](extractCtx, deps.LocalClient, deps.LocalModel, actionExtractorSystem, prompt, stage1MaxTokens, actionItemsSchema)
	if err != nil {
		return nil
	}
	return sanitizeActionItems(bundle.Actions)
}

// sanitizeActionItems drops empty-title items, trims fields, normalizes
// priority to high|medium|low, and caps the list so a runaway extraction can't
// flood the to-do list. The cap is low (3): the recipient is a delegating
// executive, so a single mail rarely warrants more than a couple of personal
// action items — the prompt asks for the same, and this is the hard backstop.
func sanitizeActionItems(in []ActionItem) []ActionItem {
	const maxActions = 3
	out := make([]ActionItem, 0, len(in))
	for _, a := range in {
		title := strings.TrimSpace(a.Title)
		if title == "" {
			continue
		}
		out = append(out, ActionItem{
			Title:    title,
			DueHint:  strings.TrimSpace(a.DueHint),
			Priority: normalizeActionPriority(a.Priority),
		})
		if len(out) >= maxActions {
			break
		}
	}
	return out
}

// normalizeActionPriority maps assorted model outputs onto high|medium|low,
// defaulting to medium so an unrecognized label never silently becomes a
// high-priority auto-to-do.
func normalizeActionPriority(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high", "urgent", "높음", "긴급":
		return "high"
	case "low", "낮음":
		return "low"
	default:
		return "medium"
	}
}

// extractDealInfo runs the local-AI extractor over the analysis text and
// returns structured deal data, or nil when the mail carries no recognizable
// business document. The analysis text includes the relevant attachments'
// content when the attachment gate selected any (synthesizeAnalysis →
// gateAndExtractAttachments), so for a 견적서 PDF this reads the document's real
// figures rather than just the body. Best-effort; mirrors extractFactsForWiki —
// same local model, same stage-1 budget. Callers gate this on attachment
// presence so it doesn't fire on every plain mail.
func extractDealInfo(ctx context.Context, deps PipelineDeps, analysisText string) *DealInfo {
	if deps.LocalClient == nil || deps.LocalModel == "" {
		return nil
	}
	if strings.TrimSpace(analysisText) == "" {
		return nil
	}

	extractCtx, cancel := context.WithTimeout(ctx, stage1Timeout)
	defer cancel()

	prompt := fmt.Sprintf(dealExtractorPrompt, analysisText)
	// json_object (schema=nil): the deal schema is wide free-text and triggers the
	// xgrammar whitespace explosion under strict mode — see the dealExtract doc.
	ext, err := callLocalLLMJSON[dealExtract](extractCtx, deps.LocalClient, deps.LocalModel, dealExtractorSystem, prompt, stage1MaxTokens, nil)
	if err != nil {
		return nil
	}
	// Pass the same source the extractor read so the amount gate can corroborate
	// the figure against the original document text. deps.Logger may be nil in
	// tests / minimal wiring — the gate handles that.
	return dealInfoFromExtract(ext, analysisText, deps.Logger)
}

// dealInfoFromExtract is the pure post-processing of a deal extraction: returns
// nil when it isn't a deal or has no counterparty, otherwise trims fields and
// drops empty items. Split out so it can be tested without a local LLM.
//
// Amount verification gate (FinAcumen ③, deterministic + LLM-free): the Amount
// comes from RoleTiny (the smallest model) and gets frozen onto a wiki deal page
// + pinned as citable notebook evidence, so a hallucinated figure becomes a
// "fact". Mirroring parseRelatedProjects (which drops LLM project paths absent
// from the candidate set while keeping the rest), we drop a non-corroborated
// AMOUNT while preserving the rest of the deal. Source is the same text fed to
// the extractor (clean analysis + verbatim attachment); logger may be nil.
func dealInfoFromExtract(ext dealExtract, source string, logger *slog.Logger) *DealInfo {
	counterparty := strings.TrimSpace(ext.Counterparty)
	if !ext.IsDeal || counterparty == "" {
		return nil
	}
	items := make([]string, 0, len(ext.Items))
	for _, it := range ext.Items {
		if t := strings.TrimSpace(it); t != "" {
			items = append(items, t)
		}
	}
	deal := &DealInfo{
		Counterparty: counterparty,
		DocType:      strings.TrimSpace(ext.DocType),
		Amount:       strings.TrimSpace(ext.Amount),
		Date:         strings.TrimSpace(ext.Date),
		DueDate:      strings.TrimSpace(ext.DueDate),
		Items:        items,
		Summary:      strings.TrimSpace(ext.Summary),
	}
	gateDealAmount(deal, source, logger)
	return deal
}

// gateDealAmount verifies deal.Amount against the source text by integer
// equivalence and mutates deal in place when the figure is not corroborated.
//
// Decisions (over-block avoidance is the priority — a parse we can't trust never
// blanks a possibly-correct amount):
//   - empty Amount            → no-op (the prompt explicitly allows blank).
//   - Amount unparseable       → keep as-is (over-block guard; we can't claim it's wrong).
//   - Amount found in source   → keep as-is (corroborated).
//   - Amount NOT in source     → blank Amount + append a visible ⚠️ flag to Summary
//     and Warn-log. Never silent (this codebase's repeated lesson): the operator
//     and the log both see that the extracted figure failed source corroboration.
func gateDealAmount(deal *DealInfo, source string, logger *slog.Logger) {
	if deal.Amount == "" {
		return
	}
	found, parsed := amountFoundInSource(deal.Amount, source)
	if !parsed || found {
		return // over-block guard, or corroborated → keep the amount
	}
	bad := deal.Amount
	deal.Amount = ""
	flag := "⚠️ 금액 원문 대조 실패 (추출: " + bad + ")"
	if deal.Summary == "" {
		deal.Summary = flag
	} else {
		deal.Summary = deal.Summary + " " + flag
	}
	if logger != nil {
		logger.Warn("mail→deal: 추출 금액이 원문에 없어 비움",
			"counterparty", deal.Counterparty, "extractedAmount", bad)
	}
}
