// dreamer_apply.go — synthesis and application of a dream cycle: the LLM
// call that turns diary content into wikiUpdate proposals (synthesize) and
// the apply pass that writes/merges pages, rebuilds the index, and merges
// tags/related lists. Split from dreamer.go (WikiDreamer core).
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

type flexStringList []string

// UnmarshalJSON decodes the supported flexible JSON representation.
func (f *flexStringList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*f = nil
		return nil
	}
	switch trimmed[0] {
	case '[':
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*f = arr
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = splitFlexList(s)
	default:
		return fmt.Errorf("flexStringList: expected JSON array or string, got %.40s", trimmed)
	}
	return nil
}

// splitFlexList breaks a delimited string into trimmed, non-empty elements.
func splitFlexList(s string) flexStringList {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make(flexStringList, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// wikiUpdate represents a single page update instruction from the LLM.
type wikiUpdate struct {
	Action     string         `json:"action"` // "create" or "update"
	Path       string         `json:"path"`   // e.g., "업무/dgx-spark.md"
	Title      string         `json:"title"`
	ID         string         `json:"id"`      // short kebab-case identifier (e.g., "dgx-spark")
	Code       string         `json:"code"`    // optional dept-client-dtype stem for a NEW project; Go assigns the 순번
	Summary    string         `json:"summary"` // one-line description (~80 chars)
	Category   string         `json:"category"`
	Tags       flexStringList `json:"tags"`
	Related    flexStringList `json:"related"` // existing page paths semantically related
	Content    string         `json:"content"` // markdown body or section to append
	Importance float64        `json:"importance"`
	Type       string         `json:"type"`       // concept, entity, source, comparison, log
	Confidence string         `json:"confidence"` // high, medium, low
	Due        string         `json:"due"`        // YYYY-MM-DD upcoming deadline (프로젝트, 거래성 건)
	Supersedes flexStringList `json:"supersedes"` // relPath(s) of existing page(s) this update REPLACES; accepts a string or an array (the LLM emits both, and an array used to fail synthesis parsing)
	Resource   string         `json:"resource"`   // OKF resource: stable URI/id of the concept's underlying asset (gmail thread, deal ref, calendar event, file path); empty for abstract concepts
	Cues       flexStringList `json:"cues"`       // recall entry points: alternate Korean phrasings a future query might use (synonyms/aliases/question forms NOT already on the page) — indexed for search, never rendered as content
	Client     string         `json:"client"`     // 거래처 — canonical single-level 계열사 name (기아·금호타이어); digest grouping + recall anchor key — 프로젝트 대표페이지 전용. Fill-only on update: an operator-set value is never overwritten
	Sites      flexStringList `json:"sites"`      // 프로젝트 현장 (canonical "광역약칭 시/군 읍/면/동 [리]"); matching keys for recall anchor + meeting harvest — 프로젝트 대표페이지 전용
	Kinds      flexStringList `json:"kinds"`      // 프로젝트 특성 2단 enum ("1차" 또는 "1차/2차" — page.go:projectKinds, 복수) — 대표페이지 전용
}

// parseWikiUpdates parses the synthesis response array leniently: one malformed
// update is skipped (logged), not fatal. The all-or-nothing alternative failed
// the whole batch on a single bad field and — if the malformation was
// deterministic (the #2341 supersedes case) — re-failed every cycle, stalling
// the diary pipeline. Returns an error only when the response is not a JSON
// array at all (a genuine total failure worth backing off on).
// partial is true when the response array was damaged and only a salvaged
// prefix was applied — the tail's facts were not consumed, so the caller can
// hold the diary offsets for re-consumption next cycle.
func parseWikiUpdates(text string, logger *slog.Logger) (updates []wikiUpdate, partial bool, err error) {
	var rawItems []json.RawMessage
	if uerr := json.Unmarshal([]byte(text), &rawItems); uerr != nil {
		// Damaged array — a mid-string truncation (output budget) or a stray
		// unescaped character inside a Korean value (observed 2026-07-03:
		// "invalid character 'ì' after object key:value pair") used to zero
		// the whole cycle and back off 8h. Salvage every complete element
		// before the damage point instead; only a response that is not a JSON
		// array at all still fails (worth backing off on). A COMPLETE array
		// followed by trailing junk (which also fails the strict Unmarshal) is
		// not damage — nothing was lost, so it must not report partial, or the
		// caller would hold diary offsets and re-consume the cycle for nothing.
		salvaged, complete := salvageJSONArrayPrefix(text)
		if len(salvaged) == 0 && !complete {
			return nil, false, uerr
		}
		if logger != nil {
			if complete {
				logger.Warn("wiki-dream: synthesis array carried trailing junk; using the complete array",
					"error", uerr, "items", len(salvaged))
			} else {
				logger.Warn("wiki-dream: synthesis array damaged; applying salvaged prefix",
					"error", uerr, "salvaged", len(salvaged))
			}
		}
		rawItems = salvaged
		partial = !complete
	}
	updates = make([]wikiUpdate, 0, len(rawItems))
	skipped := 0
	for _, item := range rawItems {
		var u wikiUpdate
		if err := json.Unmarshal(item, &u); err != nil {
			skipped++
			if logger != nil {
				logger.Warn("wiki-dream: skipped malformed update item",
					"error", err, "raw", fmt.Sprintf("%.200s", string(item)))
			}
			continue
		}
		updates = append(updates, u)
	}
	if skipped > 0 && logger != nil {
		logger.Warn("wiki-dream: synthesis dropped malformed items",
			"skipped", skipped, "applied", len(updates))
	}
	return updates, partial, nil
}

// salvageJSONArrayPrefix decodes complete elements off the front of a JSON
// array until the first syntax error and returns them (nil when the text is
// not an array or the very first element is already damaged). Elements after
// the damage point are unrecoverable by construction — the parser cannot
// resynchronize on free-form JSON — and losing the tail of one cycle is
// strictly better than losing the cycle. complete is true when the array's
// closing bracket was reached — i.e. every element decoded and only trailing
// junk after the array made the strict whole-text Unmarshal fail.
func salvageJSONArrayPrefix(text string) (items []json.RawMessage, complete bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, false
	}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return items, false
		}
		items = append(items, raw)
	}
	if tok, err := dec.Token(); err == nil {
		if d, ok := tok.(json.Delim); ok && d == ']' {
			complete = true
		}
	}
	return items, complete
}

