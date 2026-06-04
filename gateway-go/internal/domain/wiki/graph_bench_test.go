package wiki

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
)

// graph_bench_test.go — measures the in-process wiki graph traversal
// (graphScoreMap) against operator-aligned, LLM-judge-graded page pairs, so
// signal choices — notably the body-mention pass — are tuned by evidence, not
// intuition.
//
// The judge labels live at ~/.deneb/wiki-graph/bench-grades.json, keyed by
// "seedPath\x00candPath" -> {grade 0..3, reason} where 0=unrelated, 1=weak
// (shared vendor/person only), 2=same site/different work, 3=same project or an
// explicit cross-reference. They grade the *relationship* between two pages,
// independent of any scorer, so the same labels measure any candidate ranking.
// Re-scoring is instant (no LLM); this run only reads them.
//
// It scores every graded pair twice — body mentions on and off — and reports
// Spearman rank-correlation, strong-vs-weak AUC, and per-grade mean score for
// each, so graphMentionsEnabled can be set from the comparison.
//
//	DENEB_WIKI_BENCH=1 go test -run TestGraphBench -v ./internal/domain/wiki/
func TestGraphBench(t *testing.T) {
	if os.Getenv("DENEB_WIKI_BENCH") == "" {
		t.Skip("set DENEB_WIKI_BENCH=1 to score against ~/.deneb/wiki-graph/bench-grades.json")
	}
	home, _ := os.UserHomeDir()
	store, err := NewStore(filepath.Join(home, ".deneb", "wiki"), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".deneb", "wiki-graph", "bench-grades.json"))
	if err != nil {
		t.Fatalf("read grades (run the judge pass first): %v", err)
	}
	type benchLabel struct {
		Grade  int    `json:"grade"`
		Reason string `json:"reason"`
	}
	labels := map[string]benchLabel{}
	if err := json.Unmarshal(raw, &labels); err != nil {
		t.Fatalf("parse grades: %v", err)
	}

	// Group graded candidates by seed page (keys are "seedPath\x00candPath").
	bySeed := map[string]map[string]int{}
	for key, lab := range labels {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		if bySeed[parts[0]] == nil {
			bySeed[parts[0]] = map[string]int{}
		}
		bySeed[parts[0]][parts[1]] = lab.Grade
	}
	t.Logf("loaded %d graded pairs across %d seeds", len(labels), len(bySeed))

	ctx := context.Background()
	configs := []struct {
		name      string
		mentions  bool
		crossRefs bool
	}{
		{"explicit + tags + mentions (current main)", true, false},
		{"+ body cross-references (per-seed AUC best, but adds false edges)", true, true},
	}
	for _, cfg := range configs {
		var scoreArr, gradeArr []float64
		gradeScores := map[int][]float64{}
		perSeed := map[string][][2]float64{}
		for seedPath, cands := range bySeed {
			// Pin the seed to the exact graded page (titles can be ambiguous).
			recs, seed, best, gerr := store.graphScoreMap(ctx, "", cfg.mentions, seedPath)
			if gerr != nil || seed < 0 {
				t.Logf("seed %q did not resolve — skipping", seedPath)
				continue
			}
			byPath := make(map[string]float64, len(best))
			for idx, n := range best {
				byPath[recs[idx].relPath] = n.score
			}
			// Simulate the write-path: a recovered cross-reference becomes an
			// explicit edge (score 1.0), exactly what enrichCrossRefs persists.
			if cfg.crossRefs {
				sigs := crossRefSignatures(recs)
				for ci := range crossRefsFrom(recs, seed, sigs) {
					if p := recs[ci].relPath; byPath[p] < 1.0 {
						byPath[p] = 1.0
					}
				}
			}
			for candPath, grade := range cands {
				sc := byPath[candPath] // 0 when the graph left it unconnected
				scoreArr = append(scoreArr, sc)
				gradeArr = append(gradeArr, float64(grade))
				gradeScores[grade] = append(gradeScores[grade], sc)
				perSeed[seedPath] = append(perSeed[seedPath], [2]float64{sc, float64(grade)})
				if cfg.crossRefs && grade >= 2 {
					t.Logf("  [strong grade %d → %.2f] %s", grade, sc, strings.TrimSuffix(filepath.Base(candPath), ".md"))
				}
				if cfg.crossRefs && grade <= 1 && sc >= 1.0 {
					t.Logf("  [weak grade %d → %.2f (cross-ref'd) seed=%s] %s", grade, sc,
						strings.TrimSuffix(filepath.Base(seedPath), ".md"), strings.TrimSuffix(filepath.Base(candPath), ".md"))
				}
			}
		}
		t.Logf("──────────── %s (%d pairs) ────────────", cfg.name, len(scoreArr))
		for g := 0; g <= 3; g++ {
			if v := gradeScores[g]; len(v) > 0 {
				t.Logf("  grade %d (n=%2d): mean score = %.2f", g, len(v), benchMean(v))
			}
		}
		t.Logf("  Spearman(score, grade)      : %+.3f", benchSpearman(scoreArr, gradeArr))
		t.Logf("  strong-vs-weak AUC  global  : %.3f", benchPairwiseAUC(scoreArr, gradeArr))
		if mAUC, ns := benchMacroAUC(perSeed); ns > 0 {
			t.Logf("  strong-vs-weak AUC  per-seed: %.3f  (avg over %d seeds)", mAUC, ns)
		}
	}

	if os.Getenv("DENEB_WIKI_BENCH_EMBED") == "" {
		return // embedding pass is opt-in: it needs the BGE-M3 sidecar and embeds every page (~2 min)
	}
	// Embedding cosine — does dense semantic similarity (BGE-M3, already wired
	// for compaction) rank the graded pairs better than the token/edge scorer?
	// The metric you read matters. GLOBALLY (pooling all seeds) cosine looks
	// worse — AUC ~0.78 < the token scorer's 0.80 — because every page sits near
	// a high 0.78 floor in this single-domain corpus, and each seed's floor
	// differs, so pooling lets a weak pair under a high-floor seed outrank a
	// strong pair under a low-floor seed. But PER-SEED — which is how the tool
	// actually ranks, one seed's neighbors — the floor cancels and cosine reaches
	// AUC ~0.82, edging out the token scorer (~0.80). So dense similarity is a
	// real signal once measured the way it's used; a cross-encoder reranker
	// (relevance, not similarity) would likely sharpen it further but needs a
	// sidecar Deneb doesn't run. Skipped when the sidecar is absent (e.g. CI).
	emb := embedding.New("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < 50 && !emb.IsHealthy(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !emb.IsHealthy() {
		t.Logf("embedding sidecar (BGE-M3 :8001) not healthy — skipping cosine measurement")
		return
	}
	// Embed every page, then cosine each graded pair. The store's own refresh
	// sends all pages in one batch, which exceeds the 30s client timeout on the
	// CPU sidecar (~1s/page), so embed in small batches here.
	relPaths, _ := store.ListPages("")
	var paths, texts []string
	for _, rp := range relPaths {
		p, perr := store.ReadPage(rp)
		if perr != nil || p == nil {
			continue
		}
		txt := semanticText(p)
		if len([]rune(txt)) < semanticMinChars {
			continue
		}
		paths = append(paths, rp)
		texts = append(texts, txt)
	}
	vecByPath := make(map[string][]float32, len(paths))
	const embedBatch = 16
	for i := 0; i < len(texts); i += embedBatch {
		end := min(i+embedBatch, len(texts))
		vecs, err := emb.Embed(ctx, texts[i:end])
		if err != nil {
			t.Logf("embed batch [%d:%d] failed (%v) — skipping cosine measurement", i, end, err)
			return
		}
		for j, v := range vecs {
			vecByPath[paths[i+j]] = v
		}
	}
	var eScore, eGrade []float64
	eByGrade := map[int][]float64{}
	ePerSeed := map[string][][2]float64{}
	for seedPath, cands := range bySeed {
		sv, ok := vecByPath[seedPath]
		if !ok {
			continue
		}
		for candPath, grade := range cands {
			sc := 0.0
			if cv, ok := vecByPath[candPath]; ok {
				sc = cosine(sv, cv)
			}
			eScore = append(eScore, sc)
			eGrade = append(eGrade, float64(grade))
			eByGrade[grade] = append(eByGrade[grade], sc)
			ePerSeed[seedPath] = append(ePerSeed[seedPath], [2]float64{sc, float64(grade)})
		}
	}
	t.Logf("──────────── embedding cosine (BGE-M3, %d pairs) ────────────", len(eScore))
	for g := 0; g <= 3; g++ {
		if v := eByGrade[g]; len(v) > 0 {
			t.Logf("  grade %d (n=%2d): mean cosine = %.3f", g, len(v), benchMean(v))
		}
	}
	t.Logf("  Spearman(cosine, grade)     : %+.3f", benchSpearman(eScore, eGrade))
	t.Logf("  strong-vs-weak AUC  global  : %.3f", benchPairwiseAUC(eScore, eGrade))
	if mAUC, ns := benchMacroAUC(ePerSeed); ns > 0 {
		t.Logf("  strong-vs-weak AUC  per-seed: %.3f  (avg over %d seeds)", mAUC, ns)
	}

	// Hybrid — token/edge structure + alpha*cosine re-ranking. Explicit edges
	// (token 1.0) still dominate; cosine orders the rest and breaks ties. Does
	// combining beat either signal alone, per-seed? Tunes the wiring's weight.
	t.Logf("──────────── hybrid: token + alpha*cosine ────────────")
	for _, alpha := range []float64{0.3, 0.5, 0.8, 1.5} {
		hPerSeed := map[string][][2]float64{}
		for seedPath, cands := range bySeed {
			recs, seed, best, gerr := store.graphScoreMap(ctx, "", true, seedPath)
			if gerr != nil || seed < 0 {
				continue
			}
			tok := make(map[string]float64, len(best))
			for idx, n := range best {
				tok[recs[idx].relPath] = n.score
			}
			sv, ok := vecByPath[seedPath]
			if !ok {
				continue
			}
			for candPath, grade := range cands {
				h := tok[candPath]
				if cv, ok := vecByPath[candPath]; ok {
					h += alpha * cosine(sv, cv)
				}
				hPerSeed[seedPath] = append(hPerSeed[seedPath], [2]float64{h, float64(grade)})
			}
		}
		if mAUC, ns := benchMacroAUC(hPerSeed); ns > 0 {
			t.Logf("  alpha=%.1f  per-seed AUC: %.3f  (over %d seeds)", alpha, mAUC, ns)
		}
	}
}

