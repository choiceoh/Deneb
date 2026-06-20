// search.go — In-memory full-text search for wiki pages.
// Replaces SQLite FTS5 with a pure Go textsearch index.
package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
)

// SearchResult is a single search hit.
type SearchResult struct {
	Path    string  // relative path within wiki dir
	Line    int     // always 0 (line-level matching not available)
	Content string  // matching snippet
	Score   float64 // relevance score (0-1)
}

// searchDB manages the in-memory FTS index for wiki pages.
type searchDB struct {
	idx *textsearch.Index
	mu  sync.RWMutex
	// validity holds the per-page staleness factor (see validityFactor),
	// computed when the page is (re)indexed. Search multiplies scores by it
	// so archived/superseded/aging facts stop outranking current ones.
	validity map[string]float64
}

func newSearchDB() *searchDB {
	return &searchDB{idx: textsearch.New(), validity: make(map[string]float64)}
}

// indexPage upserts a page into the search index.
func (s *searchDB) indexPage(relPath string, page *Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Upsert(relPath, searchablePageFields(page)...)
	if page != nil {
		s.validity[relPath] = validityFactor(page.Meta, time.Now())
	}
}

// removePage removes a page from the search index.
func (s *searchDB) removePage(relPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Remove(relPath)
	delete(s.validity, relPath)
}

// validityFactor scores how current a page's facts are (0–1]. Archived and
// superseded pages keep working for direct reads but should not outrank
// living pages in recall; old "updated" stamps decay gently — operational
// facts (ports, prices, configs) rot, and recall presenting a year-old fact
// as current is exactly the failure this guards against.
func validityFactor(meta Frontmatter, now time.Time) float64 {
	f := 1.0
	if meta.Archived {
		f *= 0.3
	}
	if meta.SupersededBy != "" {
		f *= 0.5
	}
	if meta.Updated != "" {
		if t, err := time.Parse("2006-01-02", meta.Updated); err == nil {
			switch age := now.Sub(t); {
			case age > 365*24*time.Hour:
				f *= 0.7
			case age > 180*24*time.Hour:
				f *= 0.85
			}
		}
	}
	return f
}

// applyValidity multiplies result scores by each page's validity factor and
// re-sorts. Pages never indexed (factor missing) pass through unchanged.
func (s *searchDB) applyValidity(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}
	s.mu.RLock()
	for i := range results {
		if f, ok := s.validity[results[i].Path]; ok && f < 1.0 {
			results[i].Score *= f
		}
	}
	s.mu.RUnlock()
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].Score != results[b].Score {
			return results[a].Score > results[b].Score
		}
		return results[a].Path < results[b].Path
	})
	return results
}

// search runs a full-text query and returns scored results.
func (s *searchDB) search(_ context.Context, query string, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hits := s.idx.Search(query, limit)
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		results[i] = SearchResult{
			Path:    h.ID,
			Content: h.Snippet,
			Score:   scoreToNormalized(h.Score),
		}
	}
	return results, nil
}

// queryMaxRarity returns the highest corpus-rarity (0–1) among the query's
// tokens that exist in the index (see textsearch.QueryMaxRarity). Used to gate
// the lexical (BM25) recall leak: a query whose only matchable tokens are
// corpus-common (rarity below bm25RarityFloor) cannot anchor a trustworthy
// BM25-only hit.
func (s *searchDB) queryMaxRarity(query string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idx.QueryMaxRarity(query)
}

// docCount returns the number of indexed pages (corpus size N). The lexical
// rarity gate needs it to stay disabled for corpora too small to estimate
// "common in corpus" — in a tiny wiki every term looks common (df is a large
// fraction of N) and gating would drop legitimate hits.
func (s *searchDB) docCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idx.Len()
}

// rebuildIndex clears and rebuilds the index from all .md files in dir.
func (s *searchDB) rebuildIndex(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.idx.Clear()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries and directories in walk
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		base := filepath.Base(path)
		if base == "index.md" || base == "_index.md" || base == "log.md" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		page, err := ParsePageFile(path)
		if err != nil {
			// An unparseable page is functionally deleted: it stays on disk but
			// never appears in search again. Surface it instead of hiding it.
			slog.Warn("wiki: unparseable page skipped during search index rebuild",
				"path", rel, "error", err)
			return nil //nolint:nilerr // skip unparseable files
		}
		s.idx.Upsert(rel, searchablePageFields(page)...)
		return nil
	})
}

