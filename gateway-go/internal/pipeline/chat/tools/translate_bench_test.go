package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Translation quality + speed benchmark for the in-app browser translator
// (en/ru → ko). Reuses the production prompt/parse (buildTranslatePrompt /
// parseTranslations) so it measures the exact translate logic — against a
// caller-chosen endpoint/model, to compare candidates while tuning the
// `translation` model role.
//
// QUALITY — primary: an LLM judge (set DENEB_TRANSLATE_JUDGE_MODEL).
//   - Two models (set DENEB_TRANSLATE_MODEL_B): PAIRWISE "which is better" — the
//     most reliable signal for ranking models. Each segment is judged in BOTH
//     orders to cancel position bias; a model wins only when both orders agree.
//   - One model: ABSOLUTE adequacy + fluency (1–5), reference-free.
//   The judge MUST be independent of the candidates (a candidate judging itself
//   self-prefers); use a strong third model.
// QUALITY — secondary: chrF vs the references below. Cheap + deterministic, so
//   it's a regression/sanity signal even with no judge (surface overlap only,
//   not meaning/fluency — that's why the LLM judge is primary).
// SPEED: batch throughput (the in-place path ships batches) + per-segment p50/p90.
//
// CI runs only the metric/parse unit tests. The live benchmark is gated:
//
//	DENEB_TRANSLATE_LIVE=1 DENEB_TRANSLATE_MODEL=<served> \
//	  [DENEB_TRANSLATE_MODEL_B=<other> for pairwise] \
//	  [DENEB_TRANSLATE_JUDGE_MODEL=<strong, independent>] \
//	  [DENEB_TRANSLATE_URL=… URL_B=… JUDGE_URL=… KEY=… JUDGE_KEY=…] \
//	  go test -run TestTranslateBench_Live -v ./internal/pipeline/chat/tools/

type benchCase struct {
	lang string
	src  string
	ref  string // a Korean reference (approximate; used only by the secondary chrF)
}

var translateBenchCorpus = []benchCase{
	{"en", "Sign in", "로그인"},
	{"en", "Read more", "더 보기"},
	{"en", "Privacy Policy", "개인정보 처리방침"},
	{"en", "The deadline for the proposal is next Friday.", "제안서 마감은 다음 주 금요일입니다."},
	{"en", "Please confirm your email address to continue.", "계속하려면 이메일 주소를 확인해 주세요."},
	{"en", "Our new model achieves state-of-the-art accuracy on the benchmark.", "우리의 새 모델은 벤치마크에서 최첨단 정확도를 달성합니다."},
	{"ru", "Войти", "로그인"},
	{"ru", "Читать далее", "더 읽기"},
	{"ru", "Настройки конфиденциальности", "개인정보 설정"},
	{"ru", "Срок подачи заявки истекает в пятницу.", "신청 마감은 금요일입니다."},
	{"ru", "Цены указаны без учёта налога.", "가격은 세금 미포함입니다."},
	{"ru", "Пожалуйста, подтвердите свой адрес электронной почты.", "이메일 주소를 확인해 주세요."},
}

