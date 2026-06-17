// pipeline_extractors.go — local-AI extractors over the finished analysis
// text: wiki fact proposals, operator action items, and deal info from
// business documents. All JSON-mode calls to the lightweight model.
package gmailpoll

import (
	"context"
	"fmt"
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

const actionExtractorSystem = `당신은 이메일 분석에서 "받는 사람이 직접 해야 할 후속 행동"만 뽑는 추출기입니다.
단순 정보·참고 사항·상대방이 할 일은 제외하고, 운영자 본인의 실행 항목만 추출합니다.
반드시 JSON으로만 응답하세요.`

const actionExtractorPrompt = `다음 이메일 분석에서 운영자가 직접 처리해야 할 후속 행동을 추출해주세요.

JSON 응답 형식:
{
  "actions": [
    {"title": "할 일 (명령형 한 문장)", "dueHint": "기한·시각 단서 — 회의·통화 등 시각이 있으면 함께 (예: 내일, 3일 후, 6월 15일, 6월 15일 14:00, 내일 오후 2시 / 없으면 빈 문자열)", "priority": "high|medium|low"}
  ]
}

추출 기준:
- 운영자 본인이 실행할 구체적 행동만 (회신·검토·결재·송금·일정확정·자료준비 등)
- 단순 안내·상대 담당 업무·이미 끝난 일은 제외
- priority: 마감 임박·금액 큼·계약/결재 관련은 high
- 최대 5개
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
	bundle, err := callLocalLLMJSON[wikiFactsBundle](extractCtx, deps.LocalClient, deps.LocalModel, factExtractorSystem, prompt, stage1MaxTokens)
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
	bundle, err := callLocalLLMJSON[actionItemsBundle](extractCtx, deps.LocalClient, deps.LocalModel, actionExtractorSystem, prompt, stage1MaxTokens)
	if err != nil {
		return nil
	}
	return sanitizeActionItems(bundle.Actions)
}

// sanitizeActionItems drops empty-title items, trims fields, normalizes
// priority to high|medium|low, and caps the list so a runaway extraction can't
// flood the to-do list.
func sanitizeActionItems(in []ActionItem) []ActionItem {
	const maxActions = 5
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
	ext, err := callLocalLLMJSON[dealExtract](extractCtx, deps.LocalClient, deps.LocalModel, dealExtractorSystem, prompt, stage1MaxTokens)
	if err != nil {
		return nil
	}
	return dealInfoFromExtract(ext)
}

// dealInfoFromExtract is the pure post-processing of a deal extraction: returns
// nil when it isn't a deal or has no counterparty, otherwise trims fields and
// drops empty items. Split out so it can be tested without a local LLM.
func dealInfoFromExtract(ext dealExtract) *DealInfo {
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
	return &DealInfo{
		Counterparty: counterparty,
		DocType:      strings.TrimSpace(ext.DocType),
		Amount:       strings.TrimSpace(ext.Amount),
		Date:         strings.TrimSpace(ext.Date),
		DueDate:      strings.TrimSpace(ext.DueDate),
		Items:        items,
		Summary:      strings.TrimSpace(ext.Summary),
	}
}