// synthesize calls the LLM to determine which wiki pages should be updated.
func (wd *WikiDreamer) synthesize(ctx context.Context, diaryContent string, state diaryProcessState) ([]wikiUpdate, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, wikiDreamSynthesisTimeout)
	defer cancel()

	// Build existing wiki context. Snapshot before rendering — Render walks the
	// entry map, which writers mutate in place under the store lock.
	indexContent := wd.store.SnapshotIndex().Render()
	processedHistory := formatProcessedDiaryCapsules(state.Recent)

	polarisSection := ""
	if wd.polarisContextFn != nil {
		if ctx := wd.polarisContextFn(); ctx != "" {
			polarisSection = "\n## 최근 Polaris 압축 요약 (사전 추출된 사실)\n" + ctx + "\n"
		}
	}

	// Operator steering (WIKI.md): re-read every cycle so a brief edit takes
	// effect on the very next dream without a restart (see brief.go).
	briefSection := WikiBriefSection(LoadWikiBrief(wd.workspaceDir))

	// 효용 접지: bias synthesis toward the pages that actually get recalled, so
	// new facts attach to living knowledge rather than cold pages (recall_hits.go).
	anchorSection := wd.formatRecalledAnchors(time.Now())

	// Rules block is externalizable (P1 절차 외부화): re-read every cycle so a
	// slow-loop/operator edit lands on the next dream without a restart.
	rules := wd.loadWikiSynthesisRules()
	prompt := buildWikiSynthesisPromptWithRules(rules, indexContent, processedHistory, polarisSection, briefSection, anchorSection, diaryContent)

	resp, err := wd.client.Complete(ctx,
		wd.llmRequest("You are a wiki knowledge base maintainer. Respond only with a JSON array.", prompt, wd.synthesisBudget()))
	if err != nil {
		return nil, false, fmt.Errorf("LLM call: %w", err)
	}

	// Extract JSON from response.
	text := resp
	text = strings.TrimSpace(text)

	// Strip markdown code fences if present.
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	updates, partial, err := parseWikiUpdates(text, wd.logger)
	if err != nil {
		return nil, false, fmt.Errorf("parse LLM response: %w (raw: %.200s)", err, text)
	}

	// Defense in depth: even if Site 1 (transcript) redacted raw tool output,
	// the LLM may still paraphrase or quote a secret into its wiki synthesis
	// ("the user's API key starts with sk-proj…"). Redact every free-text
	// field on the proposed updates before they flow into the store.
	for i := range updates {
		updates[i].Title = redact.String(updates[i].Title)
		updates[i].Summary = redact.String(updates[i].Summary)
		updates[i].Content = redact.String(updates[i].Content)
	}

	return updates, partial, nil
}

func buildWikiSynthesisPrompt(indexContent, processedHistory, polarisSection, briefSection, anchorSection, diaryContent string) string {
	return buildWikiSynthesisPromptWithRules(defaultWikiSynthesisRules,
		indexContent, processedHistory, polarisSection, briefSection, anchorSection, diaryContent)
}

// buildWikiSynthesisPromptWithRules assembles the synthesis prompt with a
// caller-supplied rules block. The dynamic sections (index/history/anchors/
// diary) stay in Go; only the tunable rules text is externalizable, so a
// slow-loop or operator can evolve the synthesis policy without a rebuild
// (loadWikiSynthesisRules). buildWikiSynthesisPrompt passes the built-in default.
func buildWikiSynthesisPromptWithRules(rules, indexContent, processedHistory, polarisSection, briefSection, anchorSection, diaryContent string) string {
	return fmt.Sprintf(`당신은 위키 지식베이스 관리자입니다. 아래 일지 내용을 분석하여 위키 페이지를 생성하거나 업데이트할 지시사항을 JSON 배열로 반환하세요.

## 현재 위키 인덱스
%s

## 최근 처리 이력
%s
%s%s%s
## 새 일지 내용
%s

%s`, indexContent, processedHistory, polarisSection, briefSection, anchorSection, diaryContent, rules)
}