func searchablePageFields(page *Page) []string {
	if page == nil {
		return nil
	}
	return []string{
		page.Meta.Title,
		page.Meta.Summary,
		page.Meta.ID,
		page.Meta.Category,
		strings.Join(page.Meta.Tags, " "),
		strings.Join(page.Meta.Related, " "),
		page.Body,
	}
}

// close is a no-op (in-memory index, nothing to close).
func (s *searchDB) close() error {
	return nil
}

// Search runs a search across wiki pages. With no embedder configured it is
// pure BM25 (exact prior behavior). When a semantic index is attached and the
// embedding server is healthy, it blends BM25 with dense-vector neighbors so a
// query also finds pages by meaning. Semantic degradation (server down, embed
// error) silently falls back to the BM25 result.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if s.fts == nil {
		return nil, nil
	}
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	bm25, err := s.fts.search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	// Lexical-leak gate: a query whose only matchable tokens are corpus-common
	// (no rare anchor term) produces BM25 hits that matched on a frequent word
	// and are likely off-topic — the measured recall leak. commonOnlyQuery is
	// true for exactly those; it gates UNCONFIRMED lexical hits below.
	//
	// DISABLED below bm25GateMinCorpus: in a tiny wiki every term's df is a large
	// fraction of N, so even a distinctive term scores "common" and the gate
	// would drop legitimate hits (a single-page corpus searched for its one rare
	// word, a 2-page corpus where both pages share the query terms). The leak it
	// guards needs many pages for a common word to collide with off-topic ones;
	// at small N there is little to leak and the conservative choice is gate-off.
	commonOnlyQuery := s.fts.docCount() >= bm25GateMinCorpus &&
		s.fts.queryMaxRarity(query) < bm25RarityFloorValue()

	sem := s.searchSemantic(ctx, query, max(limit, semanticBlendK))
	if len(sem) == 0 {
		// Pure-BM25 path (no embedder / server down / CI). Nothing can confirm a
		// lexical hit here, so a common-only query's hits are all weak matches and
		// are dropped — this is the floorless branch that injected an off-topic
		// page for a single common noun. A query WITH a rare anchor is unaffected.
		if commonOnlyQuery {
			return nil, nil
		}
		return s.fts.applyValidity(bm25), nil
	}
	return s.fts.applyValidity(mergeSearchResults(bm25, sem, limit, commonOnlyQuery)), nil
}

