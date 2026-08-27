// Package textsearch provides an in-memory full-text search index.
//
// Designed to replace SQLite FTS5 for small-to-medium document sets
// (single-user deployment). Uses stdlib only — zero external dependencies.
//
// Features:
//   - Unicode-aware tokenization (handles Hangul, Latin, CJK)
//   - BM25-based relevance scoring
//   - Snippet extraction with match highlighting
//   - AND/OR query modes with automatic fallback
//   - Hangul index-time key expansion (particle/substring) so Korean queries
//     hit posting lists instead of scanning the vocabulary
package textsearch

import (
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// maxORCandidates bounds how many documents OR-mode will score. Rarest query
// tokens fill the set first so BM25-dominant hits stay, while a flood of
// common Hangul postings cannot drag a search into multi-second territory.
const maxORCandidates = 512

// Index is a thread-safe in-memory full-text search index.
type Index struct {
	mu       sync.RWMutex
	docs     map[string]*document           // docID -> document
	inverted map[string]map[string]struct{} // token/expansion key -> set of docIDs
	totalLen int                            // sum of all document lengths (for BM25 avgdl)
	lengthB  float64                        // BM25 length-normalization strength (b)
	// lengthBSet distinguishes "caller asked for b=0" (no normalization, a
	// legitimate setting) from a zero-value Index that predates this field.
	lengthBSet bool
}

type document struct {
	id     string
	fields []string // original text fields for snippet generation
	// weights holds one BM25F-style boost per field (nil = every field 1.0, the
	// plain-Upsert path). A weight scales the field's term-frequency contribution
	// only — document length/avgdl stay raw token counts so weighted and
	// unweighted documents share one normalization basis.
	weights []float64
	// snippetSrc, when non-nil, is the subset of fields eligible for snippet
	// extraction (Hidden fields removed). nil = all fields visible (the plain
	// path and hidden-free UpsertFields calls pay nothing).
	snippetSrc []string
	tokens     int // total token count
	// Cached tokenization so search never re-tokenizes bodies under the read
	// lock. tokList is the flat (unweighted) path; fieldToks is per-field for
	// UpsertFields. indexKeys lists every inverted key this doc posted under
	// (surface tokens + Hangul expansions) so Remove is exact.
	tokList   []string
	fieldToks [][]string
	indexKeys []string
}

// termStats is the per-query-token posting + IDF, computed once before the
// candidate scoring loop (calling matchingDocs inside that loop was O(C·Q·V)).
type termStats struct {
	token string
	docs  map[string]struct{}
	idf   float64
	df    int
}

// Field is one searchable text field with a term-frequency boost for
// UpsertFields. Weight 1.0 behaves exactly like plain Upsert; >1.0 makes a
// match in this field count proportionally more toward BM25 relevance
// (identity fields like titles/summaries vs. body prose).
type Field struct {
	Text   string
	Weight float64
	// Hidden indexes the field for matching/scoring but excludes it from
	// snippet extraction — for retrieval-only anchors (e.g. wiki cue
	// paraphrases) that must never surface as "match" text.
	Hidden bool
}

// Hit is a single search result.
type Hit struct {
	ID      string  // document ID
	Score   float64 // relevance score (higher is better)
	Snippet string  // text excerpt with match context
}

// LocateSnippet returns a source-addressable line window around the strongest
// lexical match. Line numbers are 1-based and inclusive. When no query token
// matches, it returns the first non-empty window so callers still have a stable
// address instead of the historical line 0 sentinel.
func LocateSnippet(text, query string, maxLines int) (snippet string, startLine, endLine int) {
	if maxLines <= 0 {
		maxLines = 5
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return "", 0, 0
	}
	queryTokens := tokenize(query)
	best, bestScore := -1, 0
	for i, line := range lines {
		lineTokens := tokenize(line)
		score := 0
		for _, token := range queryTokens {
			if matchedTermFrequency(lineTokens, token) > 0 {
				score++
			}
			if strings.Contains(strings.ToLower(line), token) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		for i, line := range lines {
			if strings.TrimSpace(line) != "" {
				best = i
				break
			}
		}
	}
	if best < 0 {
		return "", 0, 0
	}
	start := best
	if start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	end := min(len(lines), start+maxLines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), start + 1, end
}

// New creates an empty search index.
// DefaultLengthNorm is BM25's standard b. It assumes a long document is long
// because it is padded, so length is evidence against relevance.
const DefaultLengthNorm = 0.75

func New() *Index {
	return NewWithLengthNorm(DefaultLengthNorm)
}

// NewWithLengthNorm builds an index with an explicit BM25 length-normalization
// strength (b). Lower it for a corpus where document length tracks the KIND of
// document rather than its padding — a chat transcript is the clear case: an
// assistant answer is long because answering takes words, and penalizing it for
// that ranks the answer below the one-line question that provoked it.
//
// Measured on LongMemEval_s: the evidence message for an assistant-authored
// answer runs 2.0x the corpus median length (147 vs 73 words), which at b=0.75
// costs it ~43% of its score for length alone.
func NewWithLengthNorm(b float64) *Index {
	if b < 0 {
		b = 0
	}
	if b > 1 {
		b = 1
	}
	return &Index{
		docs:       make(map[string]*document),
		inverted:   make(map[string]map[string]struct{}),
		lengthB:    b,
		lengthBSet: true,
	}
}

// Upsert adds or replaces a document in the index.
// fields are the searchable text fields (e.g., title, content).
func (idx *Index) Upsert(id string, fields ...string) {
	idx.upsert(id, fields, nil, nil)
}

// UpsertFields adds or replaces a document whose fields carry per-field
// term-frequency boosts (BM25F-lite). Membership in the inverted index is
// weight-independent — a token in any field makes the document a candidate;
// weights change only how strongly the match scores.
func (idx *Index) UpsertFields(id string, fields ...Field) {
	texts := make([]string, len(fields))
	weights := make([]float64, len(fields))
	flat := true
	var snippetSrc []string
	anyHidden := false
	for _, f := range fields {
		if f.Hidden {
			anyHidden = true
			break
		}
	}
	if anyHidden {
		snippetSrc = make([]string, 0, len(fields))
		for _, f := range fields {
			if !f.Hidden {
				snippetSrc = append(snippetSrc, f.Text)
			}
		}
	}
	for i, f := range fields {
		texts[i] = f.Text
		w := f.Weight
		// NaN/Inf must not reach scoring: a NaN score breaks sort.Slice's strict
		// ordering (NaN comparisons are all false) and Inf swamps every rank.
		if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			w = 1
		}
		if w != 1 {
			flat = false
		}
		weights[i] = w
	}
	if flat {
		weights = nil // all-1.0 collapses to the plain path
	}
	idx.upsert(id, texts, weights, snippetSrc)
}

func (idx *Index) upsert(id string, fields []string, weights []float64, snippetSrc []string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old entry if updating.
	if old, ok := idx.docs[id]; ok {
		idx.removeDoc(old)
	}

	doc := &document{id: id, fields: fields, weights: weights, snippetSrc: snippetSrc}
	keySet := make(map[string]struct{}, 16)
	addKeys := func(toks []string) {
		for _, tok := range toks {
			for _, k := range expandIndexKeys(tok) {
				keySet[k] = struct{}{}
			}
		}
	}
	if weights == nil {
		doc.tokList = tokenize(strings.Join(fields, " "))
		doc.tokens = len(doc.tokList)
		addKeys(doc.tokList)
	} else {
		doc.fieldToks = make([][]string, len(fields))
		for i, f := range fields {
			doc.fieldToks[i] = tokenize(f)
			doc.tokens += len(doc.fieldToks[i])
			addKeys(doc.fieldToks[i])
		}
	}
	doc.indexKeys = make([]string, 0, len(keySet))
	for k := range keySet {
		doc.indexKeys = append(doc.indexKeys, k)
	}

	idx.docs[id] = doc
	idx.totalLen += doc.tokens

	for _, k := range doc.indexKeys {
		if idx.inverted[k] == nil {
			idx.inverted[k] = make(map[string]struct{})
		}
		idx.inverted[k][id] = struct{}{}
	}
}

// Remove deletes a document from the index.
func (idx *Index) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if doc, ok := idx.docs[id]; ok {
		idx.removeDoc(doc)
		delete(idx.docs, id)
	}
}

