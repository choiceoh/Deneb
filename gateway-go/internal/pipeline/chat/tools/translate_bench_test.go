package tools

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Translation quality + speed benchmark for the in-app browser translator
// (en/ru → ko). Reuses the production prompt/parse (buildTranslatePrompt /
// parseTranslations), so it measures the exact translate logic — just against a
// caller-chosen endpoint/model so you can compare candidates while tuning the
// `translation` model role.
//
// Quality = chrF (character n-gram F-score) vs the references below: deterministic
// and reproducible (no judge model), which is what you want to RANK models.
// Speed = batch throughput (the in-place path ships batches) + per-segment p50/p90.
//
// CI runs only TestChrF (the metric itself). The live benchmark is gated — it
// needs a local LLM — so it skips unless DENEB_TRANSLATE_LIVE is set:
//
//	DENEB_TRANSLATE_LIVE=1 DENEB_TRANSLATE_MODEL=<served-name> \
//	  [DENEB_TRANSLATE_URL=http://127.0.0.1:8000/v1] [DENEB_TRANSLATE_KEY=...] \
//	  go test -run TestTranslateBench_Live -v ./internal/pipeline/chat/tools/

type benchCase struct {
	lang string
	src  string
	ref  string // a Korean reference translation (approximate; consistent for ranking)
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

// TestTranslateBench_Live measures translation quality (chrF) and speed against a
// live LLM endpoint. Gated; see the file comment for the run command.
func TestTranslateBench_Live(t *testing.T) {
	if os.Getenv("DENEB_TRANSLATE_LIVE") == "" {
		t.Skip("set DENEB_TRANSLATE_LIVE=1 (needs a local LLM endpoint) to run the translate benchmark")
	}
	model := strings.TrimSpace(os.Getenv("DENEB_TRANSLATE_MODEL"))
	if model == "" {
		t.Skip("set DENEB_TRANSLATE_MODEL=<served model name> to benchmark")
	}
	base := envOrDefault("DENEB_TRANSLATE_URL", "http://127.0.0.1:8000/v1")
	client := llm.NewClient(base, os.Getenv("DENEB_TRANSLATE_KEY"))

	srcs := make([]string, len(translateBenchCorpus))
	for i, c := range translateBenchCorpus {
		srcs[i] = c.src
	}

	// Batch path (what the in-place translator actually does): one call, total latency.
	batchStart := time.Now()
	outputs := benchTranslate(t, client, model, srcs)
	batchElapsed := time.Since(batchStart)

	var sumF float64
	t.Logf("── per-segment quality (model=%s) ──", model)
	for i, c := range translateBenchCorpus {
		f := chrF(outputs[i], c.ref)
		sumF += f
		t.Logf("[%s] chrF=%5.1f  %q → %q  (ref %q)", c.lang, f, c.src, outputs[i], c.ref)
	}
	meanF := sumF / float64(len(translateBenchCorpus))

	// Per-segment latency (a small batch ships as the user scrolls).
	lats := make([]time.Duration, 0, len(srcs))
	for _, s := range srcs {
		st := time.Now()
		benchTranslate(t, client, model, []string{s})
		lats = append(lats, time.Since(st))
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	p50 := lats[len(lats)/2]
	p90 := lats[(len(lats)*9)/10]
	var sum time.Duration
	for _, d := range lats {
		sum += d
	}
	mean := sum / time.Duration(len(lats))
	segPerSec := float64(len(srcs)) / batchElapsed.Seconds()

	t.Logf("════ TRANSLATE BENCH ════")
	t.Logf("model=%s  url=%s  segments=%d", model, base, len(srcs))
	t.Logf("QUALITY  mean chrF = %.1f / 100", meanF)
	t.Logf("SPEED    batch: %v total, %.1f seg/s   | per-segment: p50=%v p90=%v mean=%v",
		batchElapsed.Round(time.Millisecond), segPerSec,
		p50.Round(time.Millisecond), p90.Round(time.Millisecond), mean.Round(time.Millisecond))
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

// chrF is the character n-gram F-score (orders 1..6, β=2: recall-weighted), a
// standard MT metric well-suited to Korean (character-level, no word tokenization).
// Returns 0..100. Whitespace is stripped so spacing variants don't skew the score.
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

// TestChrF verifies the metric (runs in CI; no LLM): identical ≈ 100, empty = 0,
// and monotonicity (closer translation scores higher).
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
