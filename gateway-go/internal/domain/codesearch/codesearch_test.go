package codesearch

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
)

type symmetricCodeSearchEmbedder struct {
	calls []string
}

func (e *symmetricCodeSearchEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, "plain")
	return codeSearchTestVectors(texts), nil
}

type roleAwareCodeSearchEmbedder struct {
	calls []string
}

func (e *roleAwareCodeSearchEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, "passage")
	return codeSearchTestVectors(texts), nil
}

func (e *roleAwareCodeSearchEmbedder) EmbedKind(_ context.Context, kind string, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, kind)
	return codeSearchTestVectors(texts), nil
}

func codeSearchTestVectors(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0}
	}
	return out
}

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

type fakeReranker struct{ scores []float64 }

func (f fakeReranker) Rerank(_ context.Context, _ string, docs []string) ([]float64, error) {
	return f.scores[:len(docs)], nil
}
func (f fakeReranker) Identity() string { return "fake" }

func TestRerankHitsReorndersHead(t *testing.T) {
	hits := []Hit{
		{Entry: Entry{ID: "a", Qualified: "A"}},
		{Entry: Entry{ID: "b", Qualified: "B"}},
		{Entry: Entry{ID: "c", Qualified: "C"}},
	}
	out := rerankHits(context.Background(), t.TempDir(), fakeReranker{scores: []float64{0.1, 0.9, 0.5}}, "q", hits)
	if out[0].ID != "b" || out[1].ID != "c" || out[2].ID != "a" {
		t.Fatalf("rerank order = %s,%s,%s want b,c,a", out[0].ID, out[1].ID, out[2].ID)
	}
	// nil reranker → unchanged
	same := rerankHits(context.Background(), t.TempDir(), nil, "q", hits)
	if same[0].ID != "a" {
		t.Fatalf("nil reranker mutated order")
	}
}

func TestSearchUsesQueryRoleWithSymmetricFallback(t *testing.T) {
	dir := t.TempDir()
	meta := Meta{
		Model: "test", Dim: 2,
		Entries: []Entry{{ID: "one", Kind: "function", Qualified: "mail.Search", File: "mail.go", StartLine: 1, EndLine: 2}},
	}
	if err := saveIndex(dir, meta, [][]float32{{1, 0}}); err != nil {
		t.Fatalf("save index: %v", err)
	}

	t.Run("role aware", func(t *testing.T) {
		embedder := &roleAwareCodeSearchEmbedder{}
		hits, err := Search(context.Background(), dir, embedder, "mail lookup", 1)
		if err != nil || len(hits) != 1 || hits[0].ID != "one" {
			t.Fatalf("Search hits = %+v, err %v", hits, err)
		}
		if want := []string{"query"}; !reflect.DeepEqual(embedder.calls, want) {
			t.Fatalf("embedding roles = %v, want %v", embedder.calls, want)
		}
	})

	t.Run("symmetric", func(t *testing.T) {
		embedder := &symmetricCodeSearchEmbedder{}
		hits, err := Search(context.Background(), dir, embedder, "mail lookup", 1)
		if err != nil || len(hits) != 1 || hits[0].ID != "one" {
			t.Fatalf("Search hits = %+v, err %v", hits, err)
		}
		if want := []string{"plain"}; !reflect.DeepEqual(embedder.calls, want) {
			t.Fatalf("embedding fallback = %v, want %v", embedder.calls, want)
		}
	})
}
