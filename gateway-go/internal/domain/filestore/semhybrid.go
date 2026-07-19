// semhybrid.go — hybrid (lexical BM25 + dense-vector) file search.
//
// Search (semindex.go) ranks files purely by chunk cosine and cuts everything
// below an absolute Korean-calibrated floor (minSemanticScore, embedder-
// calibrated — see semindex.go). That floor rejects the noise band cleanly,
// but pays for it two ways:
//
//   - an exact match phrased oddly can dip just under the floor and vanish, and
//   - a file whose NAME or text literally contains the query gets no credit for
//     that lexical hit — meaning is the only signal.
//
// HybridSearch adds the lexical signal back. It fuses the semantic ranking with
// a BM25 ranking over the same indexed text (lexscore.go) using Reciprocal Rank
// Fusion, and admits a file when EITHER signal is convincing:
//
//   - its best chunk cosine clears the semantic floor (a genuine meaning match), OR
//   - it has a strong lexical match (a query token in the file NAME, or BM25
//     evidence above a corpus-relative bar).
//
// The OR-gate is the whole point: it preserves the floor's clean rejection of
// purely-semantic noise (a below-floor cosine with no lexical overlap is still cut)
// while letting exact name/content matches survive below the floor. Like Search,
// it degrades to an empty result on any embedding failure so callers fall back
// to name/content search.
package filestore

import (
	"context"
	"sort"
	"strings"
)

// Why RRF over a normalized weighted sum.
//
// The two signals live on incompatible scales. Embedding cosine occupies a
// model-specific band (BGE-M3 packed Korean into ~0.58–0.86; Nemotron spreads
// it across ~0.0–0.6), while BM25 is an unbounded sum of IDF·TF terms whose
// magnitude swings with
// query length and corpus IDF. A weighted sum α·cos + (1-α)·norm(bm25) forces a
// choice of normalizer (min-max? sigmoid? per-query?) and an α, both of which
// need re-tuning whenever the corpus or query mix shifts — exactly the brittle
// knob the semindex floor comment warns against. RRF instead consumes only the
// RANK of each item in each list (score = Σ 1/(k+rank)), so it is invariant to
// the raw score scales and needs a single, well-understood constant k. With a
// single-user corpus and no labeled data to fit weights on, rank fusion's
// scale-free robustness is the right default; the calibrated cosine floor is
// kept as the admission GATE (not the ranker), so we lose none of its measured
// noise rejection. (The wiki blends with max(bm25,cosine)+bonus/penalty because
// its BM25 is already sigmoid-normalized to 0–1 and it has no absolute cosine
// floor to anchor on — a different starting point, hence a different choice.)
const (
	// rrfK dampens how steeply rank position is rewarded. The standard value
	// from the original RRF paper (Cormack et al., 2009). Larger k flattens the
	// curve (later ranks matter more); 60 is the widely-used default and needs no
	// corpus-specific tuning — the property that motivated choosing RRF.
	rrfK = 60.0

	// lexStrongBM25Frac gates lexical-only admission relative to the corpus's own
	// BM25 distribution: a file with no semantic support is admitted only if its
	// BM25 reaches this fraction of the best BM25 score for the query. Relative
	// (not absolute) because BM25 magnitude is query/corpus dependent — anchoring
	// to the top hit makes the bar self-scaling. A name match bypasses this (the
	// strongest lexical signal admits on its own).
	lexStrongBM25Frac = 0.5

	// lexMinMatchTokens requires a lexical-only (no-name, no-semantic) admission
	// to match at least this many DISTINCT query tokens, so a single common word
	// (e.g. "파일") shared with an otherwise-unrelated file can't sneak in on BM25
	// alone. A name match or a semantic match is exempt.
	lexMinMatchTokens = 2

	// hybridSemanticK widens the semantic neighbor set scored for the fusion
	// beyond the result cap, so a relevant file just outside the top-`max` still
	// contributes its rank to RRF instead of being invisible to the lexical side.
	hybridSemanticK = 30
)

