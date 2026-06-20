// semhybrid.go — hybrid (lexical BM25 + dense-vector) file search.
//
// Search (semindex.go) ranks files purely by chunk cosine and cuts everything
// below an absolute Korean-calibrated floor (minSemanticScore=0.73). That floor
// rejects the BGE-M3 "Korean noise band" cleanly, but pays for it two ways:
//
//   - an exact match phrased oddly can dip just under 0.73 and vanish, and
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
// purely-semantic noise (a 0.6-band cosine with no lexical overlap is still cut)
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
// The two signals live on incompatible scales. BGE-M3 Korean cosine is packed
// into a high, narrow band (~0.58–0.86 across relevant AND irrelevant pairs),
// while BM25 is an unbounded sum of IDF·TF terms whose magnitude swings with
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
	if si == nil || embed == nil || !embed.IsHealthy() {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if len([]rune(q)) < minChunkRunes {
		return nil, nil // too short to embed meaningfully
	}
	if max <= 0 {
		max = 20
	}

	// Embed the query (best-effort: an embed failure degrades to empty so the
	// caller's lexical fallback runs, exactly like Search).
	qvecs, err := embed.Embed(ctx, []string{q})
	if err != nil || len(qvecs) == 0 {
		return nil, nil //nolint:nilerr // intentional graceful degradation to lexical search
	}
	qv := qvecs[0]

	// Snapshot entry pointers under the lock; do all scoring outside it (pure CPU
	// over immutable chunk vectors + tokens). Entries are replaced wholesale, so
	// retained pointers stay valid even if the map mutates after the snapshot.
	si.mu.Lock()
	entries := make([]*fileEntry, 0, len(si.files))
	for _, fe := range si.files {
		entries = append(entries, fe)
	}
	si.mu.Unlock()
	if len(entries) == 0 {
		return nil, nil
	}

	// --- semantic side: best chunk cosine per file ---
	type semHit struct {
		path    string
		size    int64
		mtime   string
		cos     float64
		snippet string
	}
	sem := make([]semHit, 0, len(entries))
	cosByPath := make(map[string]float64, len(entries))
	snipByPath := make(map[string]string, len(entries))
	for _, fe := range entries {
		best := -1.0
		bestSnip := ""
		for i := range fe.Chunks {
			s := cosine(qv, fe.Chunks[i].Vector)
			if s > best {
				best = s
				bestSnip = fe.Chunks[i].Snippet
			}
		}
		if best < 0 {
			best = 0 // a file with no chunks (text-less) has no semantic signal
		}
		cosByPath[fe.Path] = best
		snipByPath[fe.Path] = bestSnip
		sem = append(sem, semHit{path: fe.Path, size: fe.Size, mtime: fe.MTime, cos: best, snippet: bestSnip})
	}
	// Rank the semantic list by cosine, descending; keep a generous top-K so files
	// just outside the result cap still feed their rank into RRF.
	sort.Slice(sem, func(a, b int) bool {
		if sem[a].cos != sem[b].cos {
			return sem[a].cos > sem[b].cos
		}
		return sem[a].path < sem[b].path
	})
	semRank := make(map[string]int, len(sem))
	semCap := max
	if hybridSemanticK > semCap {
		semCap = hybridSemanticK
	}
	for i, h := range sem {
		if i >= semCap {
			break
		}
		semRank[h.path] = i // 0-based rank
	}

	// --- lexical side: BM25 over name + chunk text ---
	corpus := buildLexCorpus(entries)
	queryTokens := lexTokenize(q)
	lex := corpus.score(queryTokens)
	// Rank the lexical list by BM25, descending.
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

	// --- admission gate + RRF fusion ---
	// A file is admitted (a real hit, not noise) when its cosine clears the
	// semantic floor OR it has a strong lexical match. Both signals contribute
	// their rank to the RRF score regardless of which gate admitted the file, so
	// agreement between the two naturally floats to the top.
	type fused struct {
		path    string
		size    int64
		mtime   string
		snippet string
		score   float64 // RRF score
		cos     float64
	}
	out := make([]fused, 0, len(entries))
	for _, h := range sem {
		lr, hasLex := lex[h.path]
		admit := false
		switch {
		case h.cos >= minSemanticScore:
			admit = true // genuine meaning match (the original floor)
		case hasLex && lr.nameHit:
			admit = true // exact name match — the strongest lexical signal
		case hasLex && bestBM25 > 0 && lr.score >= lexStrongBM25Frac*bestBM25 && lr.matched >= lexMinMatchTokens:
			admit = true // strong, multi-token corpus-relative lexical match
		}
		if !admit {
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
		out = append(out, fused{
			path: h.path, size: h.size, mtime: h.mtime,
			snippet: h.snippet, score: score, cos: h.cos,
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
			Score:   h.cos,
			Snippet: h.snippet,
		})
	}
	return results, nil
}
