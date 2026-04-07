package tokenest

import (
	"strings"
	"testing"
)

func TestEstimate_Empty(t *testing.T) {
	if got := Estimate(""); got != 0 {
		t.Errorf("Estimate(\"\") = %d, want 0", got)
	}
}

func TestEstimate_SingleChar(t *testing.T) {
	// Single character should return at least 1.
	if got := Estimate("a"); got < 1 {
		t.Errorf("Estimate(\"a\") = %d, want >= 1", got)
	}
}

func TestCount_PureEnglish(t *testing.T) {
	est := ForFamily(FamilyClaude)
	// "The quick brown fox jumps over the lazy dog" = 44 chars
	// English: ~4 runes/token → expect ~11 tokens (plus spaces).
	text := "The quick brown fox jumps over the lazy dog"
	got := est.Count(text)
	// Should be in a reasonable range: 10-16 tokens.
	if got < 8 || got > 20 {
		t.Errorf("Claude English estimate = %d, want 8-20 for %q", got, text)
	}
	t.Logf("Claude English: %q → %d tokens", text, got)
}

func TestCount_PureKorean(t *testing.T) {
	est := ForFamily(FamilyClaude)
	// "서울에서 맛있는 김치를 먹었습니다" = 15 Hangul + 2 spaces
	text := "서울에서 맛있는 김치를 먹었습니다"
	got := est.Count(text)
	// Korean: ~1.5 runes/token → expect ~10-13 tokens.
	if got < 8 || got > 18 {
		t.Errorf("Claude Korean estimate = %d, want 8-18 for %q", got, text)
	}
	t.Logf("Claude Korean: %q → %d tokens", text, got)
}

func TestCount_MixedKoreanEnglish(t *testing.T) {
	est := ForFamily(FamilyClaude)
	text := "안녕하세요 Hello World 테스트입니다"
	got := est.Count(text)
	// Mixed content should be between pure Korean and pure English rates.
	if got < 5 || got > 20 {
		t.Errorf("Claude mixed estimate = %d, want 5-20 for %q", got, text)
	}
	t.Logf("Claude mixed: %q → %d tokens", text, got)
}

func TestCount_CodeContent(t *testing.T) {
	est := ForFamily(FamilyClaude)
	text := `func estimateTokens(s string) int {
	return utf8.RuneCountInString(s) / 2
}`
	got := est.Count(text)
	// Code is mostly ASCII Latin + punctuation + spaces.
	if got < 10 || got > 40 {
		t.Errorf("Claude code estimate = %d, want 10-40 for code snippet", got)
	}
	t.Logf("Claude code: %d tokens for %d chars", got, len(text))
}

func TestCount_Numbers(t *testing.T) {
	est := ForFamily(FamilyClaude)
	text := "2024년 4월 7일 오후 3시 15분"
	got := est.Count(text)
	if got < 5 || got > 20 {
		t.Errorf("Claude numbers+Korean estimate = %d, want 5-20", got)
	}
	t.Logf("Claude date: %q → %d tokens", text, got)
}

// TestFamilyDifferences verifies that different families produce different
// estimates for Korean-heavy content (the primary differentiator).
func TestFamilyDifferences(t *testing.T) {
	text := "서울특별시에서 인공지능 연구를 진행하고 있습니다"

	claude := ForFamily(FamilyClaude).Count(text)
	openai := ForFamily(FamilyOpenAI).Count(text)

	// OpenAI should estimate MORE tokens for Korean (weaker Korean coverage).
	if openai <= claude {
		t.Errorf("OpenAI Korean tokens (%d) should be > Claude (%d)", openai, claude)
	}
	t.Logf("Korean estimates — Claude: %d, OpenAI: %d", claude, openai)
}

func TestForModel_Resolution(t *testing.T) {
	tests := []struct {
		modelID string
		want    Family
	}{
		{"claude-sonnet-4.6", FamilyClaude},
		{"claude-opus-4.6", FamilyClaude},
		{"claude-3-haiku-20240307", FamilyClaude},
		{"gpt-5.4-mini", FamilyOpenAI},
		{"gpt-5.4", FamilyOpenAI},
		{"gpt-5.3-codex", FamilyOpenAI},
		{"o1-mini", FamilyOpenAI},
		{"o3-mini", FamilyOpenAI},
		{"o4-mini", FamilyOpenAI},
		{"gemini-3.1-pro-preview", FamilyGemini},
		{"gemma-4-26B", FamilyGemini},
		{"google/gemma-4-26B-A4B-it", FamilyGemini},
		{"glm-5-turbo", FamilyDefault},
		{"doubao-pro-32k", FamilyDefault},
		{"unknown-model", FamilyDefault},
	}
	for _, tt := range tests {
		got := ForModel(tt.modelID).Family()
		if got != tt.want {
			t.Errorf("ForModel(%q).Family() = %d, want %d", tt.modelID, got, tt.want)
		}
	}
}

// TestEstimate_BetterThanRuneDiv2 demonstrates improvement over the old
// rune/2 approach for English-heavy content.
func TestEstimate_BetterThanRuneDiv2(t *testing.T) {
	// The old approach: rune_count / 2.
	// For pure English, this massively overestimates.
	text := "The quick brown fox jumps over the lazy dog near the river"
	runeDiv2 := len([]rune(text)) / 2
	scriptAware := ForFamily(FamilyClaude).Count(text)

	t.Logf("English text: rune/2=%d, script-aware=%d", runeDiv2, scriptAware)
	// Script-aware should produce fewer tokens for English (more accurate).
	if scriptAware >= runeDiv2 {
		t.Errorf("script-aware (%d) should be < rune/2 (%d) for English text",
			scriptAware, runeDiv2)
	}
}