func (idx *Index) removeDoc(doc *document) {
	idx.totalLen -= doc.tokens
	for _, k := range doc.indexKeys {
		if set, ok := idx.inverted[k]; ok {
			delete(set, doc.id)
			if len(set) == 0 {
				delete(idx.inverted, k)
			}
		}
	}
}

// Clear removes all documents from the index.
func (idx *Index) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.docs = make(map[string]*document)
	idx.inverted = make(map[string]map[string]struct{})
	idx.totalLen = 0
}

// Len returns the number of indexed documents.
func (idx *Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.docs)
}

// Search performs a full-text search. Tries AND first, falls back to OR.
// Returns up to limit results sorted by relevance.
func (idx *Index) Search(query string, limit int) []Hit {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	// Try AND first for precision.
	hits := idx.search(queryTokens, true, limit)
	if len(hits) == 0 {
		// Fall back to OR for recall.
		hits = idx.search(queryTokens, false, limit)
	}
	return hits
}

// SearchOR performs an OR-only search (any token matches).
func (idx *Index) SearchOR(query string, limit int) []Hit {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}
	return idx.search(queryTokens, false, limit)
}

func (idx *Index) search(queryTokens []string, andMode bool, limit int) []Hit {
	if len(idx.docs) == 0 {
		return nil
	}

	n := float64(len(idx.docs))
	terms := make([]termStats, 0, len(queryTokens))
	for _, qt := range queryTokens {
		docs := idx.matchingDocs(qt)
		df := len(docs)
		if df == 0 {
			if andMode {
				return nil
			}
			continue
		}
		idf := math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
		terms = append(terms, termStats{token: qt, docs: docs, idf: idf, df: df})
	}
	if len(terms) == 0 {
		return nil
	}

	candidates := collectCandidates(terms, andMode)
	if len(candidates) == 0 {
		return nil
	}

	avgdl := float64(idx.totalLen) / float64(len(idx.docs))
	// A literal Index{} predates this field and expects the standard b.
	b := DefaultLengthNorm
	if idx.lengthBSet {
		b = idx.lengthB
	}

	type scored struct {
		id    string
		score float64
	}
	results := make([]scored, 0, len(candidates))

	for docID := range candidates {
		doc := idx.docs[docID]
		if doc == nil {
			continue
		}

		dl := float64(doc.tokens)
		var score float64
		for _, t := range terms {
			var termTF float64
			if doc.weights == nil {
				termTF = float64(matchedTermFrequency(doc.tokList, t.token))
			} else {
				for i, toks := range doc.fieldToks {
					if c := matchedTermFrequency(toks, t.token); c > 0 {
						termTF += float64(c) * doc.weights[i]
					}
				}
			}
			if termTF == 0 {
				continue
			}
			tfScore := (termTF * 2.2) / (termTF + 1.2*(1-b+b*(dl/avgdl)))
			score += t.idf * tfScore
		}

		if score > 0 {
			results = append(results, scored{id: docID, score: score})
		}
	}

	// Deterministic order: score desc, ID asc on ties. Candidates come from map
	// iteration, so without the tie-break equal-score documents would shuffle
	// between runs (measured as test flakiness; the same shuffle would hit
	// production ranking across restarts).
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].id < results[j].id
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	hits := make([]Hit, len(results))
	for i, r := range results {
		doc := idx.docs[r.id]
		src := doc.fields
		if doc.snippetSrc != nil {
			src = doc.snippetSrc
		}
		hits[i] = Hit{
			ID:      r.id,
			Score:   r.score,
			Snippet: extractSnippet(src, queryTokens, 40),
		}
	}
	return hits
}