// defaultWikiSynthesisRules is the built-in synthesis policy. An operator or the
// slow-loop can override it per deployment with a wiki-dream-rules.md file (see
// loadWikiSynthesisRules); the apply guards enforce structure regardless, so a
// bad override degrades synthesis quality but cannot corrupt the store.
const defaultWikiSynthesisRules = `## 규칙
- 일시적인 내용(인사, 잡담)은 무시
- 중요한 결정, 새로운 사실, 인물 정보, 프로젝트 진행 등만 위키에 반영
- 수요 우선순위: 챗 회상 수요의 대부분은 프로젝트 페이지다(회상-히트 원장 실측 ~86%). 일지에 프로젝트 관련 사실과 일반 지식이 섞여 있으면 프로젝트 반영을 우선하고, 기타(세상 지식·시사·잡학) 페이지는 지속 참조 가치가 분명할 때만 만들어라 — 일회성 브라우징 주제는 자료/일지로 충분하다
- 기존 페이지가 있으면 action:"update", 없으면 action:"create"
- 최근 처리 이력에 이미 반영된 주제/경로는 새 사실이 추가된 경우에만 update하고, 같은 내용을 반복 생성하지 마라
- 지식은 저장만 하지 말고 연결·정리하라. 다음 세션이 다시 추론하지 않도록 상호링크·모순 표시·정리 위치를 함께 결정한다
- 카테고리는 반드시 다음 6개 중 하나 (경로의 첫 디렉토리 = 카테고리):
  - 프로젝트: 진행 중인 일·거래·결정 — 거래처/금액/납기가 걸린 건, 의사결정 근거 등 특정 건별 컨텍스트
  - 인물: 사람·조직 — 연락처, 관계, 담당자, 거래처 인물
  - 시스템: Deneb 자신의 구성·운영 — 서버/모델/배포/도구 설정
  - 업무: 직무 도메인 지식 — 태양광·전선·구리값 등 일에 직접 쓰이는 지식
  - 사용자: 사용자 개인 — 선호, 톤·스타일 규칙, 개인 컨텍스트
  - 기타: 그 외 일반/세상 지식 — 국제정세·시사·잡학 등 위 분류에 안 맞는 것 (catch-all)
- 프로젝트 카테고리의 문서 구조 (프로젝트당 폴더 하나, 정해진 슬롯만 사용):
  - 프로젝트/<프로젝트명>/대표.md — 대표페이지 (현재 상태·개요·핵심 사실). 새 프로젝트 create는 반드시 이 경로
  - 프로젝트/<프로젝트명>/로그.md — 진행 로그. 회의·결재·발주·법무검토 같은 **사건·진행 소식은 새 페이지를 만들지 말고** 이 페이지에 action:"update"로 날짜와 함께 append. 섹션 제목은 '## [YYYY-MM-DD] <op> | <주제>' 꼴(op: 회의/결정/발주/이슈/ingest …) — grep 가능한 로그 문법
  - 프로젝트/<프로젝트명>/기자재/ — 케이블·모듈 등 기자재·자재 문서
  - 프로젝트/<프로젝트명>/메일분석/ — 메일 분석 원본 (시스템이 자동 생성; 직접 만들지 마라)
  - 프로젝트/<프로젝트명>/자료/ — 외부 소스(URL·영상) 캡처 (wiki ingest가 생성; 직접 만들지 마라. 인용은 [[링크]]로)
  - 프로젝트/<프로젝트명>/회의록/ — 회의 녹음 분석 (시스템이 자동 생성; 직접 만들지 마라. 인용은 [[링크]]로)
- 프로젝트 중 거래성 건(거래처·금액·납기)은 가장 임박한 결제기한/마감일을 frontmatter의 due 필드(YYYY-MM-DD)에 기록
- content는 마크다운 형식. create 시 전체 본문, update 시 추가할 섹션/내용. 본문에서 다른 페이지를 언급할 때는 [[경로-또는-제목]] 형식의 위키링크를 쓰면 지식그래프 엣지가 된다 (예: [[프로젝트/dgx-spark]], [[홍길동]])
- 상호링크: related에는 인덱스에 존재하는 관련 페이지 경로를 1~3개만 넣고, content에도 필요한 곳에 [[...]] 링크를 남겨라. 관련 페이지가 불확실하면 억지로 만들지 마라
- 출처 규율(부유 주장 금지): 특정 소스(메일분석·자료·일지 항목)에서 온 주장은 그 [[페이지]]를 문장에 병기하고, 소스 없이 당신이 종합한 추론 문장은 '> 합성:' 인용으로 시작해 구분하라 — 나중에 사실 검증이 가능해야 한다
- 모순/갱신: 새 일지 내용이 기존 페이지와 충돌하거나 대체하면 content에 "모순/갱신:" 섹션 또는 bullet로 날짜·근거·최신 기준을 적고 supersedes에 대체되는 기존 페이지 경로를 넣어라
- 지식 정리: 반복될 운영법·실패 회피법은 loose log 페이지를 늘리지 말고 기존 프로젝트/시스템/업무 페이지의 troubleshooting/recipe 성격 섹션에 병합하라. 새 페이지가 필요하면 반드시 6개 카테고리 아래에 둔다
- importance: 0.5(일반) ~ 0.9(핵심 결정)
- type: 페이지 유형 — concept(개념), entity(인물/조직), source(출처), comparison(비교), log(이력)
- confidence: 정보 신뢰도 — high(검증됨), medium(합리적 추론), low(불확실)
- due: 임박한 결제기한·마감일 (YYYY-MM-DD). 프로젝트의 거래성 건에서만 사용, 없으면 생략
- supersedes: 새 일지 내용이 기존 페이지의 사실과 **모순되거나 그것을 대체**할 때, 대체되는 기존 페이지 경로 (인덱스에서 선택). 단순 추가 정보면 생략 — 사실이 바뀐 경우에만 (예: 단가 변경, 담당자 교체, 정책 폐기)
- 사용자 모델(사용자 카테고리): 사용자가 어떤 사람인지의 **현행 프로필**을 축별 페이지로 유지하라 — ①소통(호칭·말투·답변 형식·길이) ②업무 리듬(시간대·루틴·보고 방식) ③도구·포맷 취향 ④판단·결정 성향(위험 감수·우선순위 기준) ⑤개인 컨텍스트(업무에 필요한 만큼만). 축 하나=페이지 하나로 작게 나누고, 각 규칙에 근거(날짜+발화/행동 요지)를 함께 남기고, cues를 채워라
- 사용자 working-style 추론(사용자 카테고리 한정): 명시적으로 말한 지속 선호("앞으로/항상/다음부터 …" — 일지 "신호:"의 **선호** 태그가 1차 단서)는 **1회로도 즉시 기록**하라(confidence=high). 행동에서 추론한 규칙(예: 답변을 산문으로 고침, 특정 맥락서 늘 숫자 요구, 특정 형식 반복 거부)은 **2회 이상 반복**이 분명할 때만 도출하고(confidence=medium 이하) 단발 행동·추측은 금지 — 운영자가 검토·정정하게 하라
- 사용자 선호 갱신(사용자 카테고리 한정): 기존 선호가 **바뀌면**(예: 간결→상세 전환) 모순 bullet을 누적하지 말고 action:"update"로 **그 값을 현재값으로 교체**하라(낡은 값 삭제) — 사용자 페이지는 이력 로그가 아니라 *현행 정책*이어야 한다. '이번만' 류 일회성 지시는 기록하지 마라. 페이지 전체가 무의미해진 경우에만 supersedes를 쓴다
- id: 짧은 kebab-case 식별자 (예: "dgx-spark", "gemma4-switch", "peter-kim")
- code: **새 프로젝트(거래)** 페이지를 처음 만들 때만, 고정코드 줄기 "[부서]-[고객]-[거래타입]" 을 제안 (순번은 시스템이 부여). 부서=pl0(실장 직할·오선택 직접)·pl1(1팀 사업개발)·pl2(2팀 루프탑·자가소비)·pl3(3팀 모듈·인버터)·nde(남도에코 케이블)·etc(타부서)·com(다부서). 거래타입=dev(개발)·epc(시공)·mod(모듈)·inv(인버터)·cbl(케이블)·bes(BESS)·wnd(풍력). 고객=거래상대 3자 약어 (트리나→tri, 기아→kia). 전 세그먼트 3자 고정. 기존 프로젝트의 하위 메일/이력 페이지는 code 생략 — 폴더에서 자동 상속됨
- summary: 한 줄 요약 (~80자, 한국어)
- related: 의미적으로 관련된 기존 위키 페이지 경로 목록 (인덱스에서 선택)
- resource: 이 개념의 근거가 되는 실제 자산의 안정 식별자/URI (예: gmail 스레드 id, 거래 ref, 캘린더 이벤트, 파일 경로). 다음 세션이 원본으로 바로 점프하게. 추상 개념이면 생략
- cues: 이 문서를 나중에 다시 찾을 때 질문에 나올 법한 **검색 진입 표현** 2~5개 (동의어·별칭·다른 관점의 명사 — 제목/본문/tags에 **이미 있는 단어는 넣지 마라**; 예: 본문이 "선수금"이면 cues는 ["계약금", "착수금"]). 검색 전용이라 본문에 안 보인다. 마땅한 게 없으면 생략
- client: **프로젝트 대표페이지 전용** — 거래처(발주처/계약 상대)를 계열사 단위 정식명 1개로 (예: "기아", "현대차", "LG전자", "금호타이어" — 그룹명·법인 접미어 ㈜ 금지). 프로젝트 위계의 최상단 그룹핑 축이다. 일지·메일에서 거래처가 확인된 신규 페이지에 기입; 이미 값이 있으면 시스템이 보존한다(덮어쓰기 안 됨). 자체 개발 등 거래처 없는 사업은 생략(추측 금지)
- sites: **프로젝트 대표페이지 전용** — 현장 위치를 고정 규칙 "광역약칭 시/군 읍/면/동 [리]"로 (예: "전북 군산시 옥구읍 수산리"; 공백 구분·번지/마침표 금지·광역은 약칭). 일지·메일에서 현장이 확인되면 기입/갱신, 복수 현장은 배열로. 불확실하면 생략(추측 금지)
- kinds: **프로젝트 대표페이지 전용** — 2단 고정 체계 "1차" 또는 "1차/2차" (복수 허용): 태양광(발전소 사업 — 시공·개발·인허가 포함; 2차: 토지/루프탑/수상/ESS — ESS 사업도 태양광), 기자재(2차: 모듈/인버터/케이블/기타), 풍력(2차: 육상/해상), 기타(2차: 용역/협력). 2차를 모르면 1차만, 확인되면 세분화. 어휘 밖 값은 무시됨
- stage: **프로젝트 대표페이지 전용** — 사업 단계를 고정 어휘 하나로: 제안 → 견적 → 입찰 → 개발 → 계약협의 → 시공/납품 → 운영 (진행 순), 종결/유실 (말단). 개발=자체개발 인허가·부지 확보 단계. 시공과 납품은 병렬 트랙: 현장 사업은 시공, 기자재(모듈·인버터·케이블 등 조달) 건은 계약 이행을 **납품**으로 쓴다(시공 아님). 일지·메일 신호로 단계 진행이 확인되면 갱신 (예: "계약서 검토 요청" 수신 → 계약협의, 기자재 선적/출하 → 납품). 어휘 밖 값은 무시됨. 불확실하면 생략(추측 금지)
- 현장 상세 문서 게이트: 대표페이지의 현장 상세 섹션(주소 목록·현장 스펙 문단)과 write-site 현장 페이지는 stage가 **개발 또는 계약협의 이상**(개발/계약협의/시공/운영, 또는 기존 O&M 자산)일 때만 작성하라 — 자체개발은 부지가 사업의 본체라 개발 단계부터 현장 문서가 정당하다. 영업 단계(제안·견적·입찰)는 sites 메타데이터 한 줄까지만 — 현장 원본 정보는 메일분석 페이지가 이미 보존한다 (운영자 확정 2026-07-19)
- 업데이트가 불필요하면 빈 배열 [] 반환

JSON 배열만 반환하세요. 다른 텍스트 없이.`

