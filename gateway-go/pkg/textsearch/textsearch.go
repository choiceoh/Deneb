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
package textsearch

import (
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Index is a thread-safe in-memory full-text search index.
type Index struct {
	mu       sync.RWMutex
	docs     map[string]*document           // docID -> document
	inverted map[string]map[string]struct{} // token -> set of docIDs
	totalLen int                            // sum of all document lengths (for BM25 avgdl)
}

type document struct {
	id     string
	fields []string // original text fields for snippet generation
	tokens int      // total token count
}

// Hit is a single search result.
type Hit struct {
	ID      string  // document ID
	Score   float64 // relevance score (higher is better)
	Snippet string  // text excerpt with match context
}

// New creates an empty search index.
func New() *Index {
	return &Index{
		docs:     make(map[string]*document),
		inverted: make(map[string]map[string]struct{}),
	}
}

// Upsert adds or replaces a document in the index.
// fields are the searchable text fields (e.g., title, content).
func (idx *Index) Upsert(id string, fields ...string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old entry if updating.
	if old, ok := idx.docs[id]; ok {
		idx.removeDoc(old)
	}

	tokens := tokenize(strings.Join(fields, " "))
	doc := &document{id: id, fields: fields, tokens: len(tokens)}
	idx.docs[id] = doc
	idx.totalLen += doc.tokens

	for _, tok := range tokens {
		if idx.inverted[tok] == nil {
			idx.inverted[tok] = make(map[string]struct{})
		}
		idx.inverted[tok][id] = struct{}{}
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
	tokens := tokenize(strings.Join(doc.fields, " "))
	for _, tok := range tokens {
		if set, ok := idx.inverted[tok]; ok {
			delete(set, doc.id)
			if len(set) == 0 {
				delete(idx.inverted, tok)
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

	// Collect candidate documents.
	candidates := idx.collectCandidates(queryTokens, andMode)
	if len(candidates) == 0 {
		return nil
	}

	// Score each candidate using BM25.
	avgdl := float64(idx.totalLen) / float64(len(idx.docs))
	n := float64(len(idx.docs))

	type scored struct {
		id    string
		score float64
	}
	var results []scored

	for docID := range candidates {
		doc := idx.docs[docID]
		if doc == nil {
			continue
		}

		text := strings.Join(doc.fields, " ")
		docTokens := tokenize(text)
		dl := float64(doc.tokens)

		var score float64
		for _, qt := range queryTokens {
			matchIDs := idx.matchingDocs(qt)
			df := float64(len(matchIDs))
			if df == 0 {
				continue
			}
			// BM25+ IDF (always positive, even for very common terms)
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			// BM25 TF component (k1=1.2, b=0.75)
			termTF := float64(matchedTermFrequency(docTokens, qt))
			if termTF == 0 {
				continue
			}
			tfScore := (termTF * 2.2) / (termTF + 1.2*(1-0.75+0.75*(dl/avgdl)))
			score += idf * tfScore
		}

		if score > 0 {
			results = append(results, scored{id: docID, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	hits := make([]Hit, len(results))
	for i, r := range results {
		doc := idx.docs[r.id]
		hits[i] = Hit{
			ID:      r.id,
			Score:   r.score,
			Snippet: extractSnippet(doc.fields, queryTokens, 40),
		}
	}
	return hits
}

// collectCandidates finds document IDs matching the query tokens.
func (idx *Index) collectCandidates(queryTokens []string, andMode bool) map[string]struct{} {
	if andMode {
		// AND: intersection of all token posting lists.
		var result map[string]struct{}
		for _, qt := range queryTokens {
			matchIDs := idx.matchingDocs(qt)
			if result == nil {
				result = make(map[string]struct{}, len(matchIDs))
				for id := range matchIDs {
					result[id] = struct{}{}
				}
			} else {
				for id := range result {
					if _, ok := matchIDs[id]; !ok {
						delete(result, id)
					}
				}
			}
			if len(result) == 0 {
				return nil
			}
		}
		return result
	}

	// OR: union of all token posting lists.
	result := make(map[string]struct{})
	for _, qt := range queryTokens {
		for id := range idx.matchingDocs(qt) {
			result[id] = struct{}{}
		}
	}
	return result
}

// DocFreq returns the number of indexed documents a query token matches,
// using the SAME matching semantics as scoring (exact for Latin, Hangul-prefix
// for Hangul) so the value equals the `df` the BM25 IDF is computed from. Zero
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

// matchingDocs returns all document IDs matching a token.
// Supports Hangul prefix matching: if the token contains Hangul,
// also matches index entries that start with the token.
func (idx *Index) matchingDocs(token string) map[string]struct{} {
	// Exact match first.
	if set, ok := idx.inverted[token]; ok && !containsHangul(token) {
		return set
	}

	// For Hangul tokens or tokens not found exactly, try prefix matching.
	if containsHangul(token) {
		merged := make(map[string]struct{})
		for indexToken, set := range idx.inverted {
			if strings.HasPrefix(indexToken, token) {
				for id := range set {
					merged[id] = struct{}{}
				}
			}
		}
		return merged
	}

	return idx.inverted[token]
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

func containsHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
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
		if strings.HasPrefix(tok, queryToken) {
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
