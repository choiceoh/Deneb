package codesearch

import (
	"math"
	"strings"
	"testing"
)

func TestFTSQueryQuotesTerms(t *testing.T) {
	got := ftsQuery(`retry "backoff" 로직`)
	want := `"retry" OR "backoff" OR "로직"`
	if got != want {
		t.Fatalf("ftsQuery = %q, want %q", got, want)
	}
}

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	c := []float32{0, 1, 0}
	if v := cosine(a, b); math.Abs(v-1) > 1e-6 {
		t.Fatalf("identical vectors cosine = %v, want 1", v)
	}
	if v := cosine(a, c); math.Abs(v) > 1e-6 {
		t.Fatalf("orthogonal vectors cosine = %v, want 0", v)
	}
	if v := cosine(a, []float32{0, 0}); v != 0 {
		t.Fatalf("mismatched dims cosine = %v, want 0", v)
	}
}

func TestExpandQueryBridgesKorean(t *testing.T) {
	got := expandQuery("음성 전사 화자분리")
	for _, want := range []string{"transcribe", "diarize", "speech"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expandQuery(%q) = %q, missing %q", "음성 전사 화자분리", got, want)
		}
	}
	if got := expandQuery("plain english query"); got != "plain english query" {
		t.Fatalf("english query mutated: %q", got)
	}
}

func TestKindHint(t *testing.T) {
	cases := map[string]string{
		"세션 상태 구조체":         "struct",
		"메일 파서 함수":          "function",
		"ChatViewModel 메서드": "method",
		"승인 반려 클래스":         "class",
		"재시도 백오프":           "",
	}
	for q, want := range cases {
		if got := kindHint(q); got != want {
			t.Fatalf("kindHint(%q) = %q, want %q", q, got, want)
		}
	}
}