// wikiDreamRulesFile is the optional per-deployment override for the synthesis
// rules block, read from the workspace dir each cycle (like WIKI.md brief) so a
// slow-loop or operator edit takes effect on the next dream without a restart.
const wikiDreamRulesFile = "wiki-dream-rules.md"

// loadWikiSynthesisRules returns the operator/slow-loop rules override when a
// non-empty wiki-dream-rules.md exists in the workspace dir, else the built-in
// default. Empty workspaceDir or any read error falls back to the default —
// synthesis must never run with an empty rules block.
func (wd *WikiDreamer) loadWikiSynthesisRules() string {
	if wd.workspaceDir == "" {
		return defaultWikiSynthesisRules
	}
	data, err := os.ReadFile(filepath.Join(wd.workspaceDir, wikiDreamRulesFile))
	if err != nil {
		return defaultWikiSynthesisRules
	}
	if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
		return trimmed
	}
	return defaultWikiSynthesisRules
}

// recallAnchorLimit caps how many "living" anchors the synthesis hint lists.
const recallAnchorLimit = 8

// formatRecalledAnchors renders the pages pulled into chat turns most often in
// the score window as a synthesis hint. The dreamer should connect new facts to
// knowledge that gets used, not spawn cold pages beside it. Empty when the
// recall-utility ledger has nothing yet (early cycles) — the section then
// vanishes from the prompt rather than showing a hollow header.
func (wd *WikiDreamer) formatRecalledAnchors(now time.Time) string {
	counts := wd.store.RecallHitScoreCounts(now)
	if len(counts) == 0 {
		return ""
	}
	type anchor struct {
		path string
		n    int
	}
	ranked := make([]anchor, 0, len(counts))
	for p, n := range counts {
		ranked = append(ranked, anchor{p, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].n != ranked[j].n {
			return ranked[i].n > ranked[j].n
		}
		return ranked[i].path < ranked[j].path // stable tie-break
	})
	var sb strings.Builder
	shown := 0
	for _, a := range ranked {
		if shown >= recallAnchorLimit {
			break
		}
		page, err := wd.store.ReadPage(a.path)
		if err != nil || page == nil || page.Meta.Archived {
			continue // recalled path since deleted/archived — skip
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(a.path), ".md")
		}
		fmt.Fprintf(&sb, "- [[%s]] %s (회상 %d회)\n", a.path, title, a.n)
		shown++
	}
	if shown == 0 {
		return ""
	}
	return "\n## 자주 회상된 앵커 (실제 쓰이는 지식 — 새 사실을 여기에 연결하고 중복 생성 대신 갱신하라)\n" +
		strings.TrimRight(sb.String(), "\n") + "\n"
}

