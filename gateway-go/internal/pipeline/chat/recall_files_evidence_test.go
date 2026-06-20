package chat

import (
	"context"
	"strings"
	"testing"
)

// fakeFileSearch returns canned hits per query, ignoring the limit so the test
// can verify recallFilesEvidence applies its own quota/dedup.
func fakeFileSearch(byQuery map[string][]FileRecallHit) FileRecallFunc {
	return func(_ context.Context, query string, limit int) []FileRecallHit {
		hits := byQuery[query]
		if limit > 0 && len(hits) > limit {
			hits = hits[:limit]
		}
		return hits
	}
}

func TestRecallFilesEvidence_NilSearchNoQueries(t *testing.T) {
	if got := recallFilesEvidence(context.Background(), nil, []string{"q"}); got != nil {
		t.Errorf("nil search should yield nil, got %+v", got)
	}
	search := fakeFileSearch(map[string][]FileRecallHit{"q": {{Path: "/a.txt", Score: 0.8}}})
	if got := recallFilesEvidence(context.Background(), search, nil); got != nil {
		t.Errorf("no queries should yield nil, got %+v", got)
	}
}

func TestRecallFilesEvidence_MapsAndScores(t *testing.T) {
	search := fakeFileSearch(map[string][]FileRecallHit{
		"개발행위허가": {{Path: "/허가/신청서.pdf", Snippet: "제출 서류 안내", Score: 0.81, ModifiedAt: 123}},
	})
	got := recallFilesEvidence(context.Background(), search, []string{"개발행위허가"})
	if len(got) != 1 {
		t.Fatalf("got %d evidence rows, want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "file" {
		t.Errorf("kind = %q, want file", ev.Kind)
	}
	if ev.Source != "/허가/신청서.pdf" {
		t.Errorf("source = %q, want the file path", ev.Source)
	}
	wantScore := recallFilesSourcePrior + 0.81
	if ev.Score < wantScore-1e-9 || ev.Score > wantScore+1e-9 {
		t.Errorf("score = %v, want prior(%.2f)+cosine(0.81) = %v", ev.Score, recallFilesSourcePrior, wantScore)
	}
	if ev.At != 123 {
		t.Errorf("At = %d, want 123 (modified time carried through)", ev.At)
	}
	if !strings.Contains(ev.Note, "/허가/신청서.pdf") || !strings.Contains(ev.Note, "제출 서류 안내") {
		t.Errorf("note should carry path + matched chunk: %q", ev.Note)
	}
}

func TestRecallFilesEvidence_DedupsAcrossQueries(t *testing.T) {
	// The same file matches two different queries; it must appear once.
	hit := FileRecallHit{Path: "/dup.txt", Snippet: "shared", Score: 0.8}
	search := fakeFileSearch(map[string][]FileRecallHit{
		"q1": {hit},
		"q2": {hit},
	})
	got := recallFilesEvidence(context.Background(), search, []string{"q1", "q2"})
	if len(got) != 1 {
		t.Fatalf("duplicate file across queries should collapse to 1 row, got %d: %+v", len(got), got)
	}
}

func TestRecallFilesEvidence_PerQueryQuota(t *testing.T) {
	// The search is called with recallFileQuota as the per-query limit; a source
	// returning more than that is capped by the search itself (fakeFileSearch
	// honors the limit), so a single broad query cannot flood the evidence list.
	var gotLimit int
	search := FileRecallFunc(func(_ context.Context, _ string, limit int) []FileRecallHit {
		gotLimit = limit
		out := make([]FileRecallHit, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, FileRecallHit{Path: "/f" + string(rune('A'+i)), Score: 0.8})
		}
		return out
	})
	got := recallFilesEvidence(context.Background(), search, []string{"q"})
	if gotLimit != recallFileQuota {
		t.Errorf("search called with limit %d, want recallFileQuota %d", gotLimit, recallFileQuota)
	}
	if len(got) > recallFileQuota {
		t.Errorf("evidence rows %d exceed quota %d", len(got), recallFileQuota)
	}
}

func TestRecallFilesEvidence_SkipsBlankPath(t *testing.T) {
	search := fakeFileSearch(map[string][]FileRecallHit{
		"q": {{Path: "  ", Score: 0.9}, {Path: "/real.txt", Score: 0.8}},
	})
	got := recallFilesEvidence(context.Background(), search, []string{"q"})
	if len(got) != 1 || got[0].Source != "/real.txt" {
		t.Fatalf("blank-path hit should be skipped, got %+v", got)
	}
}

// TestBuildRecallPreflight_FilesSourceEndToEnd drives the full preflight with a
// files source wired (no wiki/diary/polaris), proving the task's two scenarios:
// a topical question surfaces the matching file as recall evidence in the tail
// block, and an off-topic question ("오늘 날씨") injects nothing. The fake search
// stands in for the BGE-M3 hybrid index (which needs the live embedding server).
func TestBuildRecallPreflight_FilesSourceEndToEnd(t *testing.T) {
	// The fake returns the file only for queries containing a real signal term of
	// the topical message; the weather message tokenizes to no matching query.
	search := FileRecallFunc(func(_ context.Context, query string, _ int) []FileRecallHit {
		if strings.Contains(query, "개발행위허가") || strings.Contains(query, "신청서") {
			return []FileRecallHit{{
				Path: "/허가/개발행위허가 신청서.pdf", Snippet: "제출 서류 및 절차 안내", Score: 0.82,
			}}
		}
		return nil
	})

	// Topical question → file surfaces.
	out, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "client:main", Message: "개발행위허가 신청서 핵심 알려줘"},
		runDeps{fileRecallFn: search}, nil)
	if !strings.Contains(out, "source=file") {
		t.Fatalf("topical question should surface a file recall row, got:\n%s", out)
	}
	if !strings.Contains(out, "개발행위허가 신청서.pdf") {
		t.Errorf("file path should appear in the recall block, got:\n%s", out)
	}

	// Off-topic question → no file injection (and, with no other source + no cue,
	// the whole preflight returns empty so nothing pollutes the tail).
	weather, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "client:main", Message: "오늘 날씨 어때"},
		runDeps{fileRecallFn: search}, nil)
	if strings.Contains(weather, "source=file") {
		t.Fatalf("off-topic question must not inject any file, got:\n%s", weather)
	}
	if weather != "" {
		t.Errorf("off-topic non-cue turn with no evidence should be empty, got:\n%s", weather)
	}
}

func TestRecallFileConfidence(t *testing.T) {
	high := recallConfidence(recallEvidence{Kind: "file", Score: recallFilesSourcePrior + 0.80})
	if high != "high" {
		t.Errorf("strong file hit confidence = %q, want high", high)
	}
	// A file right at the floor (cosine 0.73 + prior 0.78 = 1.51) is below the 1.55
	// high bar → medium.
	med := recallConfidence(recallEvidence{Kind: "file", Score: recallFilesSourcePrior + 0.73})
	if med != "medium" {
		t.Errorf("floor-level file hit confidence = %q, want medium", med)
	}
}