// HybridSearch ranks files by a fusion of BM25 lexical overlap and dense-vector
// cosine over the same indexed text, returning up to max hits. It admits a file
// when its cosine clears the semantic floor OR it has a strong lexical match
// (name token or corpus-relative BM25), then orders admitted files by Reciprocal
// Rank Fusion of the two signals' ranks.
//
// Degradation mirrors Search: a nil/unhealthy embedder, a too-short query, an
// empty index, or an embed failure yields an empty slice (never an error) so the
// caller falls back to name/content search. extractFn is accepted for signature
// symmetry with the rest of the index API and future use; the lexical side
// scores the already-extracted chunk text held in the index, so it is not called
// here (kept non-nil-required to keep the DI contract uniform).
func (si *SemanticIndex) HybridSearch(ctx context.Context, query string, max int, embed Embedder, extractFn ExtractFunc) ([]ScoredEntry, error) {
	q, qv, entries, max, ok := si.prepareSemanticQuery(ctx, query, max, embed)
	if !ok {
		return nil, nil
	}

	// --- semantic side: best chunk cosine per file, ranked descending ---
	sem := hybridSemanticHits(qv, entries)
	semRank := hybridSemRanks(sem, max)

	// --- lexical side: BM25 over name + chunk text, ranked descending ---
	corpus := buildLexCorpus(entries)
	queryTokens := lexTokenize(q)
	lex := corpus.score(queryTokens)
	lexRank, bestBM25 := hybridLexRanks(lex)

	// --- admission gate + RRF fusion ---
	out := fuseHybridHits(sem, lex, semRank, lexRank, bestBM25, max)

	results := make([]ScoredEntry, 0, len(out))
	for _, h := range out {
		results = append(results, ScoredEntry{
			Entry: Entry{
				Tag:            "file",
				Name:           pathBase(h.path),
				PathDisplay:    h.path,
				PathLower:      strings.ToLower(h.path),
				ID:             h.path,
				Size:           h.size,
				ServerModified: h.mtime,
			},
			// Surface the cosine as the displayed similarity (a stable, familiar
			// 0–1 number) rather than the RRF score, which is a tiny fusion value
			// with no intuitive meaning to a reader. Ranking still uses RRF.
			Score:     h.cos,
			Snippet:   h.snippet,
			StartLine: h.start,
			EndLine:   h.end,
			Kind:      h.kind,
			Heading:   h.heading,
		})
	}
	return results, nil
}

// hybridSemHit is one file's semantic-side evaluation: its best chunk cosine
// against the query vector and that chunk's snippet.
type hybridSemHit struct {
	path    string
	size    int64
	mtime   string
	cos     float64
	snippet string
	start   int
	end     int
	kind    string
	heading string
}

// hybridFusedHit is an admitted file carrying its RRF fusion score (the
// ranking key) alongside the cosine (the displayed similarity).
type hybridFusedHit struct {
	path    string
	size    int64
	mtime   string
	snippet string
	score   float64 // RRF score
	cos     float64
	start   int
	end     int
	kind    string
	heading string
}

// hybridSemanticHits computes each file's best chunk cosine (and its snippet),
// returning one hit per file ranked by cosine descending, path ascending on
// ties.
func hybridSemanticHits(qv []float32, entries []*fileEntry) []hybridSemHit {
	sem := make([]hybridSemHit, 0, len(entries))
	for _, fe := range entries {
		best := -1.0
		bestSnip := ""
		bestStart, bestEnd := 0, 0
		bestKind, bestHeading := "", ""
		for i := range fe.Chunks {
			s := cosine(qv, fe.Chunks[i].Vector)
			if s > best {
				best = s
				bestSnip = fe.Chunks[i].Snippet
				bestStart = fe.Chunks[i].StartLine
				bestEnd = fe.Chunks[i].EndLine
				bestKind = fe.Chunks[i].Kind
				bestHeading = fe.Chunks[i].Heading
			}
		}
		if best < 0 {
			best = 0 // a file with no chunks (text-less) has no semantic signal
		}
		sem = append(sem, hybridSemHit{
			path: fe.Path, size: fe.Size, mtime: fe.MTime, cos: best, snippet: bestSnip,
			start: bestStart, end: bestEnd, kind: bestKind, heading: bestHeading,
		})
	}
	sort.Slice(sem, func(a, b int) bool {
		if sem[a].cos != sem[b].cos {
			return sem[a].cos > sem[b].cos
		}
		return sem[a].path < sem[b].path
	})
	return sem
}