// newPageFromUpdate stamps a fresh Page with the meta carried by a wikiUpdate:
// the create branch and the update-create-on-missing fallback both build an
// identical new page (same fields, same normalization), so this is the single
// source of truth for "what meta a freshly-created page inherits." It does NOT
// touch Body — the caller sets that (template vs raw content).
func newPageFromUpdate(u wikiUpdate, code string) *Page {
	page := NewPage(u.Title, u.Category, u.Tags)
	if code != "" {
		page.Meta.Code = code
	}
	if u.Importance > 0 {
		page.Meta.Importance = u.Importance
	}
	if u.ID != "" {
		page.Meta.ID = u.ID
	}
	if u.Summary != "" {
		page.Meta.Summary = u.Summary
	}
	if len(u.Related) > 0 {
		page.Meta.Related = u.Related
	}
	if u.Type != "" {
		page.Meta.Type = u.Type
	}
	if u.Confidence != "" {
		page.Meta.Confidence = u.Confidence
	}
	if u.Due != "" {
		page.Meta.Due = u.Due
	}
	if u.Resource != "" {
		page.Meta.Resource = u.Resource
	}
	if len(u.Cues) > 0 {
		page.Meta.Cues = u.Cues
	}
	if u.Client != "" {
		page.Meta.Client = u.Client
	}
	if len(u.Sites) > 0 {
		page.Meta.Sites = normalizeSites(u.Sites)
	}
	if len(u.Kinds) > 0 {
		page.Meta.Kinds = normalizeKinds(u.Kinds)
	}
	return page
}

// applyUpdates creates or updates wiki pages based on LLM instructions.
// Returns (created, updated) counts, the 사용자-category subset of those writes
// (userPages — the user model), and paths of oversized pages.
func (wd *WikiDreamer) applyUpdates(_ context.Context, updates []wikiUpdate) (created, updated, userPages int, oversized []string, appliedPaths []string) {
	maxBytes := wd.config.MaxPageBytes
	// Snapshot existing codes once so filings inherit their project's frozen code
	// (and new-project mints stay collision-free across this batch).
	codeIdx := wd.buildCodeIndex()

	for _, u := range updates {
		var ready bool
		u, ready = wd.prepareDreamUpdate(u)
		if !ready {
			continue
		}
		u = wd.retargetDreamUpdate(u)
		u = wd.rerouteDreamProgressLog(u)

		// Stamp the project's frozen code: a child filing inherits the folder's
		// code; a new project mints one from the LLM stem (Go assigns the 순번).
		code := codeIdx.resolveCode(u)

		outcome := wd.persistDreamUpdate(u, code)
		if outcome.failed {
			continue
		}
		created += outcome.created
		updated += outcome.updated
		if outcome.wrote {
			appliedPaths = append(appliedPaths, u.Path)
		}

		// 사용자-category writes are the user model — counted separately so the
		// dream report/notification surfaces how the model of the user itself
		// evolved (the DreamReport counter existed but was never fed).
		if outcome.wrote && strings.HasPrefix(u.Path, userPrefCategory+"/") {
			userPages++
		}

		wd.markDreamSuperseded(u)
		splitCreated, stillOversized := wd.splitOversizedDreamPage(u.Path, maxBytes)
		created += splitCreated
		if stillOversized {
			oversized = append(oversized, u.Path)
		}
	}

	return created, updated, userPages, oversized, appliedPaths
}

