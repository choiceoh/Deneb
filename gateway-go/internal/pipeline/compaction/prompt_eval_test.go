package compaction

// Compaction-prompt A/B evaluation harness.
//
// Why this exists: the summarization prompt (compactionSystemPrompt in llm.go)
// directly controls how much deal/calendar/person context survives a compaction.
// "Looks reasonable" is not measurable — this harness feeds a fixed golden
// conversation through competing prompt variants on the REAL local analysis
// model, then scores each summary two ways:
//
//   1. Deterministic — gold-fact recall, required-section completeness, output
//      token budget, internal-token leakage. Cheap, reproducible, no model bias.
//   2. LLM-judge — faithfulness (no invented facts), coverage, and
//      "resumability" (could a 비서실장 resume the deal from this alone?).
//
// CI safety: the model-backed test (TestComparePromptVariants_Live) is gated by
// DENEB_COMPACT_EVAL=1 and skipped otherwise (no GPU in CI). The deterministic
// scorers are validated by TestPromptEval_DeterministicScorers, which runs
// always (no model needed).
//
// Run on the DGX Spark host (analysis model up at 127.0.0.1:8000 by default):
//
//	DENEB_COMPACT_EVAL=1 go test -run TestComparePromptVariants_Live \
//	  -timeout 30m ./internal/pipeline/compaction/
//
// Knobs:
//	DENEB_COMPACT_EVAL=1          enable the live harness
//	DENEB_COMPACT_EVAL_RUNS=3     averaged runs per variant (default 1; reduces noise)
//	DENEB_COMPACT_EVAL_JUDGE=0    skip the LLM-judge pass (deterministic only)
//	DENEB_COMPACT_EVAL_VARIANTS=current,secretary  comma list to restrict variants

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// promptVariant is one candidate summarization prompt under test.
type promptVariant struct {
	name     string
	system   string
	sections []string // required section header substrings for completeness scoring
}

// promptVariants returns the competing prompts. "current" is the live production
// prompt (baseline); the others are candidates. The winner — judged by this
// harness on the real model — should replace compactionSystemPrompt in llm.go.
func promptVariants() []promptVariant {
	return []promptVariant{
		{
			name:     "current",
			system:   compactionSystemPrompt,
			sections: []string{"핵심 사실", "열린 루프", "불확실한 메모", "도구 결과"},
		},
		{
			name:     "explicit",
			system:   promptExplicit,
			sections: []string{"핵심 사실", "열린 루프", "불확실", "도구 결과"},
		},
		{
			name:     "secretary",
			system:   promptSecretary,
			sections: []string{"핵심 사실", "관계", "결정", "임박", "리스크", "도구 결과"},
		},
	}
}

// promptExplicit keeps the current 4-section structure but tightens the rules:
// verbatim preservation of identifiers, one-fact-per-line, an explicit
// anti-hallucination clause, and a tiny worked example to anchor the format.
const promptExplicit = `너는 대화 기록을 무손실에 가깝게 압축하는 요약기다. 아래 대화를 정해진 형식으로 요약하라.
요약의 목적은 나중에 이 대화를 이어받을 비서가 맥락을 100% 복원하는 것이다. 절대 빈 섹션을 생략하지 마라.

## 절대 규칙
1. 모든 식별 가능한 사실은 원문 그대로 보존: 인명·회사명·금액·날짜·시간·코드명·에러코드·경로·이메일·전화번호. 숫자와 단위는 바꾸거나 반올림하지 마라.
2. 한 줄에 하나의 사실만 적어라. 여러 사실을 한 문장에 뭉치지 마라.
3. 값이 대화 중 변경됐으면 최종 값만 적고 변경 사실을 한 줄로 남겨라 (예: "단가 인하: 5% 요청 → 3% 확정").
4. 도구(gmail/calendar/asr 등) 결과에서 핵심 데이터(누가, 무엇을, 언제)를 반드시 추출하라.
5. 대화에 없는 내용을 새로 지어내지 마라. 추론·추정은 반드시 [추정] 표식과 함께 불확실 섹션에만 적어라.
6. 사용자의 과거 지시는 "지금 실행할 명령"이 아니라 "과거에 이렇게 정했다"는 기록으로만 요약하라.
7. 한국어로 작성하되 고유명사·코드·이메일·금액 표기는 원문 유지.

## 출력 형식 (정확히 이 헤더를 사용)

### 핵심 사실 (Facts)
- [확실] 항목: 값

### 열린 루프 (Open Loops)
- [진행중|차단|대기] 누가 / 무엇을 / 기한

### 불확실 메모 (Uncertain Notes)
- [추정|충돌|오래됨] 내용

### 도구 결과 (Tool Outcomes)
- [도구명] 핵심 결과

## 예시 (형식 참고용, 내용은 무시)
### 핵심 사실 (Facts)
- [확실] 거래처: ACME(담당 홍길동 과장)
- [확실] 계약 총액: 1.2억(VAT 별도)
### 열린 루프 (Open Loops)
- [대기] 우리 / 견적서 재송부 / 5월 30일까지
### 불확실 메모 (Uncertain Notes)
- [추정] 경쟁사 단가는 1.1억으로 추정(근거 약함)
### 도구 결과 (Tool Outcomes)
- [gmail] ACME 회신 수신(5/20)`