// collectCandidates builds the doc set to score. AND intersects postings
// (rarest-first for smaller intermediate sets). OR unions rarest-first and
// stops at maxORCandidates so common Hangul tokens cannot flood scoring.
func collectCandidates(terms []termStats, andMode bool) map[string]struct{} {
	ordered := append([]termStats(nil), terms...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].df != ordered[j].df {
			return ordered[i].df < ordered[j].df
		}
		return ordered[i].token < ordered[j].token
	})

	if andMode {
		result := make(map[string]struct{}, len(ordered[0].docs))
		for id := range ordered[0].docs {
			result[id] = struct{}{}
		}
		for _, t := range ordered[1:] {
			for id := range result {
				if _, ok := t.docs[id]; !ok {
					delete(result, id)
				}
			}
			if len(result) == 0 {
				return nil
			}
		}
		return result
	}

	result := make(map[string]struct{}, min(maxORCandidates, len(ordered[0].docs)))
	for _, t := range ordered {
		for id := range t.docs {
			result[id] = struct{}{}
			if len(result) >= maxORCandidates {
				return result
			}
		}
	}
	return result
}

// DocFreq returns the number of indexed documents a query token matches,
// using the SAME matching semantics as scoring (exact for Latin; prefix,
// compound substring, and conservative particle handling for Hangul) so the
// value equals the `df` the BM25 IDF is computed from. Zero
// means the token is absent. Token is lowercased to match the index. Callers
// use it to gauge a term's corpus rarity (high df == common-in-corpus == a weak
// recall anchor); see NormalizedRarity.
func (idx *Index) DocFreq(token string) int {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return 0
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.matchingDocs(token))
}