// prepareDreamUpdate performs deterministic path/content normalization and the
// early digest guard. Returning false means the proposal has no write-side
// effects; duplicate lookup and progress-log rerouting intentionally happen in
// later stages.
func (wd *WikiDreamer) prepareDreamUpdate(u wikiUpdate) (wikiUpdate, bool) {
	if u.Path == "" || u.Title == "" {
		return u, false
	}
	// Store.WritePage also strips frontmatter on create, but update content is
	// merged before that boundary and must be cleaned here.
	u.Content = stripLeadingFrontmatter(u.Content)
	if !strings.HasSuffix(u.Path, ".md") {
		u.Path += ".md"
	}
	u.Path = normalizeWikiPath(u.Path)

	if newPath, newCategory := normalizeCategoryPath(u.Path, u.Category); newPath != u.Path || newCategory != u.Category {
		wd.logger.Info("wiki-dream: normalized category path",
			"from", u.Path, "fromCat", u.Category, "to", newPath, "toCat", newCategory)
		u.Path, u.Category = newPath, newCategory
	}
	if newPath := NormalizeProjectPagePath(u.Path); newPath != u.Path {
		wd.logger.Info("wiki-dream: normalized project layout path",
			"from", u.Path, "to", newPath)
		u.Path = newPath
	}
	if newPath := wd.store.CleanNewProjectRepPath(u.Path); newPath != u.Path {
		wd.logger.Info("wiki-dream: cleaned mail-subject project name",
			"from", u.Path, "to", newPath)
		u.Path = newPath
	}
	if isDailyMailDigestPage(u.Title, u.Path) {
		wd.logger.Warn("wiki-dream: skipped daily mail digest page",
			"path", u.Path, "title", u.Title)
		return u, false
	}
	return u, true
}

// retargetDreamUpdate prevents both explicit creates and update-on-missing
// fallbacks from creating a slug/ID/code variant of an existing page.
func (wd *WikiDreamer) retargetDreamUpdate(u wikiUpdate) wikiUpdate {
	if u.Action == "create" {
		// A create whose (already-normalized) path is itself an existing page must
		// become an update: findExistingPage/FindSimilarPages seed seen{self:true},
		// so the exact target is never returned as a "similar" match, and an
		// unconverted create would fall through to createDreamPage → WritePage and
		// overwrite the existing page's body wholesale with just u.Content.
		if page, _ := wd.store.ReadPage(u.Path); page != nil {
			wd.logger.Info("wiki-dream: create target already exists, converting to update",
				"path", u.Path)
			u.Action = "update"
		} else if existing := wd.findExistingPage(u); existing != "" {
			wd.logger.Info("wiki-dream: duplicate detected, converting to update",
				"proposed", u.Path, "existing", existing)
			u.Action = "update"
			u.Path = existing
		}
	}
	if u.Action != "update" {
		return u
	}
	if page, _ := wd.store.ReadPage(u.Path); page != nil {
		return u
	}
	if existing := wd.findExistingPage(u); existing != "" {
		wd.logger.Info("wiki-dream: missing update target matched existing page",
			"proposed", u.Path, "existing", existing)
		u.Path = existing
	}
	return u
}

// rerouteDreamProgressLog runs after dedup retargeting so the event lands in
// the actual project's 로그.md. On append failure the section stays in Content,
// preserving the former imperfect-placement-over-data-loss behavior.
func (wd *WikiDreamer) rerouteDreamProgressLog(u wikiUpdate) wikiUpdate {
	project, isProject := ProjectNameOf(u.Path)
	if !isProject || !IsProjectRepPage(u.Path) || u.Content == "" {
		return u
	}
	body, logLines := splitProgressLogSection(u.Content)
	if logLines == "" || !wd.appendProjectLog(project, logLines) {
		return u
	}
	u.Content = body
	wd.logger.Info("wiki-dream: rerouted 진행 로그 section to project log",
		"project", project)
	return u
}

type dreamWriteOutcome struct {
	created int
	updated int
	wrote   bool
	failed  bool
}

// persistDreamUpdate owns the create/update dispatch. Unknown actions retain
// the historical no-write/non-error outcome so post-write maintenance still
// observes the target path; actual storage errors stop later side effects for
// this proposal while the outer batch continues.
func (wd *WikiDreamer) persistDreamUpdate(u wikiUpdate, code string) dreamWriteOutcome {
	switch u.Action {
	case "create":
		if err := wd.createDreamPage(u, code); err != nil {
			wd.logger.Warn("wiki-dream: create page failed", "path", u.Path, "error", err)
			return dreamWriteOutcome{failed: true}
		}
		return dreamWriteOutcome{created: 1, wrote: true}
	case "update":
		created, err := wd.updateDreamPage(u, code)
		if err != nil {
			wd.logger.Warn("wiki-dream: update page failed", "path", u.Path, "error", err)
			return dreamWriteOutcome{failed: true}
		}
		if created {
			return dreamWriteOutcome{created: 1, wrote: true}
		}
		return dreamWriteOutcome{updated: 1, wrote: true}
	default:
		// An action outside create/update is an LLM contract violation — dropped,
		// but never silently: an unlogged drop cost a day of precision=0
		// misdiagnosis (2026-07-19).
		wd.logger.Warn("wiki-dream: unknown update action dropped", "action", u.Action, "path", u.Path)
		return dreamWriteOutcome{}
	}
}

func (wd *WikiDreamer) createDreamPage(u wikiUpdate, code string) error {
	page := newPageFromUpdate(u, code)
	if u.Content != "" {
		page.Body = u.Content
	} else {
		page.Body = fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성 (dreaming)\n",
			u.Title, time.Now().Format("2006-01-02"))
	}
	if len(u.Related) > 0 {
		page.Body += "\n\n## 관련 문서\n"
		for _, related := range u.Related {
			page.Body += fmt.Sprintf("- [[%s]]\n", related)
		}
	}
	return wd.store.WritePage(u.Path, page)
}

