package tokenest

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The counts below are what the ST engine's own tokenizer produces
// (st-glm53-meta/tokenizer.json, the file boot.py loads with --ckpt-meta,
// vocab 154,856). They were measured on 2026-09-13 with truncation disabled —
// that file ships a max_length 2048 truncation rule that silently caps counts
// otherwise, which the engine's boot.py documents as a trap.
//
// The strings are written for this test. Operator wiki and transcript content
// was used to FIT the ratios but is deliberately not copied into the repo.
var glmFixtures = []struct {
	name   string
	text   string
	tokens int // measured, not estimated
}{
	{"korean_prose", "오늘 회의에서 결정된 사항을 정리해서 내일 오전까지 공유해 주세요. 담당자는 각자 맡은 항목의 진행 상황을 한 줄씩 적어 주시면 됩니다.", 60},
	{"korean_mixed", "게이트웨이(gateway-go)의 chat 파이프라인에서 compaction 이 실행되는 시점은 history budget 을 넘길 때입니다. 자세한 내용은 docs/agent-rules/prompt-cache.md 를 참고하세요.", 59},
	{"english_prose", "The scheduler admits a request only when the entire horizon fits, so a long prompt never displaces a running row halfway through its answer.", 27},
	{"go_code", "func (e *Estimator) Count(text string) int {\n\tn := e.rawCount(text)\n\tif factor := globalCal.factor(e.family); factor != 1.0 {\n\t\tn = int(float64(n)*factor + 0.5)\n\t}\n\treturn n\n}", 57},
	{"json_schema", `{"name":"web","description":"Fetch a URL and return readable text","parameters":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}}`, 35},
	{"korean_list", "- 첫째, 엔진이 내려가면 웜홀이 클라우드로 넘긴다.\n- 둘째, 그 사실이 로그에만 남는다.\n- 셋째, 사용자는 모른다.", 59},
	{"korean_everyday", "서울에서 맛있는 김치를 먹었습니다", 14},
	{"ascii_json_small", `{"role":"assistant","content":"Hello world"}`, 10},
}

// TestRuneEstimateStaysNearMeasuredGLMCounts pins the calibration to real
// tokenizer output. The band is wide on purpose: a rune-class heuristic cannot
// follow BPE merges on short strings, and everyday Korean ("korean_everyday")
// merges far better than the technical Korean this deployment actually budgets.
// What the test protects is the aggregate, which is what a context budget uses.
func TestRuneEstimateStaysNearMeasuredGLMCounts(t *testing.T) {
	est := ForFamily(FamilyDefault)
	var sumEst, sumReal int
	for _, f := range glmFixtures {
		got := est.rawCount(f.text)
		sumEst += got
		sumReal += f.tokens
		ratio := float64(got) / float64(f.tokens)
		t.Logf("%-18s real=%3d est=%3d ratio=%.2f", f.name, f.tokens, got, ratio)
		// Dense JSON punctuation is the known worst case (~1.6): the BPE merges
		// `":"` and `","` runs that this heuristic prices rune by rune.
		if ratio < 0.85 || ratio > 1.65 {
			t.Errorf("%s: est=%d real=%d ratio=%.2f outside 0.85-1.65", f.name, got, f.tokens, ratio)
		}
	}
	total := float64(sumEst) / float64(sumReal)
	t.Logf("aggregate est=%d real=%d ratio=%.3f", sumEst, sumReal, total)
	// Over-counting is the safe direction (compact early), under-counting
	// overflows the window. Refuse a calibration that drifts under.
	if total < 1.0 || total > 1.25 {
		t.Errorf("aggregate ratio %.3f outside 1.00-1.25", total)
	}
}

// TestByteEstimateFallsWithMultibyteContent is the regression guard for the bug
// this calibration fixed: byteDivisor interpolated 4.0 → 4.5 bytes per token as
// content became multi-byte, when the truth runs the other way. A Hangul
// syllable is 3 UTF-8 bytes and costs more than one token (~2.2 bytes/token);
// an ASCII run is ~3.5. The old direction under-counted Korean by 47%.
func TestByteEstimateFallsWithMultibyteContent(t *testing.T) {
	ascii := byteDivisor([]byte("the quick brown fox jumps over the lazy dog again and again"))
	korean := byteDivisor([]byte("서울에서 맛있는 김치를 먹었습니다 오늘 회의에서 결정된 사항을 정리했습니다"))
	t.Logf("bytes per token: ascii=%.2f korean=%.2f", ascii, korean)
	if korean >= ascii {
		t.Errorf("korean bytes/token %.2f must be BELOW ascii %.2f — multibyte content costs more tokens per byte", korean, ascii)
	}
	if korean < 2.1 || korean > 2.9 {
		t.Errorf("korean bytes/token %.2f outside the measured 2.1-2.9 band", korean)
	}
	if ascii < 3.3 || ascii > 4.0 {
		t.Errorf("ascii bytes/token %.2f outside the measured 3.3-4.0 band", ascii)
	}
}

// TestByteEstimateNoLongerUnderCountsKorean checks the aggregate the feedback
// loop actually consumes: recordTokenFeedback estimates with CountBytes, so a
// byte path that under-counts teaches the calibrator a correction that has
// nothing to do with the tokenizer.
func TestByteEstimateNoLongerUnderCountsKorean(t *testing.T) {
	est := ForFamily(FamilyDefault)
	var sumEst, sumReal int
	for _, f := range glmFixtures {
		sumEst += est.rawCountBytes([]byte(f.text))
		sumReal += f.tokens
	}
	ratio := float64(sumEst) / float64(sumReal)
	t.Logf("byte aggregate est=%d real=%d ratio=%.3f", sumEst, sumReal, ratio)
	if ratio < 0.98 || ratio > 1.25 {
		t.Errorf("byte aggregate ratio %.3f outside 0.98-1.25", ratio)
	}
}

// TestCalibrationGenerationDropsStaleFactors: a correction factor learned
// against other ratios is not a correction for these. The file on disk when
// this calibration landed held 0.81 over 29,441 samples, learned against both
// the old ratios and a contaminated signal.
func TestCalibrationGenerationDropsStaleFactors(t *testing.T) {
	dir := t.TempDir()
	globalCal.mu.Lock()
	globalCal.entries[FamilyDefault] = calEntry{Factor: 0.81, Samples: 29441}
	globalCal.mu.Unlock()
	if err := SaveCalibration(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Simulate the next release changing the ratios.
	globalCal.mu.Lock()
	globalCal.entries[FamilyDefault] = calEntry{}
	globalCal.mu.Unlock()

	writeVersion(t, dir, calRatioGeneration-1)
	LoadCalibration(dir)
	if f := CorrectionFactor(FamilyDefault); f != 1.0 {
		t.Errorf("stale generation was applied: factor=%v, want 1.0", f)
	}

	writeVersion(t, dir, calRatioGeneration)
	LoadCalibration(dir)
	if f := CorrectionFactor(FamilyDefault); math.Abs(f-0.81) > 1e-9 {
		t.Errorf("current generation was not applied: factor=%v, want 0.81", f)
	}
	globalCal.mu.Lock()
	globalCal.entries[FamilyDefault] = calEntry{}
	globalCal.mu.Unlock()
}

// writeVersion rewrites just the version field of the persisted calibration so
// the loader's generation check can be exercised from both sides.
func writeVersion(t *testing.T, dir string, version int) {
	t.Helper()
	path := filepath.Join(dir, calFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read calibration: %v", err)
	}
	var p calPersist
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal calibration: %v", err)
	}
	p.Version = version
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal calibration: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write calibration: %v", err)
	}
}
