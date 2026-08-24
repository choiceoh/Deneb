// tool_grep_rerank.go — relevance-aware selection for overflowing grep output
// (RARG level-3 adoption, arXiv 2607.24223, 2026-08-25).
//
// GrepResultSummarizer used to keep the FIRST grepMaxMatches lines and drop
// the rest — a positional cut. RARG's finding is that when grep output exceeds
// the observation limit, the informative excerpts are frequently in the
// dropped tail: every retained line matched the PATTERN, so position carries
// no information about which matches serve the QUESTION. Their fix reranks
// the overflowing match pool against the global query; ported here as a
// deterministic lexical scorer over the turn's user message (the RARG paper
// uses an embedder — a lexical overlap scorer is the zero-latency,
// zero-dependency first slice, and the mechanism only runs on overflow, so
// the happy path pays nothing).
//
// Selection, not reordering: the kept lines stay in their original order so
// file grouping and path:line context read naturally — relevance decides only
// WHICH lines survive the cap. Ties (and a query with no scoreable tokens)
// degrade to exactly the old positional behavior.
package chat

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// grepQueryTokenRe extracts scoreable tokens from the turn's user message:
// Korean runs and latin/digit identifiers, length ≥ 2.
var grepQueryTokenRe = regexp.MustCompile(`[가-힣]{2,}|[A-Za-z_][A-Za-z0-9_]{2,}`)

// grepQueryStop are message tokens too common to discriminate between match
// lines — conversational filler and the verbs every request carries.
var grepQueryStop = map[string]struct{}{
	"그리고": {}, "해줘": {}, "확인": {}, "정리": {}, "다시": {}, "부분": {},
	"관련": {}, "어디": {}, "찾아": {}, "the": {}, "and": {}, "for": {}, "with": {},
}

// grepRerankQueryTokens tokenizes the turn query for match scoring.
func grepRerankQueryTokens(query string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, tok := range grepQueryTokenRe.FindAllString(query, -1) {
		lower := strings.ToLower(tok)
		if _, stop := grepQueryStop[lower]; stop {
			continue
		}
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, lower)
	}
	return out
}

// grepSelectRelevant returns the indices (ascending) of the lines to keep:
// the cap best-scoring lines by distinct query-token overlap, positional
// order breaking ties. With no scoring signal it returns nil, telling the
// caller to keep the plain positional head.
func grepSelectRelevant(lines []string, queryTokens []string, limit int) []int {
	if len(queryTokens) == 0 || len(lines) <= limit {
		return nil
	}
	type scored struct{ idx, score int }
	entries := make([]scored, len(lines))
	signal := false
	for i, line := range lines {
		lower := strings.ToLower(line)
		score := 0
		for _, tok := range queryTokens {
			if strings.Contains(lower, tok) {
				score++
			}
		}
		if score > 0 {
			signal = true
		}
		entries[i] = scored{idx: i, score: score}
	}
	if !signal {
		return nil
	}
	sort.SliceStable(entries, func(a, b int) bool {
		if entries[a].score != entries[b].score {
			return entries[a].score > entries[b].score
		}
		return entries[a].idx < entries[b].idx
	})
	kept := entries[:limit]
	idx := make([]int, len(kept))
	for i, e := range kept {
		idx[i] = e.idx
	}
	sort.Ints(idx)
	return idx
}

// GrepResultRelevanceSummarizer caps grep output like GrepResultSummarizer,
// but on overflow it keeps the query-relevant matches instead of the first N.
func GrepResultRelevanceSummarizer(ctx context.Context, _ string, output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= grepMaxMatches {
		return output
	}
	idx := grepSelectRelevant(lines, grepRerankQueryTokens(toolport.TurnQueryFromContext(ctx)), grepMaxMatches)
	if idx == nil {
		// No query signal — the old positional cut, verbatim.
		kept := strings.Join(lines[:grepMaxMatches], "\n")
		return fmt.Sprintf("%s\n\n[... %d more matches omitted (total: %d lines)]", kept, len(lines)-grepMaxMatches, len(lines))
	}
	kept := make([]string, len(idx))
	for i, j := range idx {
		kept[i] = lines[j]
	}
	return fmt.Sprintf("%s\n\n[... %d more matches omitted (total: %d lines; kept the %d most relevant to the current request, original order)]",
		strings.Join(kept, "\n"), len(lines)-len(idx), len(lines), len(idx))
}