// updateDreamPage keeps the read-modify-write under Store.UpdatePage. The
// boolean distinguishes update-on-missing creation from a true append so the
// original counters remain exact.
func (wd *WikiDreamer) updateDreamPage(u wikiUpdate, code string) (bool, error) {
	created := false
	err := wd.store.UpdatePage(u.Path, func(existing *Page) (*Page, error) {
		if existing == nil {
			page := newPageFromUpdate(u, code)
			page.Body = u.Content
			created = true
			return page, nil
		}
		wd.mergeDreamUpdate(existing, u, code)
		return existing, nil
	})
	return created, err
}

func (wd *WikiDreamer) mergeDreamUpdate(existing *Page, u wikiUpdate, code string) {
	if code != "" && existing.Meta.Code == "" {
		existing.Meta.Code = code
	}
	if u.Content != "" {
		merged := mergeUpdateContent(existing.Body, u.Content)
		if merged == existing.Body {
			wd.logger.Info("wiki-dream: update content already on page; append skipped", "path", u.Path)
		}
		existing.Body = merged
	}
	if len(u.Tags) > 0 {
		existing.Meta.Tags = mergeTags(existing.Meta.Tags, u.Tags)
	}
	if u.Importance > existing.Meta.Importance {
		existing.Meta.Importance = u.Importance
	}
	if u.ID != "" {
		existing.Meta.ID = u.ID
	}
	if u.Summary != "" {
		existing.Meta.Summary = u.Summary
	}
	if len(u.Related) > 0 {
		existing.Meta.Related = mergeRelated(existing.Meta.Related, u.Related)
	}
	if u.Type != "" {
		existing.Meta.Type = u.Type
	}
	if u.Confidence != "" {
		existing.Meta.Confidence = u.Confidence
	}
	if u.Due != "" {
		existing.Meta.Due = u.Due
	}
	if u.Resource != "" {
		existing.Meta.Resource = u.Resource
	}
	if len(u.Cues) > 0 {
		existing.Meta.Cues = mergeCues(existing.Meta.Cues, u.Cues)
	}
	// Fill-only: an operator-set client remains authoritative.
	if u.Client != "" && existing.Meta.Client == "" {
		existing.Meta.Client = u.Client
	}
	// Sites/kinds are usually confirmed AFTER the rep page already exists (the
	// common case) — newPageFromUpdate sets them on create but the update path
	// dropped them, so a later "현장이 확인되면 기입/갱신" never persisted. Union
	// with existing (normalize dedups; normalizeKinds refines parent→child) so a
	// confirmation adds without clobbering prior values.
	if len(u.Sites) > 0 {
		existing.Meta.Sites = normalizeSites(append(append([]string{}, existing.Meta.Sites...), u.Sites...))
	}
	if len(u.Kinds) > 0 {
		existing.Meta.Kinds = normalizeKinds(append(append([]string{}, existing.Meta.Kinds...), u.Kinds...))
	}
	existing.Meta.Updated = time.Now().Format("2006-01-02")
}

// markDreamSuperseded is deliberately after a non-failed persist. A failed
// target write must not demote the pages it claimed to replace.
func (wd *WikiDreamer) markDreamSuperseded(u wikiUpdate) {
	for _, old := range u.Supersedes {
		if old == "" {
			continue
		}
		if err := wd.store.MarkSuperseded(old, u.Path); err != nil {
			wd.logger.Warn("wiki-dream: supersede mark failed",
				"old", old, "new", u.Path, "error", err)
			continue
		}
		wd.logger.Info("wiki-dream: page superseded", "old", old, "new", u.Path)
	}
}

// splitOversizedDreamPage returns newly-created split page count and whether
// the original remains oversized. Stat failures and non-oversized pages retain
// the former silent no-op behavior.
func (wd *WikiDreamer) splitOversizedDreamPage(path string, maxBytes int) (int, bool) {
	if maxBytes <= 0 {
		return 0, false
	}
	info, err := os.Stat(filepath.Join(wd.store.Dir(), path))
	if err != nil || info.Size() <= int64(maxBytes) {
		return 0, false
	}
	subPaths, splitErr := wd.store.splitPage(path, maxBytes)
	if splitErr != nil {
		wd.logger.Warn("wiki-dream: split failed",
			"path", path, "error", splitErr)
		return 0, true
	}
	if len(subPaths) == 0 {
		wd.logger.Warn("wiki-dream: page oversized but cannot split",
			"path", path, "size", info.Size())
		return 0, true
	}
	wd.logger.Info("wiki-dream: page split",
		"path", path, "subPages", len(subPaths))
	return len(subPaths), false
}

// rebuildIndex scans all wiki pages and rebuilds the master index. It delegates
// to Store.rebuildIndex, which holds writeMu so the disk scan + index swap is a
// consistent snapshot — a page write completing concurrently (wiki-research
// turn, mail analysis) can't have its index entry dropped by the wholesale swap.
func (wd *WikiDreamer) rebuildIndex() error {
	return wd.store.rebuildIndex()
}

// findExistingPage checks if a similar page already exists by ID match,
// slug match, or FTS title search (the shared FindSimilarPages primitive).
// Returns the existing path or "".
func (wd *WikiDreamer) findExistingPage(u wikiUpdate) string {
	q := SimilarQuery{
		Path:     u.Path,
		ID:       u.ID,
		Title:    u.Title,
		Category: u.Category,
	}
	// The frozen-code signal identifies the PROJECT (FindSimilarPages resolves
	// it to rep pages), so it applies only when the proposal itself IS a rep
	// page. A CHILD page (기자재/메일분석/상세) legitimately carries its project's
	// code — matching on it returned the 대표페이지 and converted the child
	// create into a rep update, absorbing child content into the rep.
	if IsProjectRepPage(u.Path) {
		q.Code = u.Code
	}
	hits := wd.store.FindSimilarPages(context.Background(), q, 1)
	if len(hits) == 0 {
		return ""
	}
	return hits[0].Path
}