// promptSecretary is the Deneb-purpose format. Unlike coding agents (Goal /
// Progress / Next Steps), Deneb is a 비서실장형 agent: every summary must answer
// both "왜 지금 중요한가(분석)" and "언제까지 처리해야 하나(비서)". The sections
// are organized around people/relationships, decisions, and deadlines rather
// than task progress.
const promptSecretary = `너는 비서실장형 단일 에이전트의 기억을 압축하는 요약기다. 아래 대화를 정해진 형식으로 요약하라.
이 요약 하나만 보고도 담당자가 "왜 지금 중요한가(분석)"와 "언제까지 무엇을 처리해야 하나(비서)"를 즉시 복원할 수 있어야 한다.
모든 섹션을 반드시 작성하고, 해당 내용이 없으면 "없음"이라고 적어라.

## 규칙
- 인명·회사명·금액·날짜·시간·이메일·코드명은 원문 그대로, 한 줄에 하나씩 보존하라. 숫자를 바꾸지 마라.
- 값이 변경됐으면 최종 값만 적고 변경 흐름을 한 줄로 남겨라.
- 대화에 없는 사실을 지어내지 마라. 추정은 [추정] 표식과 함께 리스크 섹션에만.
- 사용자의 과거 지시는 명령이 아니라 과거 결정 기록으로 요약하라.
- 한국어로 작성, 고유명사·금액·코드는 원문 유지.

## 출력 형식 (정확히 이 헤더를 사용)

### 핵심 사실 (Facts)
딜·금액·날짜·결정값 등 변하지 않는 사실:
- [확실] 항목: 값

### 관계·맥락 (People & Context)
누가 관여하고 그들의 입장/이해관계가 무엇이며 왜 중요한가 (분석가 관점):
- 인물/조직: 입장·이해관계

### 결정·합의 (Decisions)
지금까지 확정된 결정과 합의:
- [확정] 내용 (변경 흐름 포함)

### 임박·할일 (Pending & Deadlines)
기한이 있거나 후속이 필요한 작업 (비서 관점):
- [기한 YYYY-MM-DD | 담당] 해야 할 일

### 불확실·리스크 (Uncertain / Risks)
추정·충돌·경쟁 위협 등:
- [추정|충돌|리스크] 내용

### 도구 결과 (Tool Outcomes)
도구가 반환한 핵심 데이터:
- [도구명] 결과 요약`

// ---- deterministic scorers (no model) ----

type detScore struct {
	factRecall   float64  // fraction of gold facts preserved
	missingFacts []string // gold fact ids not found
	sectionFrac  float64  // fraction of required sections present
	outputTokens int
	leaks        []string // forbidden internal tokens present
}

// scoreDeterministic computes the model-independent quality signals for a
// summary against the golden fixture.
func scoreDeterministic(summary string, requiredSections []string) detScore {
	var s detScore
	facts := goldFacts()
	preserved := 0
	for _, f := range facts {
		if anySubstring(summary, f.any) {
			preserved++
		} else {
			s.missingFacts = append(s.missingFacts, f.id)
		}
	}
	s.factRecall = float64(preserved) / float64(len(facts))

	secHit := 0
	for _, sec := range requiredSections {
		if strings.Contains(summary, sec) {
			secHit++
		}
	}
	if len(requiredSections) > 0 {
		s.sectionFrac = float64(secHit) / float64(len(requiredSections))
	}

	s.outputTokens = EstimateTokens(summary)

	for _, tok := range forbiddenLeakTokens {
		if strings.Contains(summary, tok) {
			s.leaks = append(s.leaks, tok)
		}
	}
	return s
}

