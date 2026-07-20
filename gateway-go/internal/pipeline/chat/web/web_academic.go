// web_academic.go — free structured academic lane attached to web search.
//
// Wigolo review follow-up (2026-07-21): the useful part of "18 engines" was
// the vertical adapters, and those are just stable public JSON/Atom APIs. This
// lane queries arXiv and Semantic Scholar IN PARALLEL with the main Serper
// search and appends a labeled section AFTER the normal results — no rank
// fusion, so the main SERP ordering is never polluted (the concern that killed
// the RRF-merge design). Korean business queries never trigger it.
//
// Trigger is two-fold: the model can force it (`academic` param, schema-guided)
// or the query carries obvious academic intent (arXiv ID / arxiv / 논문 /
// paper / preprint / 학술). Both APIs are keyless and free; total lane failure
// degrades to "no section" without touching the main search result.
//
// This complements — not replaces — `type:"scholar"` (Serper's Google Scholar,
// key-gated): the lane rides the DEFAULT search path and returns structured
// abstracts, which Scholar snippets lack.
package web

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// academicLaneTimeout bounds the whole lane; it runs concurrently with the
	// main search, so the added wall-clock is max(0, lane − search). 6s covers
	// arXiv's occasional multi-second cold responses (live-measured 4s misses
	// at a 3.5s budget) — the lane only fires on academic-intent queries, where
	// structured paper hits are worth a few extra seconds.
	academicLaneTimeout = 6 * time.Second
	// academicPerSourceCap bounds hits per source (arXiv / S2).
	academicPerSourceCap = 4
	// academicAbstractRunes bounds each rendered abstract.
	academicAbstractRunes = 220
)

// academicHit is one normalized paper hit from either source.
type academicHit struct {
	Source    string // "arXiv" | "S2"
	Title     string
	Authors   []string
	Year      string
	URL       string
	ArxivID   string
	Abstract  string
	Citations int
}

// Seams (jinaFetchFn pattern): tests swap the per-source searchers.
var (
	arxivSearchFn           = arxivSearch
	semanticScholarSearchFn = semanticScholarSearch
)

// arxivIDRe matches modern arXiv identifiers ("2607.12161", optional version).
var arxivIDRe = regexp.MustCompile(`\b(\d{4}\.\d{4,5})(v\d+)?\b`)

// academicIntentTokens force the lane on when present in the query.
var academicIntentTokens = []string{"arxiv", "논문", "paper", "preprint", "학술", "semantic scholar"}

// academicQueryIntent reports whether the query obviously asks for papers.
func academicQueryIntent(query string) bool {
	lower := strings.ToLower(query)
	for _, tok := range academicIntentTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return arxivIDRe.MatchString(query)
}

// startAcademicLane launches the lane when forced (model param) or the query
// carries academic intent. Returns nil when the lane is off; otherwise a
// 1-buffered channel that always yields exactly one value ("" = no section).
func startAcademicLane(ctx context.Context, query string, forced bool) <-chan string {
	if !forced && !academicQueryIntent(query) {
		return nil
	}
	ch := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("academic lane panic", "panic", r)
				ch <- ""
			}
		}()
		laneCtx, cancel := context.WithTimeout(ctx, academicLaneTimeout)
		defer cancel()
		ch <- academicLane(laneCtx, query)
	}()
	return ch
}

// joinAcademicLane receives the lane result; nil channel means "lane off".
func joinAcademicLane(ch <-chan string) string {
	if ch == nil {
		return ""
	}
	return <-ch
}