func TestTranslateBench_Live(t *testing.T) {
	if os.Getenv("DENEB_TRANSLATE_LIVE") == "" {
		t.Skip("set DENEB_TRANSLATE_LIVE=1 (needs a local LLM endpoint) to run the translate benchmark")
	}
	modelA := strings.TrimSpace(os.Getenv("DENEB_TRANSLATE_MODEL"))
	if modelA == "" {
		t.Skip("set DENEB_TRANSLATE_MODEL=<served model name> to benchmark")
	}
	urlA := envOrDefault("DENEB_TRANSLATE_URL", "http://127.0.0.1:8000/v1")
	clientA := llm.NewClient(urlA, os.Getenv("DENEB_TRANSLATE_KEY"))

	modelB := strings.TrimSpace(os.Getenv("DENEB_TRANSLATE_MODEL_B"))
	var clientB *llm.Client
	if modelB != "" {
		clientB = llm.NewClient(envOrDefault("DENEB_TRANSLATE_URL_B", urlA), os.Getenv("DENEB_TRANSLATE_KEY"))
	}
	judgeModel := strings.TrimSpace(os.Getenv("DENEB_TRANSLATE_JUDGE_MODEL"))
	var judge *llm.Client
	if judgeModel != "" {
		judge = llm.NewClient(envOrDefault("DENEB_TRANSLATE_JUDGE_URL", urlA), os.Getenv("DENEB_TRANSLATE_JUDGE_KEY"))
		if judgeModel == modelA || judgeModel == modelB {
			t.Logf("⚠ judge model %q is also a candidate — self-preference bias; prefer an independent judge", judgeModel)
		}
	}

	srcs := make([]string, len(translateBenchCorpus))
	for i, c := range translateBenchCorpus {
		srcs[i] = c.src
	}

	outA, batchA := timedTranslate(t, clientA, modelA, srcs)
	var outB []string
	var batchB time.Duration
	if clientB != nil {
		outB, batchB = timedTranslate(t, clientB, modelB, srcs)
	}

	// ── Primary quality: LLM judge ──
	if judge != nil {
		if clientB != nil {
			aWins, bWins, ties := 0, 0, 0
			t.Logf("── pairwise judgment (judge=%s): A=%s vs B=%s ──", judgeModel, modelA, modelB)
			for i, c := range translateBenchCorpus {
				switch judgePairwise(t, judge, judgeModel, c.src, outA[i], outB[i]) {
				case 1:
					aWins++
				case -1:
					bWins++
				default:
					ties++
				}
				t.Logf("[%s] A=%q  B=%q", c.lang, outA[i], outB[i])
			}
			n := float64(len(translateBenchCorpus))
			t.Logf("QUALITY (pairwise, order-debiased): A=%s wins %d · B=%s wins %d · tie %d  → A win-rate %.0f%%",
				modelA, aWins, modelB, bWins, ties, 100*float64(aWins)/n)
		} else {
			var sa, sf float64
			for _, c := range translateBenchCorpus {
				ad, fl := judgeAbsolute(t, judge, judgeModel, c.src, outA[indexOfSrc(c.src, srcs)])
				sa += float64(ad)
				sf += float64(fl)
			}
			n := float64(len(translateBenchCorpus))
			t.Logf("QUALITY (absolute, judge=%s): adequacy %.2f/5 · fluency %.2f/5", judgeModel, sa/n, sf/n)
		}
	} else {
		t.Logf("QUALITY (LLM judge skipped): set DENEB_TRANSLATE_JUDGE_MODEL=<strong, independent> for meaning/fluency scoring")
	}

	// ── Secondary quality: chrF (deterministic) ──
	t.Logf("QUALITY (chrF, secondary): A=%s mean=%.1f%s", modelA, meanChrF(outA),
		func() string {
			if clientB != nil {
				return fmt.Sprintf("   B=%s mean=%.1f", modelB, meanChrF(outB))
			}
			return ""
		}())

	// ── Speed ──
	reportSpeed(t, "A="+modelA, clientA, modelA, srcs, batchA)
	if clientB != nil {
		reportSpeed(t, "B="+modelB, clientB, modelB, srcs, batchB)
	}
}

func meanChrF(out []string) float64 {
	var sum float64
	for i, c := range translateBenchCorpus {
		sum += chrF(out[i], c.ref)
	}
	return sum / float64(len(translateBenchCorpus))
}

func indexOfSrc(src string, srcs []string) int {
	for i, s := range srcs {
		if s == src {
			return i
		}
	}
	return 0
}

