// deep_research.go — Multi-step research tool for complex multi-constraint questions.
//
// Decomposes a question into sub-queries (via LLM or caller-supplied),
// searches all in parallel, fetches top results, and synthesizes findings.
// Designed for BrowseComp-style questions where single searches fail.
//
// Pipeline:
//  1. Decompose question → 3-8 focused sub-queries (LLM or provided)
//  2. Parallel search+fetch all sub-queries
//  3. Return structured results for the outer LLM to synthesize
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DeepResearchLLM is the interface for the LLM client used by deep_research
// to decompose questions into sub-queries. Kept minimal to avoid importing
// the full llm package.
type DeepResearchLLM interface {
	Complete(ctx context.Context, model, system, user string, maxTokens int) (string, error)
}

// DeepResearchTool returns the deep_research tool handler.
func DeepResearchTool(cache *FetchCache, localAI *LocalAIExtractor, llm DeepResearchLLM, defaultModel string) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Question      string   `json:"question"`
			Queries       []string `json:"queries"`
			MaxQueries    int      `json:"maxQueries"`
			FetchPerQuery int      `json:"fetchPerQuery"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return formatFetchError(webFetchErr{
				Code: "invalid_params", Message: err.Error(), Retryable: false,
			}), nil
		}
		if p.Question == "" {
			return formatFetchError(webFetchErr{
				Code: "missing_params", Message: "question is required", Retryable: false,
			}), nil
		}
		if p.MaxQueries <= 0 {
			p.MaxQueries = 5
		}
		if p.MaxQueries > 8 {
			p.MaxQueries = 8
		}
		if p.FetchPerQuery <= 0 {
			p.FetchPerQuery = 2
		}
		if p.FetchPerQuery > 3 {
			p.FetchPerQuery = 3
		}

		start := time.Now()

		// Step 1: Get sub-queries (from caller or LLM decomposition).
		queries := p.Queries
		decomposed := false
		if len(queries) == 0 && llm != nil {
			var err error
			queries, err = decomposeQuestion(ctx, llm, defaultModel, p.Question, p.MaxQueries)
			if err != nil {
				// Fallback: use the question itself as the only query.
				queries = []string{p.Question}
			} else {
				decomposed = true
			}
		}
		if len(queries) == 0 {
			queries = []string{p.Question}
		}
		if len(queries) > p.MaxQueries {
			queries = queries[:p.MaxQueries]
		}

		// Step 2: Parallel search+fetch all sub-queries.
		maxChars := 80000 // generous budget for research
		perQueryChars := maxChars / len(queries)

		type queryResult struct {
			query   string
			content string
			err     error
			ms      int64
		}
		results := make([]queryResult, len(queries))
		var wg sync.WaitGroup
		for i, q := range queries {
			wg.Add(1)
			go func(idx int, query string) {
				defer wg.Done()
				qStart := time.Now()
				content, err := webSearchAndFetch(ctx, cache, localAI, query, 5, p.FetchPerQuery, perQueryChars)
				results[idx] = queryResult{
					query:   query,
					content: content,
					err:     err,
					ms:      time.Since(qStart).Milliseconds(),
				}
			}(i, q)
		}
		wg.Wait()

		// Step 3: Format structured output.
		var sb strings.Builder
		fmt.Fprintf(&sb, "<deep_research question=\"%s\">\n", truncateForAttr(p.Question, 200))
		if decomposed {
			sb.WriteString("<decomposition>\n")
			for i, q := range queries {
				fmt.Fprintf(&sb, "  %d. %s\n", i+1, q)
			}
			sb.WriteString("</decomposition>\n\n")
		}

		succeeded := 0
		for i, r := range results {
			fmt.Fprintf(&sb, "<research_result index=\"%d\" query=\"%s\" ms=\"%d\">\n", i+1, truncateForAttr(r.query, 150), r.ms)
			if r.err != nil {
				fmt.Fprintf(&sb, "Search failed: %s\n", r.err.Error())
			} else {
				sb.WriteString(r.content)
				succeeded++
			}
			sb.WriteString("\n</research_result>\n\n")
		}

		totalMs := time.Since(start).Milliseconds()
		fmt.Fprintf(&sb, "<summary queries=\"%d\" succeeded=\"%d\" total_ms=\"%d\" />\n", len(queries), succeeded, totalMs)
		sb.WriteString("</deep_research>")

		return sb.String(), nil
	}
}

// decomposeQuestion uses an LLM to break a complex question into focused sub-queries.
func decomposeQuestion(ctx context.Context, llm DeepResearchLLM, model, question string, maxQueries int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	system := `You decompose complex research questions into focused web search queries.
Rules:
- Output ONLY the queries, one per line, no numbering, no explanation
- Use English keywords optimized for web search (even if the question is in another language)
- Each query should target a different constraint or aspect of the question
- Be specific: include years, names, technical terms
- 3-8 queries depending on complexity`

	user := fmt.Sprintf("Decompose into %d search queries:\n\n%s", maxQueries, question)

	result, err := llm.Complete(ctx, model, system, user, 500)
	if err != nil {
		return nil, err
	}

	var queries []string
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		// Strip leading numbering (1. or 1) or - or *)
		line = strings.TrimLeft(line, "0123456789.-)*) ")
		line = strings.TrimSpace(line)
		if line != "" && len(line) > 5 {
			queries = append(queries, line)
		}
	}
	return queries, nil
}

// truncateForAttr truncates a string for safe use in XML attributes.
func truncateForAttr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\"", "'")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