// normalizedRarityLocked computes a token's rarity under a held read lock so the
// (df, N) pair is read atomically. Caller must hold idx.mu.RLock.
func (idx *Index) normalizedRarityLocked(token string) float64 {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return 0
	}
	df := len(idx.matchingDocs(token))
	if df <= 0 {
		return 0
	}
	n := float64(len(idx.docs))
	if n <= 1 {
		return 0
	}
	idfOf := func(d float64) float64 {
		return math.Log(1 + (n-d+0.5)/(d+0.5))
	}
	base := idfOf(1) // IDF of the rarest possible term in this corpus
	if base <= 0 {
		return 0
	}
	r := idfOf(float64(df)) / base
	if r < 0 {
		return 0
	}
	if r > 1 {
		r = 1
	}
	return r
}

// NormalizedRarity maps a query token to a corpus-size-invariant rarity in
// [0,1]: 1.0 == as rare as the rarest possible term (df==1), → 0 == appears in
// nearly every document. It is IDF(df)/IDF(df==1), which cancels the raw IDF's
// strong dependence on N (a fixed raw-IDF or normalized-BM25 threshold drifts
// with corpus size; this ratio holds: df==1 is 1.0 and a noun in ~15% of pages
// is ~0.3-0.5 at any realistic N). Returns 0 for an absent token (df==0) and for
// a degenerate single-doc corpus. This is the discriminator between a rare
// proper noun (거래처명, 고유명사 — a strong single-term anchor) and a noun that
// is merely common in the corpus (a weak anchor that lexically matches many
// off-topic pages).
func (idx *Index) NormalizedRarity(token string) float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.normalizedRarityLocked(token)
}