const (
	// semAgreementBonus rewards a BM25 hit confirmed by real semantic similarity
	// (cosine >= semSupportThreshold) — the two signals agreeing is strong
	// evidence of relevance.
	semAgreementBonus = 0.1
	// bm25OnlyPenalty demotes a BM25 hit whose semantic similarity to the query
	// is weak/absent (cosine < semSupportThreshold) — a common query word that
	// matched an otherwise-unrelated page. Without it, max(bm25,cosine) lets such
	// a lexical false positive keep its full BM25 score even when it is
	// semantically off-topic — e.g. "리눅스 파일 권한" matching a "트리나솔라 모듈
	// 계약" page on the shared word "파일". No-op when semantic did not run.
	bm25OnlyPenalty = 0.7
	// semSupportThreshold is the cosine above which a page counts as genuinely
	// related, so a BM25 hit is confirmed rather than a lexical accident.
	// On-topic pages measure ~0.6-0.76; off-topic lexical matches ~0.2-0.3.
	semSupportThreshold = 0.4
	// semanticBlendK widens the semantic neighbor set used for the blend beyond
	// the result limit, so a BM25 hit's cosine is known even when the page is not
	// in the semantic top-N — otherwise a relevant page just outside the top-N
	// would be wrongly demoted as having "no semantic support".
	semanticBlendK = 30
	// semanticOnlyFloor is the cosine a SEMANTIC-ONLY hit (no BM25/lexical match
	// at all) must clear to be admitted. Before this floor the semantic-only
	// branch had NO admission gate: searchSemantic keeps any cosine > 0
	// (semantic.go) and mergeSearchResults' bonus/penalty cases both require
	// inBM25, so a page BM25 never touched kept its full raw cosine and could
	// fill a BM25-empty recall query with an off-topic page (measured: an
	// unrelated wiki page injected at score 0.6302 == raw cosine, no gate).
	//
	// The floor applies ONLY to semantic-only hits. A page with any lexical
	// match (inBM25) is left to the existing bonus/penalty logic — the floor is
	// purely the missing gate on the floorless branch, so lexically-relevant
	// pages (including the bm25OnlyPenalty-demoted ones) are unaffected.
	//
	// Value 0.70 (override via DENEB_WIKI_SEM_FLOOR). Rationale: BGE-M3 packs
	// Korean text into a high, narrow cosine band — even a totally unrelated
	// Korean (query,doc) pair scores ~0.58–0.69 and a genuinely relevant one
	// ~0.77–0.86 (filestore/semindex.go:80-82, measured on the live srv4 :8001).
	// The floor must sit INSIDE that separation band, not at the generic-cosine
	// level. filestore's office-doc corpus has a clean window [0.689, 0.772]
	// (midpoint 0.73), but wiki pages are SHORT curated summaries (title +
	// summary + body), so a genuinely relevant page's cosine can land a notch
	// lower than a full document's — 0.70 is the conservative choice: still above
	// the ~0.69 irrelevant-band ceiling (rejecting the off-topic leak) while
	// leaving more headroom under the relevant band so a terse on-topic summary
	// is not dropped. An srv4 sweep over the real wiki corpus is the final
	// confirmation of the exact value; 0.70 is the measurement-grounded default.
	semanticOnlyFloor = 0.70

	// bm25RarityFloor is the SYMMETRIC counterpart to semanticOnlyFloor, for the
	// lexical (BM25) path. It is the minimum corpus-rarity (textsearch
	// NormalizedRarity, 0–1) the rarest matchable token of a query must clear for
	// a BM25-ONLY hit (no semantic confirmation) to be admitted. A query whose
	// every matchable token is corpus-common (rarity below the floor) is a weak
	// lexical query: its hits matched on a frequent word and are likely off-topic
	// (the measured recall leak — a single common noun like "보고"/"일정" matched
	// unrelated pages at confidence high/medium). Hits from such a query are kept
	// ONLY when semantic similarity independently confirms them.
	//
	// This gate is deliberately MORE conservative than semanticOnlyFloor, by
	// design: semantic similarity scores EVERY page against the query, so a weak
	// semantic-only match is always noise; but a single lexical match CAN be a
	// strong signal when the term is rare (a 거래처명/고유명사 appearing in one
	// page is a legitimate one-term recall). So the gate keys on the term's
	// rarity, not the hit's score — a rare anchor survives, only common-only
	// queries are floored, and it never touches a semantically-confirmed hit.
	//
	// Value 0.55 (override via DENEB_WIKI_BM25_RARITY_FLOOR). Rationale, measured
	// across corpus sizes N∈{22,30,100,200,263} (NormalizedRarity is N-stable,
	// unlike raw IDF or normalized BM25 which both drift with N): a rare term
	// (df 1–3) scores 0.69–1.0 at every N — comfortably above the floor; a noun
	// in ~10–20% of pages (the leak band, e.g. "보고" at df 40/263) scores
	// 0.31–0.52 at realistic N — below it. 0.55 sits in that valley, leaving
	// headroom so a df=2–3 term in even a small corpus (rarity ≥0.69) is never
	// dropped (conservative: when unsure, keep).
	bm25RarityFloor = 0.55

	// bm25GateMinCorpus is the smallest corpus (page count N) at which the
	// lexical rarity gate engages. Below it the gate is OFF: NormalizedRarity is
	// a ratio of IDFs, and when N is small even a df of 2–3 is a large fraction
	// of N so every term reads as "common" (measured: at N=2 a query both pages
	// share scores rarity ~0.26; at N=22 a df=3 term is ~0.69, right at the
	// boundary). The leak the gate guards is a scale phenomenon — a common word
	// needs many pages to collide with off-topic ones — so disabling it for small
	// wikis costs no real protection while preventing false drops in a young or
	// test corpus. 30 is comfortably below the production wiki (~260 pages) and
	// above the band where the rarity ratio is too coarse to trust.
	bm25GateMinCorpus = 30
)

// bm25RarityFloorValue returns the lexical-query rarity floor, honoring the
// DENEB_WIKI_BM25_RARITY_FLOOR override (mirrors semanticOnlyFloorValue). A
// malformed or out-of-(0,1] override is ignored in favor of the default.
func bm25RarityFloorValue() float64 {
	if v := os.Getenv("DENEB_WIKI_BM25_RARITY_FLOOR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			return f
		}
	}
	return bm25RarityFloor
}

// semanticOnlyFloorValue returns the cosine admission floor for semantic-only
// hits, honoring the DENEB_WIKI_SEM_FLOOR override (mirrors filestore's
// minSemanticScore default-plus-override pattern). A malformed or out-of-(0,1]
// override is ignored in favor of the default.
func semanticOnlyFloorValue() float64 {
	if v := os.Getenv("DENEB_WIKI_SEM_FLOOR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			return f
		}
	}
	return semanticOnlyFloor
}