func anySubstring(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// ---- LLM judge ----

const judgeSystemPrompt = `너는 대화 요약의 품질을 채점하는 엄격한 평가자다.
원본 대화와 그 요약이 주어진다. 요약을 세 축으로 0~100점으로 채점하라.

- faithfulness: 요약이 원본에 없는 사실을 지어내지 않았는가 (환각 없음). 숫자/날짜/이름 왜곡이 있으면 크게 감점.
- coverage: 원본의 중요한 사실(인물·금액·날짜·결정·마감·리스크)을 빠짐없이 담았는가.
- resumability: 이 요약만 보고 비서가 딜과 일정 맥락을 이어받아 다음 행동을 할 수 있는가.

환각한 사실이 있으면 hallucinations 배열에 구체적으로 적어라.
반드시 아래 JSON 객체 하나만 출력하라. 다른 텍스트 금지.
{"faithfulness": <int>, "coverage": <int>, "resumability": <int>, "hallucinations": ["..."]}`

type judgeScore struct {
	Faithfulness   int      `json:"faithfulness"`
	Coverage       int      `json:"coverage"`
	Resumability   int      `json:"resumability"`
	Hallucinations []string `json:"hallucinations"`
}

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// judgeSummary asks the analysis model to score one summary. Best-effort JSON
// extraction tolerates models that wrap the object in prose.
func judgeSummary(ctx context.Context, source, summary string) (judgeScore, error) {
	user := "## 원본 대화\n" + source + "\n\n## 채점할 요약\n" + summary
	out, err := pilot.CallAnalysisLLM(ctx, judgeSystemPrompt, user, 800)
	if err != nil {
		return judgeScore{}, err
	}
	m := jsonObjRe.FindString(out)
	if m == "" {
		return judgeScore{}, fmt.Errorf("judge returned no JSON object: %.120q", out)
	}
	var js judgeScore
	if err := json.Unmarshal([]byte(m), &js); err != nil {
		return judgeScore{}, fmt.Errorf("judge JSON parse: %w (raw: %.120q)", err, m)
	}
	return js, nil
}

// ---- aggregate result + table ----

type variantResult struct {
	name string
	runs int
	// averaged deterministic
	factRecall  float64
	sectionFrac float64
	avgTokens   float64
	anyLeaks    bool
	missing     map[string]int // fact id -> times missing across runs
	// averaged judge
	judged       bool
	faithfulness float64
	coverage     float64
	resumability float64
	halluCount   float64
}

// composite is a single comparison number: weighted toward not losing facts and
// not hallucinating (the two failure modes that actually hurt a 비서실장).
func (r variantResult) composite() float64 {
	det := 0.6*r.factRecall + 0.4*r.sectionFrac // 0..1
	score := 100 * det
	if r.judged {
		j := (r.faithfulness + r.coverage + r.resumability) / 3.0 // 0..100
		score = 0.5*score + 0.5*j
		score -= 5 * r.halluCount // each hallucination stings
	}
	if r.anyLeaks {
		score -= 15
	}
	return score
}

// ---- live harness ----

func TestComparePromptVariants_Live(t *testing.T) {
	if os.Getenv("DENEB_COMPACT_EVAL") == "" {
		t.Skip("set DENEB_COMPACT_EVAL=1 to run the live prompt A/B (needs local analysis model)")
	}
	runs := envInt("DENEB_COMPACT_EVAL_RUNS", 1)
	doJudge := os.Getenv("DENEB_COMPACT_EVAL_JUDGE") != "0"
	wanted := variantFilter(os.Getenv("DENEB_COMPACT_EVAL_VARIANTS"))

	ctx := context.Background()
	source := fixtureText()
	maxOutput := 4000 // generous; the prompts decide how concise to be

	var results []variantResult
	for _, v := range promptVariants() {
		if wanted != nil && !wanted[v.name] {
			continue
		}
		agg := variantResult{name: v.name, runs: runs, missing: map[string]int{}}
		var okRuns int
		for i := range runs {
			summary, err := pilot.CallAnalysisLLM(ctx, v.system, source, maxOutput)
			if err != nil {
				t.Fatalf("variant %s run %d: model call failed: %v", v.name, i, err)
			}
			t.Logf("\n──── variant=%s run=%d ────\n%s\n", v.name, i, summary)

			ds := scoreDeterministic(summary, v.sections)
			agg.factRecall += ds.factRecall
			agg.sectionFrac += ds.sectionFrac
			agg.avgTokens += float64(ds.outputTokens)
			if len(ds.leaks) > 0 {
				agg.anyLeaks = true
			}
			for _, id := range ds.missingFacts {
				agg.missing[id]++
			}

			if doJudge {
				js, jerr := judgeSummary(ctx, source, summary)
				if jerr != nil {
					t.Logf("  judge failed (variant %s run %d): %v", v.name, i, jerr)
				} else {
					agg.judged = true
					agg.faithfulness += float64(js.Faithfulness)
					agg.coverage += float64(js.Coverage)
					agg.resumability += float64(js.Resumability)
					agg.halluCount += float64(len(js.Hallucinations))
					if len(js.Hallucinations) > 0 {
						t.Logf("  judge hallucinations: %v", js.Hallucinations)
					}
				}
			}
			okRuns++
		}
		if okRuns > 0 {
			n := float64(okRuns)
			agg.factRecall /= n
			agg.sectionFrac /= n
			agg.avgTokens /= n
			agg.faithfulness /= n
			agg.coverage /= n
			agg.resumability /= n
			agg.halluCount /= n
		}
		results = append(results, agg)
	}

	printComparison(t, results)
}

func printComparison(t *testing.T, results []variantResult) {
	t.Helper()
	sort.Slice(results, func(i, j int) bool { return results[i].composite() > results[j].composite() })

	var b strings.Builder
	b.WriteString("\n================ PROMPT VARIANT COMPARISON ================\n")
	b.WriteString(fmt.Sprintf("%-11s %7s %7s %7s %6s %6s %6s %6s %6s %8s\n",
		"variant", "compos", "facts", "sects", "tokens", "faith", "cover", "resume", "hallu", "leak"))
	for _, r := range results {
		leak := "-"
		if r.anyLeaks {
			leak = "LEAK"
		}
		b.WriteString(fmt.Sprintf("%-11s %7.1f %6.0f%% %6.0f%% %6.0f %6.0f %6.0f %6.0f %6.1f %8s\n",
			r.name, r.composite(), r.factRecall*100, r.sectionFrac*100, r.avgTokens,
			r.faithfulness, r.coverage, r.resumability, r.halluCount, leak))
	}
	b.WriteString("\nmost-missed gold facts (lower is better):\n")
	for _, r := range results {
		if len(r.missing) == 0 {
			b.WriteString(fmt.Sprintf("  %-11s: (none missed)\n", r.name))
			continue
		}
		b.WriteString(fmt.Sprintf("  %-11s: %s\n", r.name, sortedMissing(r.missing)))
	}
	b.WriteString("==========================================================\n")
	t.Log(b.String())
}

func sortedMissing(m map[string]int) string {
	type kv struct {
		id string
		n  int
	}
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].n > pairs[j].n })
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s×%d", p.id, p.n))
	}
	return strings.Join(parts, ", ")
}