func benchMean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// benchSpearman is Pearson correlation on the rank-transformed inputs (average
// ranks for ties): +1 perfect agreement, 0 none, -1 inverted.
func benchSpearman(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 2 {
		return 0
	}
	return benchPearson(benchRanks(x), benchRanks(y))
}

func benchRanks(v []float64) []float64 {
	n := len(v)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	ranks := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		avg := float64(i+j)/2.0 + 1.0 // shared average rank for the tie group
		for k := i; k <= j; k++ {
			ranks[idx[k]] = avg
		}
		i = j + 1
	}
	return ranks
}

func benchPearson(x, y []float64) float64 {
	n := float64(len(x))
	var sx, sy, sxx, syy, sxy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxx += x[i] * x[i]
		syy += y[i] * y[i]
		sxy += x[i] * y[i]
	}
	den := math.Sqrt((n*sxx - sx*sx) * (n*syy - sy*sy))
	if den == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

// benchPairwiseAUC is the probability a strong pair (grade>=2) outscores a weak
// one (grade<=1), ties counting half: 1 perfect separation, 0.5 random.
func benchPairwiseAUC(score, grade []float64) float64 {
	auc, _ := benchPairwiseAUCValid(score, grade)
	return auc
}

// benchPairwiseAUCValid additionally reports whether the AUC is defined (the set
// has at least one strong and one weak pair) — needed to average per-seed AUCs
// without counting degenerate single-class seeds.
func benchPairwiseAUCValid(score, grade []float64) (float64, bool) {
	var strong, weak []float64
	for i := range grade {
		if grade[i] >= 2 {
			strong = append(strong, score[i])
		} else {
			weak = append(weak, score[i])
		}
	}
	if len(strong) == 0 || len(weak) == 0 {
		return 0, false
	}
	var wins, total float64
	for _, s := range strong {
		for _, w := range weak {
			total++
			switch {
			case s > w:
				wins++
			case s == w:
				wins += 0.5
			}
		}
	}
	return wins / total, true
}

// benchMacroAUC averages the strong-vs-weak AUC computed within each seed, which
// is how the tool actually ranks (one seed's neighbors). A per-seed floor — like
// the embedding's ~0.78 baseline — cancels here because every comparison shares
// the same seed, unlike the global pooling that mixes seeds with different
// floors. perSeed maps seedPath -> the (score, grade) pairs for that seed.
func benchMacroAUC(perSeed map[string][][2]float64) (float64, int) {
	var sum float64
	var n int
	for _, pairs := range perSeed {
		sc := make([]float64, len(pairs))
		gr := make([]float64, len(pairs))
		for i, p := range pairs {
			sc[i], gr[i] = p[0], p[1]
		}
		if auc, ok := benchPairwiseAUCValid(sc, gr); ok {
			sum += auc
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}