func reportSpeed(t *testing.T, label string, client *llm.Client, model string, srcs []string, batch time.Duration) {
	t.Helper()
	lats := make([]time.Duration, 0, len(srcs))
	for _, s := range srcs {
		st := time.Now()
		timedTranslate(t, client, model, []string{s})
		lats = append(lats, time.Since(st))
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	var sum time.Duration
	for _, d := range lats {
		sum += d
	}
	t.Logf("SPEED %s: batch %v (%.1f seg/s) · per-segment p50=%v p90=%v mean=%v",
		label, batch.Round(time.Millisecond), float64(len(srcs))/batch.Seconds(),
		lats[len(lats)/2].Round(time.Millisecond), lats[(len(lats)*9)/10].Round(time.Millisecond),
		(sum / time.Duration(len(lats))).Round(time.Millisecond))
}

func timedTranslate(t *testing.T, client *llm.Client, model string, segs []string) ([]string, time.Duration) {
	t.Helper()
	start := time.Now()
	out := benchTranslate(t, client, model, segs)
	return out, time.Since(start)
}

// benchTranslate runs the production prompt/parse against the caller's client,
// keeping the same count-preserving contract as TranslateSegments.
func benchTranslate(t *testing.T, client *llm.Client, model string, segs []string) []string {
	t.Helper()
	system, user := buildTranslatePrompt(segs, "Korean")
	raw, err := client.Complete(context.Background(), llm.ChatRequest{
		Model:          model,
		System:         llm.SystemString(system),
		Messages:       []llm.Message{llm.NewTextMessage("user", user)},
		MaxTokens:      translateMaxTokens,
		Thinking:       &llm.ThinkingConfig{Type: "disabled"},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("translate call (model=%s): %v", model, err)
	}
	out, ok := parseTranslations(raw, len(segs))
	if !ok {
		t.Logf("warning: count mismatch/parse fail — keeping originals for this batch")
		return segs
	}
	return out
}

func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ── LLM judge ──

type pairVerdict struct {
	Winner string `json:"winner"`
	Reason string `json:"reason"`
}

type absScore struct {
	Adequacy int `json:"adequacy"`
	Fluency  int `json:"fluency"`
}

// judgePairwise returns +1 (A better), -1 (B better), or 0 (tie/inconsistent),
// judging both orders so a positional preference cancels out (a model wins only
// when it's preferred regardless of slot).
func judgePairwise(t *testing.T, judge *llm.Client, judgeModel, src, aTrans, bTrans string) int {
	fwd := judgeVerdict(t, judge, judgeModel, src, aTrans, bTrans) // slot A = aTrans
	rev := judgeVerdict(t, judge, judgeModel, src, bTrans, aTrans) // slot A = bTrans
	score := 0
	switch fwd {
	case "A":
		score++
	case "B":
		score--
	}
	switch rev {
	case "A":
		score-- // slot A here is bTrans, so an "A" win favors B
	case "B":
		score++
	}
	if score > 0 {
		return 1
	}
	if score < 0 {
		return -1
	}
	return 0
}

func judgeVerdict(t *testing.T, judge *llm.Client, judgeModel, src, a, b string) string {
	t.Helper()
	const system = `당신은 번역 품질 심판입니다. 원문(영어 또는 러시아어)과 두 한국어 번역 A·B가 주어집니다.
의미 정확성과 한국어의 자연스러움을 종합해 더 나은 쪽을 고르세요. 우열을 가리기 어려우면 tie.
설명·markdown 없이 JSON만 출력: {"winner":"A|B|tie","reason":"한 줄 근거"}`
	user := fmt.Sprintf("원문:\n%s\n\n번역 A:\n%s\n\n번역 B:\n%s", src, a, b)
	raw, err := judge.Complete(context.Background(), llm.ChatRequest{
		Model:          judgeModel,
		System:         llm.SystemString(system),
		Messages:       []llm.Message{llm.NewTextMessage("user", user)},
		MaxTokens:      256,
		Thinking:       &llm.ThinkingConfig{Type: "disabled"},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("judge call (model=%s): %v", judgeModel, err)
	}
	return normalizeWinner(parseWinner(raw))
}

func judgeAbsolute(t *testing.T, judge *llm.Client, judgeModel, src, trans string) (int, int) {
	t.Helper()
	const system = `원문(영어 또는 러시아어)과 그 한국어 번역이 주어집니다. 두 항목을 1~5 정수로 채점하세요:
adequacy(원문 의미를 정확히 전달하는가), fluency(자연스럽고 매끄러운 한국어인가).
설명·markdown 없이 JSON만 출력: {"adequacy":N,"fluency":N}`
	user := fmt.Sprintf("원문:\n%s\n\n번역:\n%s", src, trans)
	raw, err := judge.Complete(context.Background(), llm.ChatRequest{
		Model:          judgeModel,
		System:         llm.SystemString(system),
		Messages:       []llm.Message{llm.NewTextMessage("user", user)},
		MaxTokens:      128,
		Thinking:       &llm.ThinkingConfig{Type: "disabled"},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("judge call (model=%s): %v", judgeModel, err)
	}
	s, _ := jsonutil.UnmarshalLLM[absScore](raw)
	return clampScore(s.Adequacy), clampScore(s.Fluency)
}

func parseWinner(raw string) string {
	if v, err := jsonutil.UnmarshalLLM[pairVerdict](raw); err == nil {
		return v.Winner
	}
	return ""
}

// normalizeWinner maps the judge's winner field to "A" / "B" / "tie", defaulting
// to "tie" for anything unrecognized (so noise never fabricates a win).
func normalizeWinner(w string) string {
	switch strings.ToLower(strings.TrimSpace(w)) {
	case "a":
		return "A"
	case "b":
		return "B"
	default:
		return "tie"
	}
}

func clampScore(n int) int {
	if n < 1 {
		return 1
	}
	if n > 5 {
		return 5
	}
	return n
}

// ── chrF (secondary, deterministic) ──

// chrF is the character n-gram F-score (orders 1..6, β=2), a standard MT metric
// well-suited to Korean (character-level). Returns 0..100. Whitespace is stripped
// so spacing variants don't skew the score.
func chrF(candidate, reference string) float64 {
	cand := stripSpace(candidate)
	ref := stripSpace(reference)
	const maxN = 6
	const beta2 = 4.0 // β=2 → β² = 4
	var sumP, sumR float64
	orders := 0
	for n := 1; n <= maxN; n++ {
		cN := charNgrams(cand, n)
		rN := charNgrams(ref, n)
		cTot, rTot := countTotal(cN), countTotal(rN)
		if cTot == 0 && rTot == 0 {
			continue
		}
		matched := 0
		for ng, rc := range rN {
			if cc, ok := cN[ng]; ok {
				matched += min(cc, rc)
			}
		}
		p, r := 0.0, 0.0
		if cTot > 0 {
			p = float64(matched) / float64(cTot)
		}
		if rTot > 0 {
			r = float64(matched) / float64(rTot)
		}
		sumP += p
		sumR += r
		orders++
	}
	if orders == 0 {
		return 0
	}
	avgP, avgR := sumP/float64(orders), sumR/float64(orders)
	if avgP == 0 && avgR == 0 {
		return 0
	}
	return 100 * (1 + beta2) * avgP * avgR / (beta2*avgP + avgR)
}

func charNgrams(s string, n int) map[string]int {
	runes := []rune(s)
	m := make(map[string]int)
	for i := 0; i+n <= len(runes); i++ {
		m[string(runes[i:i+n])]++
	}
	return m
}

func countTotal(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// ── metric/parse unit tests (CI; no LLM) ──

func TestChrF(t *testing.T) {
	if got := chrF("안녕하세요 세계", "안녕하세요 세계"); got < 99.9 {
		t.Fatalf("identical strings should score ~100, got %.2f", got)
	}
	if got := chrF("", "안녕하세요"); got != 0 {
		t.Fatalf("empty candidate should score 0, got %.2f", got)
	}
	full := chrF("제안서 마감은 다음 주 금요일입니다.", "제안서 마감은 다음 주 금요일입니다.")
	partial := chrF("제안 마감 금요일", "제안서 마감은 다음 주 금요일입니다.")
	none := chrF("hello world", "제안서 마감은 다음 주 금요일입니다.")
	if !(full > partial && partial > none) {
		t.Fatalf("expected full > partial > none, got full=%.1f partial=%.1f none=%.1f", full, partial, none)
	}
	if none < 0 || full > 100 {
		t.Fatalf("chrF out of range: none=%.1f full=%.1f", none, full)
	}
}

func TestJudgeParsing(t *testing.T) {
	winners := map[string]string{
		`{"winner":"A"}`:                   "A",
		`{"winner":"b"}`:                   "B",
		`{"winner":"tie"}`:                 "tie",
		"```json\n{\"winner\":\"A\"}\n```": "A",
		`{"winner":"unsure"}`:              "tie",
		`not json`:                         "tie",
	}
	for raw, want := range winners {
		if got := normalizeWinner(parseWinner(raw)); got != want {
			t.Fatalf("winner parse %q → %q, want %q", raw, got, want)
		}
	}
	for in, want := range map[int]int{0: 1, 1: 1, 3: 3, 5: 5, 9: 5, -2: 1} {
		if got := clampScore(in); got != want {
			t.Fatalf("clampScore(%d)=%d want %d", in, got, want)
		}
	}
}