// TestEstimate_KoreanMoreAccurate demonstrates improvement for Korean text.
func TestEstimate_KoreanMoreAccurate(t *testing.T) {
	// Old approach: rune/2 underestimates Korean because Hangul syllables
	// are ~1.5 runes/token, not 2.
	text := "대한민국 서울특별시 강남구에서 열리는 국제 인공지능 학회에 참석했습니다"
	runeDiv2 := len([]rune(text)) / 2
	scriptAware := ForFamily(FamilyClaude).Count(text)

	t.Logf("Korean text: rune/2=%d, script-aware=%d", runeDiv2, scriptAware)
	// Script-aware should produce more tokens for Korean (corrects underestimate).
	if scriptAware <= runeDiv2 {
		t.Errorf("script-aware (%d) should be > rune/2 (%d) for Korean text",
			scriptAware, runeDiv2)
	}
}

func TestEstimateBytes_Empty(t *testing.T) {
	if got := EstimateBytes(nil); got != 0 {
		t.Errorf("EstimateBytes(nil) = %d, want 0", got)
	}
	if got := EstimateBytes([]byte{}); got != 0 {
		t.Errorf("EstimateBytes([]) = %d, want 0", got)
	}
}

func TestEstimateBytes_ASCII(t *testing.T) {
	data := []byte(`{"role":"assistant","content":"Hello world"}`)
	got := EstimateBytes(data)
	// Pure ASCII: ~4 bytes/token → ~11 tokens for 44 bytes.
	if got < 8 || got > 15 {
		t.Errorf("EstimateBytes(ASCII JSON) = %d, want 8-15", got)
	}
	t.Logf("ASCII JSON %d bytes → %d tokens", len(data), got)
}

func TestEstimateBytes_Korean(t *testing.T) {
	data := []byte("서울에서 맛있는 김치를 먹었습니다")
	got := EstimateBytes(data)
	// Korean UTF-8: ~4.5 bytes/token.
	if got < 5 || got > 20 {
		t.Errorf("EstimateBytes(Korean) = %d, want 5-20", got)
	}
	t.Logf("Korean %d bytes → %d tokens", len(data), got)
}

func TestCountBytes_DivisorStability(t *testing.T) {
	est := ForFamily(FamilyClaude)
	// Verify that the byte estimate is within 2x of the rune estimate
	// for the same content.
	text := "안녕하세요 Hello World 테스트입니다 1234"
	runeEst := est.Count(text)
	byteEst := est.CountBytes([]byte(text))

	ratio := float64(byteEst) / float64(runeEst)
	if ratio < 0.3 || ratio > 3.0 {
		t.Errorf("byte/rune ratio = %.2f, want 0.3-3.0 (rune=%d, byte=%d)",
			ratio, runeEst, byteEst)
	}
	t.Logf("Mixed: rune=%d, byte=%d, ratio=%.2f", runeEst, byteEst, ratio)
}

func TestClassifyRune(t *testing.T) {
	tests := []struct {
		r    rune
		want scriptClass
	}{
		{'a', classLatin},
		{'Z', classLatin},
		{'0', classDigit},
		{'9', classDigit},
		{' ', classSpace},
		{'\n', classSpace},
		{'\t', classSpace},
		{'.', classPunct},
		{'{', classPunct},
		{'가', classHangul},
		{'힣', classHangul},
		{'中', classCJK},
		{'あ', classCJK},
		{'ア', classCJK},
	}
	for _, tt := range tests {
		got := classifyRune(tt.r)
		if got != tt.want {
			t.Errorf("classifyRune(%q) = %d, want %d", tt.r, got, tt.want)
		}
	}
}

// TestEstimate_LargeText verifies performance doesn't degrade on large input.
func TestEstimate_LargeText(t *testing.T) {
	// 100KB of mixed Korean/English.
	chunk := "서울 Seoul 테스트 test 1234 !@# "
	text := strings.Repeat(chunk, 4000) // ~100KB
	got := Estimate(text)
	if got < 1000 {
		t.Errorf("Estimate(100KB) = %d, want > 1000", got)
	}
	t.Logf("100KB mixed: %d tokens (%d runes)", got, len([]rune(text)))
}

func BenchmarkEstimate_Short(b *testing.B) {
	text := "안녕하세요 Hello World"
	for b.Loop() {
		Estimate(text)
	}
}

func BenchmarkEstimate_Medium(b *testing.B) {
	text := strings.Repeat("서울에서 맛있는 김치를 먹었습니다. ", 100) // ~1.6KB
	for b.Loop() {
		Estimate(text)
	}
}

func BenchmarkEstimate_Large(b *testing.B) {
	text := strings.Repeat("The quick brown fox 점프합니다. ", 1000) // ~30KB
	for b.Loop() {
		Estimate(text)
	}
}

func BenchmarkEstimateBytes_Large(b *testing.B) {
	data := []byte(strings.Repeat("The quick brown fox 점프합니다. ", 1000))
	for b.Loop() {
		EstimateBytes(data)
	}
}