// QueryMaxRarity tokenizes a query the same way Search does and returns the
// highest NormalizedRarity among its tokens that are present in the corpus
// (df>0). It answers "does this query contain ANY rare anchor term?": a high
// value means at least one token is specific (a proper noun / distinctive term),
// so a lexical hit is trustworthy; a low value means every matchable token is
// corpus-common, so a BM25-only hit is a likely common-word false positive (the
// measured recall leak). Tokens absent from the corpus (df==0) contribute
// nothing — they cannot have produced any hit. Returns 0 for an empty query or
// one whose tokens are all absent. A single read lock spans all tokens so the
// corpus snapshot (matched df + N) is consistent across the whole query.
func (idx *Index) QueryMaxRarity(query string) float64 {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return 0
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	maxR := 0.0
	for _, tok := range tokens {
		if r := idx.normalizedRarityLocked(tok); r > maxR {
			maxR = r
		}
	}
	return maxR
}

// matchingDocs returns all document IDs matching a token. Latin and numeric
// tokens stay exact. Hangul resolves via index-time expansion keys (exact,
// particle-stripped base, substrings ≥ 2 runes) plus a query-side particle
// trim so "대한전선은" hits the posting for "대한전선".
func (idx *Index) matchingDocs(token string) map[string]struct{} {
	if !containsHangul(token) {
		return idx.inverted[token]
	}

	merged := make(map[string]struct{})
	add := func(key string) {
		if key == "" {
			return
		}
		for id := range idx.inverted[key] {
			merged[id] = struct{}{}
		}
	}
	add(token)
	if base := trimHangulParticle(token); base != token {
		add(base)
	}
	// One-syllable Hangul prefix ("레" → "레시피") is not expansion-keyed
	// (would flood). Fall back to a bounded prefix scan over surface-length
	// keys only when the query itself is a single Hangul syllable.
	if len([]rune(token)) == 1 && len(merged) == 0 {
		for indexToken, set := range idx.inverted {
			if !containsHangul(indexToken) {
				continue
			}
			// Expansion keys are themselves substrings; only consider keys that
			// could be surface tokens (prefix match against the query).
			if strings.HasPrefix(indexToken, token) {
				for id := range set {
					merged[id] = struct{}{}
				}
			}
		}
	}
	return merged
}

// tokenize splits text into lowercase tokens.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if isTokenChar(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func isTokenChar(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	return false
}

func termFrequencies(tokens []string) map[string]int {
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	return tf
}

func matchedTermFrequency(tokens []string, queryToken string) int {
	if !containsHangul(queryToken) {
		return termFrequencies(tokens)[queryToken]
	}
	count := 0
	for _, tok := range tokens {
		if hangulTokenMatches(tok, queryToken) {
			count++
		}
	}
	return count
}

// extractSnippet finds the best matching window in the document fields
// and returns a snippet of approximately windowTokens tokens.
func extractSnippet(fields []string, queryTokens []string, windowTokens int) string {
	text := strings.Join(fields, " ")
	if len(text) == 0 {
		return ""
	}

	runes := []rune(text)
	lower := []rune(strings.ToLower(text))
	windowChars := windowTokens * 5 // approximate chars per token

	// Find the rune position of the first query token match.
	bestPos := -1
	for _, qt := range queryTokens {
		qtRunes := []rune(qt)
		pos := runeIndex(lower, qtRunes)
		if pos >= 0 && (bestPos < 0 || pos < bestPos) {
			bestPos = pos
		}
	}

	if bestPos < 0 {
		// No exact substring match; return the beginning.
		if len(runes) > windowChars {
			return string(runes[:windowChars]) + "..."
		}
		return text
	}

	// Expand window around the match.
	start := bestPos - windowChars/2
	if start < 0 {
		start = 0
	}
	end := start + windowChars
	if end > len(runes) {
		end = len(runes)
	}

	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet = snippet + "..."
	}
	return snippet
}

// runeIndex returns the index of the first occurrence of needle in haystack,
// operating on rune slices (not byte offsets).
func runeIndex(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