// normalizeSlug reduces a wiki path to a comparable slug form.
// "사람/에코프로-담당자---석문호,-표과장.md" -> "사람/에코프로담당자석문호표과장"
func normalizeSlug(path string) string {
	path = strings.TrimSuffix(path, ".md")
	path = strings.ToLower(path)
	var sb strings.Builder
	for _, r := range path {
		if r == '/' {
			sb.WriteRune(r)
		} else if r == '-' || r == '_' || r == ',' || r == ' ' || r == '(' || r == ')' {
			continue
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// normalizeWikiPath strips a leading wikilink namespace ("w:") from a proposed
// page path. The dreamer model occasionally prefixes the path's category
// directory with the knowledge-router's "w:" ref form ("w:프로젝트/…"); since the
// category is the page's directory, that files the page under a phantom
// "w:프로젝트" bucket that duplicates "프로젝트". A plain path is unchanged.
func normalizeWikiPath(p string) string {
	return strings.TrimPrefix(strings.TrimSpace(p), "w:")
}

// defaultCategory is the catch-all bucket for pages whose directory maps to no
// taxonomy category — keeps nothing loose at the wiki root.
const defaultCategory = "기타"

// remapLegacyCategory folds a legacy or alias category/directory name onto the
// current 5-category taxonomy, returning ("", false) when there's no known
// mapping. Used during the transition so the dreamer emitting an old name (거래,
// 기술, 운영시스템, …) still files correctly instead of resurrecting a retired
// bucket.
func remapLegacyCategory(name string) (string, bool) {
	switch strings.TrimSpace(name) {
	case "거래", "결정", "메일분석", "mail-analyses", "mail-analysis":
		return "프로젝트", true
	case "사람", "연락처", "관계":
		return "인물", true
	case "기술", "지식", "산업", "세상":
		return "업무", true
	case "선호", "취향":
		return "사용자", true
	case "운영시스템", "운영", "시스템설정":
		return "시스템", true
	}
	return "", false
}

// resolveCategory maps a category field value onto a valid taxonomy category,
// falling back to the 기타 catch-all.
func resolveCategory(category string) string {
	if ValidateCategory(category) {
		return category
	}
	if cat, ok := remapLegacyCategory(category); ok {
		return cat
	}
	return defaultCategory
}

// normalizeCategoryPath canonicalizes a page path onto the 5-category taxonomy by
// its leading directory and returns the corrected path plus the category that now
// matches that directory. Resolution order: (1) a path already under a valid
// category (including a valid-category sub-folder like "프로젝트/거래/…") is kept;
// (2) a legacy/alias directory is remapped; (3) otherwise the category field is
// consulted (valid, then remappable); (4) failing all that, the 기타 catch-all. A
// path with no directory ("foo.md") derives its directory from the same cascade
// so nothing lands at the wiki root.
func normalizeCategoryPath(path, category string) (string, string) {
	if parts := strings.SplitN(path, "/", 2); len(parts) == 2 {
		dir, rest := parts[0], parts[1]
		if ValidateCategory(dir) {
			return path, dir
		}
		if cat, ok := remapLegacyCategory(dir); ok {
			return cat + "/" + rest, cat
		}
		cat := resolveCategory(category)
		return cat + "/" + rest, cat
	}
	cat := resolveCategory(category)
	return cat + "/" + path, cat
}

func (wd *WikiDreamer) resetCounters() {
	wd.cmu.Lock()
	wd.turnCount = 0
	wd.prefSignals = 0 // the cycle consumed (or backed off on) the pending 선호 capsules
	wd.lastDream = time.Now()
	last := wd.lastDream
	wd.cmu.Unlock()
	// Persist lastDream so the time-trigger survives restarts (see NewWikiDreamer).
	if wd.store == nil {
		return
	}
	state := wd.loadDiaryProcessState()
	state.LastDreamMs = last.UnixMilli()
	if err := wd.saveDiaryProcessState(state); err != nil && wd.logger != nil {
		wd.logger.Warn("wiki-dream: persist lastDream failed", "error", err)
	}
}

// mergeCues appends new cue anchors not already present, normalized
// (trim/drop-empty/dedupe) BEFORE the cap so whitespace variants and empties
// can't eat cap slots, then capped so repeated dream cycles can't grow a page
// into a BM25 stopword magnet — a page matching everything is as useless as
// one matching nothing. Existing cues keep priority (stable across cycles);
// overflow from a single update is dropped. normalizeCues owns the cap (10).
func mergeCues(existing, added []string) []string {
	return normalizeCues(mergeTags(existing, added))
}

// mergeTags merges two tag lists, deduplicating.

func mergeTags(existing, added []string) []string {
	seen := map[string]struct{}{}
	for _, t := range existing {
		seen[t] = struct{}{}
	}
	result := append([]string{}, existing...)
	for _, t := range added {
		if _, ok := seen[t]; !ok {
			result = append(result, t)
			seen[t] = struct{}{}
		}
	}
	return result
}

// mergeRelated merges two related-page lists, deduplicating (union).
func mergeRelated(existing, added []string) []string {
	seen := map[string]struct{}{}
	for _, r := range existing {
		seen[r] = struct{}{}
	}
	result := append([]string{}, existing...)
	for _, r := range added {
		if _, ok := seen[r]; !ok {
			result = append(result, r)
			seen[r] = struct{}{}
		}
	}
	return result
}