// academicLane queries both sources in parallel and renders the labeled block.
func academicLane(ctx context.Context, query string) string {
	var (
		wg        sync.WaitGroup
		arxivHits []academicHit
		s2Hits    []academicHit
	)
	run := func(name string, out *[]academicHit, fn func(context.Context, string, int) ([]academicHit, error)) {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("academic source panic", "source", name, "panic", r)
			}
		}()
		hits, err := fn(ctx, query, academicPerSourceCap)
		if err != nil {
			slog.Debug("academic source failed", "source", name, "error", err)
			return
		}
		*out = hits
	}
	wg.Add(2)
	go run("arxiv", &arxivHits, arxivSearchFn)
	go run("s2", &s2Hits, semanticScholarSearchFn)
	wg.Wait()

	// Dedupe: an S2 hit that IS an arXiv paper already listed adds nothing.
	seen := map[string]bool{}
	for _, h := range arxivHits {
		if h.ArxivID != "" {
			seen[h.ArxivID] = true
		}
	}
	merged := arxivHits
	for _, h := range s2Hits {
		if h.ArxivID != "" && seen[h.ArxivID] {
			continue
		}
		merged = append(merged, h)
	}
	if len(merged) == 0 {
		return ""
	}
	slog.Info("web academic lane attached",
		"arxivHits", len(arxivHits), "s2Hits", len(s2Hits), "merged", len(merged))

	var sb strings.Builder
	sb.WriteString("── 학술 레인 (arXiv · Semantic Scholar — 무료 구조화 검색) ──\n")
	for i, h := range merged {
		fmt.Fprintf(&sb, "%d. %s", i+1, h.Title)
		if a := renderAuthors(h.Authors); a != "" {
			sb.WriteString(" — " + a)
		}
		if h.Year != "" {
			sb.WriteString(" (" + h.Year + ")")
		}
		if h.ArxivID != "" {
			sb.WriteString(" | arXiv:" + h.ArxivID)
		}
		if h.Citations > 0 {
			fmt.Fprintf(&sb, " | 인용 %d", h.Citations)
		}
		if h.URL != "" {
			sb.WriteString(" | " + h.URL)
		}
		sb.WriteByte('\n')
		if abs := collapseWhitespace(h.Abstract); abs != "" {
			sb.WriteString("   초록: " + truncateRunes(abs, academicAbstractRunes) + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// renderAuthors shows up to two names ("A, B 외").
func renderAuthors(authors []string) string {
	switch {
	case len(authors) == 0:
		return ""
	case len(authors) <= 2:
		return strings.Join(authors, ", ")
	default:
		return strings.Join(authors[:2], ", ") + " 외"
	}
}

// academicAPIQuery strips trigger tokens (they describe intent, not topic) and
// returns the cleaned query plus the ASCII-only variant arXiv needs.
func academicAPIQuery(query string) (cleaned, asciiOnly string) {
	lower := strings.ToLower(query)
	for _, tok := range academicIntentTokens {
		lower = strings.ReplaceAll(lower, tok, " ")
	}
	var all, ascii []string
	for _, f := range strings.Fields(lower) {
		all = append(all, f)
		isASCII := true
		for _, r := range f {
			if r > unicode.MaxASCII {
				isASCII = false
				break
			}
		}
		if isASCII {
			ascii = append(ascii, f)
		}
	}
	return strings.Join(all, " "), strings.Join(ascii, " ")
}

// --- arXiv (Atom API, keyless) ---

type arxivFeed struct {
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID        string `xml:"id"` // http://arxiv.org/abs/2607.12161v1
	Title     string `xml:"title"`
	Summary   string `xml:"summary"`
	Published string `xml:"published"`
	Authors   []struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

// arxivSearch queries the arXiv Atom API: a direct id_list lookup when the
// query carries an arXiv ID, else an AND term search over ASCII terms (arXiv
// indexes no Korean — a fully non-ASCII query skips the source).
func arxivSearch(ctx context.Context, query string, limit int) ([]academicHit, error) {
	params := url.Values{"max_results": {fmt.Sprint(limit)}}
	if m := arxivIDRe.FindStringSubmatch(query); m != nil {
		params.Set("id_list", m[1]+m[2])
	} else {
		_, ascii := academicAPIQuery(query)
		terms := strings.Fields(ascii)
		if len(terms) == 0 {
			return nil, nil
		}
		if len(terms) > 6 {
			terms = terms[:6]
		}
		for i, t := range terms {
			terms[i] = "all:" + arxivQuoteTerm(t)
		}
		params.Set("search_query", strings.Join(terms, " AND "))
		params.Set("sortBy", "relevance")
	}
	body, err := academicGet(ctx, "https://export.arxiv.org/api/query?"+params.Encode())
	if err != nil {
		return nil, err
	}
	return parseArxivFeed(body)
}

// arxivQuoteTerm quotes a term for the arXiv query grammar.
func arxivQuoteTerm(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, "") + `"`
}

// parseArxivFeed converts the Atom payload to hits.
func parseArxivFeed(body []byte) ([]academicHit, error) {
	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("arxiv: parse feed: %w", err)
	}
	var hits []academicHit
	for _, e := range feed.Entries {
		id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(e.ID, "http://arxiv.org/abs/"), "https://arxiv.org/abs/"), "")
		if m := arxivIDRe.FindStringSubmatch(id); m != nil {
			id = m[1]
		}
		year := ""
		if len(e.Published) >= 4 {
			year = e.Published[:4]
		}
		var authors []string
		for _, a := range e.Authors {
			authors = append(authors, a.Name)
		}
		hits = append(hits, academicHit{
			Source:   "arXiv",
			Title:    collapseWhitespace(e.Title),
			Authors:  authors,
			Year:     year,
			URL:      "https://arxiv.org/abs/" + id,
			ArxivID:  id,
			Abstract: e.Summary,
		})
	}
	return hits, nil
}

