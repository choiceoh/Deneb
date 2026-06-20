// lexscore.go — a small, self-contained BM25 lexical scorer over the text the
// semantic index already holds (each file's chunk snippets + its name).
//
// Why a local scorer instead of reusing pkg/textsearch: the hybrid search must
// score the SAME corpus the vector scan ranks (the indexed files' chunk text),
// computed inline during one HybridSearch pass over the in-memory index. Pulling
// in textsearch.Index (or the wiki searchDB) would mean maintaining a second,
// separately-keyed FTS index in lockstep with the embedding index — more moving
// parts and a cross-package coupling for what is ~80 lines of stdlib BM25. Per
// the layering note in the task, a tiny scorer in this package keeps filestore's
// dependency surface unchanged (it stays off the textsearch import path) while
// reusing the chunks already in memory. The tokenizer mirrors pkg/textsearch's
// Unicode-aware, Hangul-prefix-matching behavior so Korean lexical recall lines
// up with the rest of the system.
package filestore

import (
	"math"
	"strings"
	"unicode"
)

// BM25 tuning constants — the standard defaults (also what pkg/textsearch uses).
// k1 controls term-frequency saturation; b controls length normalization.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// lexDoc is one file's tokenized lexical content for BM25 scoring: the tokens of
// its name plus its indexed chunk snippets, and a set of the name's tokens for
// the exact-name-match signal (the strongest "this file IS what you asked for"
// lexical evidence).
type lexDoc struct {
	path     string
	tf       map[string]int  // term frequency over all tokens (name + chunk text)
	nameToks map[string]bool // tokens drawn from the file name only
	length   int             // total token count, the BM25 document length
}

// lexCorpus is the BM25 statistics over all indexed files for one search.
type lexCorpus struct {
	docs  []*lexDoc
	df    map[string]int // document frequency: how many docs contain each token
	avgdl float64        // average document length
	n     int            // number of documents
}

// buildLexCorpus tokenizes every entry's (name + chunk snippets) into a BM25
// corpus. Called once per HybridSearch over the same snapshot the vector scan
// uses, so lexical and semantic scores cover an identical file set.
func buildLexCorpus(entries []*fileEntry) *lexCorpus {
	c := &lexCorpus{
		docs: make([]*lexDoc, 0, len(entries)),
		df:   make(map[string]int),
	}
	var totalLen int
	for _, fe := range entries {
		if fe == nil {
			continue
		}
		nameToks := lexTokenize(pathBase(fe.Path))
		// The file name is the most direct lexical handle on a file, so fold its
		// tokens into the scored text (and remember them for the exact-name signal).
		var b strings.Builder
		b.WriteString(strings.Join(nameToks, " "))
		for i := range fe.Chunks {
			b.WriteByte(' ')
			b.WriteString(fe.Chunks[i].Snippet)
		}
		toks := lexTokenize(b.String())
		d := &lexDoc{
			path:     fe.Path,
			tf:       termFreq(toks),
			nameToks: toSet(nameToks),
			length:   len(toks),
		}
		c.docs = append(c.docs, d)
		totalLen += d.length
		// Document frequency counts each token once per document.
		seen := make(map[string]bool, len(d.tf))
		for tok := range d.tf {
			if !seen[tok] {
				c.df[tok]++
				seen[tok] = true
			}
		}
	}
	c.n = len(c.docs)
	if c.n > 0 {
		c.avgdl = float64(totalLen) / float64(c.n)
	}
	return c
}

// lexResult is one document's lexical evaluation for the query.
type lexResult struct {
	score   float64 // BM25 score (>= 0; 0 = no query token matched)
	nameHit bool    // a query token exactly matched a token of the file NAME
	matched int     // number of distinct query tokens that matched this doc
}

// score evaluates every document against the query tokens, returning a
// path→lexResult map. A query token matches a doc token by exact equality, or —
// for a Hangul query token — by prefix (mirroring pkg/textsearch's Korean
// matching, which approximates stemming for an agglutinative script that
// suffixes particles like 을/를/이/가 onto nouns).
func (c *lexCorpus) score(queryTokens []string) map[string]lexResult {
	out := make(map[string]lexResult, len(c.docs))
	// avgdl == 0 only if every doc tokenized to nothing (no name, no text) — not
	// reachable for real files (a name always tokenizes), but guarding it keeps
	// the BM25 length term (dl/avgdl) from producing NaN on a degenerate corpus.
	if c.n == 0 || len(queryTokens) == 0 || c.avgdl == 0 {
		return out
	}
	// Precompute IDF per query token (BM25+ form: always positive).
	idf := make(map[string]float64, len(queryTokens))
	for _, qt := range queryTokens {
		df := c.docFreqMatching(qt)
		idf[qt] = math.Log(1 + (float64(c.n)-float64(df)+0.5)/(float64(df)+0.5))
	}
	for _, d := range c.docs {
		var score float64
		matched := 0
		nameHit := false
		for _, qt := range queryTokens {
			tf := d.matchTF(qt)
			if tf == 0 {
				continue
			}
			matched++
			if d.nameMatch(qt) {
				nameHit = true
			}
			dl := float64(d.length)
			denom := float64(tf) + bm25K1*(1-bm25B+bm25B*(dl/c.avgdl))
			score += idf[qt] * (float64(tf) * (bm25K1 + 1)) / denom
		}
		if score > 0 || nameHit {
			out[d.path] = lexResult{score: score, nameHit: nameHit, matched: matched}
		}
	}
	return out
}

// docFreqMatching returns how many documents contain a token (exact, or by
// Hangul prefix), so IDF reflects the same matching rule TF uses.
func (c *lexCorpus) docFreqMatching(qt string) int {
	if !containsHangulRune(qt) {
		return c.df[qt]
	}
	// Hangul: count docs with any token that has qt as a prefix. Walk docs once
	// (the corpus is single-user-small); a shared inverted index isn't worth the
	// extra structure for a per-query scan.
	count := 0
	for _, d := range c.docs {
		for tok := range d.tf {
			if strings.HasPrefix(tok, qt) {
				count++
				break
			}
		}
	}
	return count
}

// matchTF returns the term frequency of a query token in this doc: exact for
// non-Hangul, prefix-summed for Hangul.
func (d *lexDoc) matchTF(qt string) int {
	if !containsHangulRune(qt) {
		return d.tf[qt]
	}
	sum := 0
	for tok, n := range d.tf {
		if strings.HasPrefix(tok, qt) {
			sum += n
		}
	}
	return sum
}

// nameMatch reports whether a query token matches a token of the file name
// (exact, or Hangul-prefix) — the exact-name-match signal.
func (d *lexDoc) nameMatch(qt string) bool {
	if d.nameToks[qt] {
		return true
	}
	if containsHangulRune(qt) {
		for tok := range d.nameToks {
			if strings.HasPrefix(tok, qt) {
				return true
			}
		}
	}
	return false
}

// lexTokenize splits text into lowercase Unicode tokens (letters/digits),
// mirroring pkg/textsearch.tokenize so Korean/Latin/CJK split identically.
func lexTokenize(text string) []string {
	text = strings.ToLower(text)
	var toks []string
	var cur strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

func termFreq(toks []string) map[string]int {
	tf := make(map[string]int, len(toks))
	for _, t := range toks {
		tf[t]++
	}
	return tf
}

func toSet(toks []string) map[string]bool {
	s := make(map[string]bool, len(toks))
	for _, t := range toks {
		s[t] = true
	}
	return s
}

// containsHangulRune reports whether s contains a precomposed Hangul syllable,
// matching pkg/textsearch.containsHangul.
func containsHangulRune(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}