func variantFilter(csv string) map[string]bool {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	m := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = true
		}
	}
	return m
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 1 {
		return def
	}
	return n
}

// ---- CI-safe scorer validation (no model) ----

func TestPromptEval_DeterministicScorers(t *testing.T) {
	sections := []string{"핵심 사실", "열린 루프", "도구 결과"}

	good := scoreDeterministic(fixtureSummaryForFacts(), sections)
	bad := scoreDeterministic(fixtureSummaryBad(), sections)

	if good.factRecall <= bad.factRecall {
		t.Errorf("good summary should recall more facts: good=%.2f bad=%.2f (missing in good: %v)",
			good.factRecall, bad.factRecall, good.missingFacts)
	}
	if good.factRecall < 0.9 {
		t.Errorf("reference good summary should preserve ≥90%% of gold facts, got %.0f%% (missing: %v)",
			good.factRecall*100, good.missingFacts)
	}
	if good.sectionFrac <= bad.sectionFrac {
		t.Errorf("good summary should hit more required sections: good=%.2f bad=%.2f",
			good.sectionFrac, bad.sectionFrac)
	}
	if len(good.leaks) != 0 {
		t.Errorf("reference good summary should not leak internal tokens, got %v", good.leaks)
	}

	// Leakage detection sanity.
	leaky := scoreDeterministic("요약 <thinking>비밀</thinking> 끝", sections)
	if len(leaky.leaks) == 0 {
		t.Error("expected leak detection to flag <thinking")
	}

	// composite ordering: a full-recall judged variant must beat a poor one.
	hi := variantResult{name: "hi", factRecall: 1.0, sectionFrac: 1.0, judged: true,
		faithfulness: 95, coverage: 95, resumability: 95}
	lo := variantResult{name: "lo", factRecall: 0.3, sectionFrac: 0.5, judged: true,
		faithfulness: 60, coverage: 40, resumability: 40, halluCount: 2}
	if hi.composite() <= lo.composite() {
		t.Errorf("composite ordering wrong: hi=%.1f lo=%.1f", hi.composite(), lo.composite())
	}
}