// mergeSearchResults blends lexical (BM25) and semantic hits, scoring each page
// by max(bm25, cosine). A BM25 hit confirmed by real semantic similarity
// (cosine >= semSupportThreshold) gets an agreement bonus; a BM25 hit with
// weak/absent semantic support is demoted (bm25OnlyPenalty) as a likely lexical
// false positive. A semantic-only hit (no lexical match) is admitted only above
// semanticOnlyFloor and then keeps its cosine — the floor is the admission gate
// the floorless semantic-only branch lacked. BM25 snippets are preserved. Order
// is by blended score, descending; ties broken by path.
//
// commonOnlyQuery is the lexical counterpart to that floor: when true (the query
// has no rare anchor term — every matchable token is corpus-common), a BM25 hit
// that semantic does NOT independently confirm (cosine < semSupportThreshold) is
// DROPPED, not merely demoted — it matched only on a frequent word and is the
// measured leak. Semantically-confirmed lexical hits and genuine semantic-only
// hits are unaffected, so a relevant page that happens to share a common word
// still surfaces via its meaning.
func mergeSearchResults(bm25, sem []SearchResult, limit int, commonOnlyQuery bool) []SearchResult {
	type merged struct {
		res       SearchResult
		bm25Score float64
		semCos    float64
		final     float64
		inBM25    bool
		inSem     bool
	}
	byPath := make(map[string]*merged, len(bm25)+len(sem))
	for _, r := range bm25 {
		if m := byPath[r.Path]; m != nil {
			if r.Score > m.bm25Score {
				m.bm25Score = r.Score
			}
			m.inBM25 = true
			if m.res.Content == "" && r.Content != "" {
				m.res.Content = r.Content
			}
			continue
		}
		byPath[r.Path] = &merged{res: r, bm25Score: r.Score, inBM25: true}
	}
	for _, r := range sem {
		if m := byPath[r.Path]; m != nil {
			if r.Score > m.semCos {
				m.semCos = r.Score
			}
			m.inSem = true
			continue
		}
		byPath[r.Path] = &merged{res: r, semCos: r.Score, inSem: true}
	}

	semAvailable := len(sem) > 0
	floor := semanticOnlyFloorValue()
	out := make([]merged, 0, len(byPath))
	for _, m := range byPath {
		// Semantic-only admission floor: a hit with NO lexical match (!inBM25)
		// reaches the result only on its cosine, which the searchSemantic stage
		// gates at >0 only. Without a floor here an off-topic page in the
		// Korean irrelevant cosine band (~0.6) is admitted and can fill a
		// BM25-empty recall query (the measured leak). BM25 hits are never
		// floored — they took a lexical path and keep the existing
		// bonus/penalty treatment below.
		if !m.inBM25 && m.semCos < floor {
			continue
		}
		// Lexical-leak gate (symmetric to the floor above): for a common-only
		// query (no rare anchor term), a lexical hit semantic does not confirm
		// (cosine < semSupportThreshold) matched only on a frequent word — drop
		// it. A semantically-confirmed lexical hit (>= threshold) survives, so a
		// genuinely relevant page sharing a common word is not lost.
		if commonOnlyQuery && m.inBM25 && m.semCos < semSupportThreshold {
			continue
		}
		m.final = m.bm25Score
		if m.semCos > m.final {
			m.final = m.semCos
		}
		switch {
		case m.inBM25 && m.semCos >= semSupportThreshold:
			m.final += semAgreementBonus // lexical hit confirmed by semantic similarity
		case m.inBM25 && semAvailable:
			m.final *= bm25OnlyPenalty // lexical hit with weak/no semantic support
		}
		m.res.Score = m.final
		out = append(out, *m)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].final != out[b].final {
			return out[a].final > out[b].final
		}
		return out[a].res.Path < out[b].res.Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	results := make([]SearchResult, len(out))
	for i := range out {
		results[i] = out[i].res
	}
	return results
}

// SearchFiles returns wiki file paths matching a query.
func (s *Store) SearchFiles(ctx context.Context, query string, limit int) ([]string, error) {
	results, err := s.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(results))
	for i, r := range results {
		paths[i] = r.Path
	}
	return paths, nil
}

// scoreToNormalized converts a raw BM25 score to a 0-1 range.
func scoreToNormalized(score float64) float64 {
	if score <= 0 {
		return 0
	}
	// Sigmoid normalization: maps [0, +inf) to (0, 1).
	return score / (score + 1)
}