// --- Semantic Scholar (Graph API, keyless free tier) ---

type s2Response struct {
	Data []struct {
		Title         string `json:"title"`
		Abstract      string `json:"abstract"`
		Year          int    `json:"year"`
		URL           string `json:"url"`
		CitationCount int    `json:"citationCount"`
		ExternalIds   struct {
			ArXiv string `json:"ArXiv"`
		} `json:"externalIds"`
		Authors []struct {
			Name string `json:"name"`
		} `json:"authors"`
	} `json:"data"`
}

// semanticScholarSearch queries the S2 Graph API (rate-limited shared pool —
// a 429 simply drops the source for this query).
func semanticScholarSearch(ctx context.Context, query string, limit int) ([]academicHit, error) {
	cleaned, _ := academicAPIQuery(query)
	if strings.TrimSpace(cleaned) == "" {
		return nil, nil
	}
	params := url.Values{
		"query":  {cleaned},
		"limit":  {fmt.Sprint(limit)},
		"fields": {"title,abstract,year,url,citationCount,externalIds,authors"},
	}
	body, err := academicGet(ctx, "https://api.semanticscholar.org/graph/v1/paper/search?"+params.Encode())
	if err != nil {
		return nil, err
	}
	return parseS2Response(body)
}

// parseS2Response converts the Graph API payload to hits.
func parseS2Response(body []byte) ([]academicHit, error) {
	var resp s2Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("semantic scholar: parse: %w", err)
	}
	var hits []academicHit
	for _, d := range resp.Data {
		var authors []string
		for _, a := range d.Authors {
			authors = append(authors, a.Name)
		}
		year := ""
		if d.Year > 0 {
			year = fmt.Sprint(d.Year)
		}
		hits = append(hits, academicHit{
			Source:    "S2",
			Title:     collapseWhitespace(d.Title),
			Authors:   authors,
			Year:      year,
			URL:       d.URL,
			ArxivID:   d.ExternalIds.ArXiv,
			Abstract:  d.Abstract,
			Citations: d.CitationCount,
		})
	}
	return hits, nil
}

// academicGet fetches a keyless academic API over the SSRF-safe shared client.
func academicGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/atom+xml, */*")
	resp, err := SharedClient(6 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// collapseWhitespace flattens Atom/JSON multi-line fields to one line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncateRunes caps s at n runes with an ellipsis.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