// hybridSemRanks assigns each path its 0-based position in the cosine-ranked
// semantic list, keeping a generous top-K (at least hybridSemanticK) so files
// just outside the result cap still feed their rank into RRF.
func hybridSemRanks(sem []hybridSemHit, max int) map[string]int {
	semCap := max
	if hybridSemanticK > semCap {
		semCap = hybridSemanticK
	}
	semRank := make(map[string]int, len(sem))
	for i, h := range sem {
		if i >= semCap {
			break
		}
		semRank[h.path] = i // 0-based rank
	}
	return semRank
}

// hybridLexRanks ranks the lexical results by BM25 descending (path ascending
// on ties), returning each path's 0-based rank and the query's best BM25 score
// (the anchor for the corpus-relative admission bar).
func hybridLexRanks(lex map[string]lexResult) (map[string]int, float64) {
	type lexRanked struct {
		path  string
		score float64
	}
	lexList := make([]lexRanked, 0, len(lex))
	bestBM25 := 0.0
	for p, r := range lex {
		lexList = append(lexList, lexRanked{path: p, score: r.score})
		if r.score > bestBM25 {
			bestBM25 = r.score
		}
	}
	sort.Slice(lexList, func(a, b int) bool {
		if lexList[a].score != lexList[b].score {
			return lexList[a].score > lexList[b].score
		}
		return lexList[a].path < lexList[b].path
	})
	lexRank := make(map[string]int, len(lexList))
	for i, r := range lexList {
		lexRank[r.path] = i
	}
	return lexRank, bestBM25
}

// admitHybridHit is the OR-gate: a file is admitted (a real hit, not noise)
// when its cosine clears the semantic floor OR it has a strong lexical match.
func admitHybridHit(h hybridSemHit, lex map[string]lexResult, bestBM25 float64) bool {
	lr, hasLex := lex[h.path]
	switch {
	case h.cos >= minSemanticScore:
		return true // genuine meaning match (the original floor)
	case hasLex && lr.nameHit:
		return true // exact name match — the strongest lexical signal
	case hasLex && bestBM25 > 0 && lr.score >= lexStrongBM25Frac*bestBM25 && lr.matched >= lexMinMatchTokens:
		return true // strong, multi-token corpus-relative lexical match
	}
	return false
}

// fuseHybridHits applies the admission gate and orders admitted files by
// Reciprocal Rank Fusion of the two signals' ranks, capped to max. Both
// signals contribute their rank to the RRF score regardless of which gate
// admitted the file, so agreement between the two naturally floats to the top.
func fuseHybridHits(sem []hybridSemHit, lex map[string]lexResult, semRank, lexRank map[string]int, bestBM25 float64, max int) []hybridFusedHit {
	out := make([]hybridFusedHit, 0, len(sem))
	for _, h := range sem {
		if !admitHybridHit(h, lex, bestBM25) {
			continue
		}
		// RRF: sum 1/(k+rank) over the lists this file appears in. A file absent
		// from a list contributes nothing from that list (rank = +inf).
		var score float64
		if r, ok := semRank[h.path]; ok {
			score += 1.0 / (rrfK + float64(r))
		}
		if r, ok := lexRank[h.path]; ok {
			score += 1.0 / (rrfK + float64(r))
		}
		out = append(out, hybridFusedHit{
			path: h.path, size: h.size, mtime: h.mtime,
			snippet: h.snippet, score: score, cos: h.cos, start: h.start, end: h.end,
			kind: h.kind, heading: h.heading,
		})
	}

	// Order by fused score, descending; ties broken by cosine then path so the
	// ordering is deterministic and resumable.
	sort.Slice(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score > out[b].score
		}
		if out[a].cos != out[b].cos {
			return out[a].cos > out[b].cos
		}
		return out[a].path < out[b].path
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}
