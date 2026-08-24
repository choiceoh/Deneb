// web_fetch_rank.go — search+fetch candidate ranking, filters, and usable fill.
package web

import (
	"context"
	"log/slog"
	"net/url"
	"path"
	"strings"
	"sync"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

type searchFetchOutcome struct {
	content string
	err     error
}

type fetchedPage struct {
	url     string
	content string
}

// fetchUsability is the structured verdict for a fetch result (search+fetch).
type fetchUsability struct {
	Usable    bool
	Thin      bool
	HasError  bool
	BodyChars int
	Signals   []string
}

// fetchOutcome pairs the agent-facing envelope with a structured usability
// verdict produced on the fetch path (not by re-parsing Signals: text).
type fetchOutcome struct {
	Content string
	Assess  fetchUsability
}

type candidateFilterStats struct {
	DeniedHost  int
	DeniedYT    int
	DeniedPath  int
	DeniedMedia int
	DupHost     int
	DupETLD     int
	Kept        int
}

func fetchCandidatePoolSize(count, fetchTop int) int {
	pool := fetchTop + 2
	if pool > 5 {
		pool = 5
	}
	if count > 0 && pool > count {
		pool = count
	}
	if pool < fetchTop {
		pool = fetchTop
	}
	return pool
}

// rankFetchCandidates orders answer-box, then knowledge-graph website, then
// organic URLs scored by query overlap with diversity demotion.
func rankFetchCandidates(query, answerLink, knowledgeLink string, organic []searchResult, limit int) ([]string, candidateFilterStats) {
	ordered := make([]string, 0, 2+len(organic))
	if link := strings.TrimSpace(answerLink); link != "" {
		ordered = append(ordered, link)
	}
	if link := strings.TrimSpace(knowledgeLink); link != "" && link != strings.TrimSpace(answerLink) {
		ordered = append(ordered, link)
	}
	ordered = append(ordered, orderOrganicByRelevance(query, organic)...)
	return filterFetchCandidates(ordered, limit)
}

func orderOrganicByRelevance(query string, organic []searchResult) []string {
	if len(organic) == 0 {
		return nil
	}
	tokens := queryTokens(query)
	type scored struct {
		url   string
		score int
		idx   int
	}
	items := make([]scored, 0, len(organic))
	for i, r := range organic {
		items = append(items, scored{
			url:   r.URL,
			score: snippetQueryScore(tokens, r),
			idx:   i,
		})
	}
	for i := 0; i < len(items); i++ {
		best := i
		for j := i + 1; j < len(items); j++ {
			if items[j].score > items[best].score ||
				(items[j].score == items[best].score && items[j].idx < items[best].idx) {
				best = j
			}
		}
		items[i], items[best] = items[best], items[i]
	}

	out := make([]string, 0, len(items))
	seenETLD := map[string]int{}
	seenPath := map[string]int{}
	remaining := items
	for len(remaining) > 0 && len(out) < len(items) {
		best := -1
		bestAdj := -1 << 30
		for i, it := range remaining {
			adj := it.score
			if u, err := url.Parse(it.url); err == nil {
				etld := registrableDomain(u.Host)
				pp := pathPrefixKey(u)
				adj -= seenETLD[etld] * 3
				adj -= seenPath[pp] * 2
			}
			if adj > bestAdj || (adj == bestAdj && (best < 0 || it.idx < remaining[best].idx)) {
				bestAdj = adj
				best = i
			}
		}
		pick := remaining[best]
		out = append(out, pick.url)
		if u, err := url.Parse(pick.url); err == nil {
			seenETLD[registrableDomain(u.Host)]++
			seenPath[pathPrefixKey(u)]++
		}
		remaining = append(remaining[:best], remaining[best+1:]...)
	}
	return out
}

func queryTokens(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) >= 2 {
			out = append(out, f)
		}
	}
	return out
}

func snippetQueryScore(tokens []string, r searchResult) int {
	if len(tokens) == 0 {
		return 0
	}
	hay := strings.ToLower(r.Title + " " + r.Description + " " + r.URL)
	score := 0
	for _, t := range tokens {
		if strings.Contains(hay, t) {
			score++
		}
	}
	return score
}

func registrableDomain(host string) string {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return host
}

func pathPrefixKey(u *url.URL) string {
	seg := strings.Trim(u.Path, "/")
	if seg == "" {
		return registrableDomain(u.Host) + "/"
	}
	if i := strings.IndexByte(seg, '/'); i >= 0 {
		seg = seg[:i]
	}
	return registrableDomain(u.Host) + "/" + strings.ToLower(seg)
}

func filterFetchCandidates(urls []string, limit int) ([]string, candidateFilterStats) {
	var stats candidateFilterStats
	if limit <= 0 {
		return nil, stats
	}
	seenHost := make(map[string]struct{}, limit)
	seenETLD := make(map[string]struct{}, limit)
	out := make([]string, 0, limit)
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if lower := strings.ToLower(raw); strings.HasPrefix(lower, "javascript:") ||
			strings.HasPrefix(lower, "data:") ||
			strings.HasPrefix(lower, "vbscript:") {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		switch {
		case isSupportedSocialFetchURL(raw):
			// Reddit pages and X status URLs have native JSON read handlers
			// (web_reddit.go / web_x.go), so exempt them from the social host
			// denylist below — otherwise search+fetch could never reach them.
		case isNonDocumentMediaURL(parsed):
			stats.DeniedMedia++
			continue
		case isDeniedFetchHost(parsed.Host):
			stats.DeniedHost++
			continue
		case isDeniedYouTubeURL(parsed):
			stats.DeniedYT++
			continue
		case isLowQualityFetchPath(parsed):
			stats.DeniedPath++
			continue
		}
		host := strings.ToLower(parsed.Host)
		if _, ok := seenHost[host]; ok {
			stats.DupHost++
			continue
		}
		etld := registrableDomain(host)
		if _, ok := seenETLD[etld]; ok {
			stats.DupETLD++
			continue
		}
		seenHost[host] = struct{}{}
		seenETLD[etld] = struct{}{}
		out = append(out, raw)
		if len(out) >= limit {
			break
		}
	}
	stats.Kept = len(out)
	return out, stats
}

func selectFetchURLs(urls []string, limit int) []string {
	out, _ := filterFetchCandidates(urls, limit)
	return out
}

var fetchHostDenyExact = map[string]struct{}{
	"facebook.com": {}, "fb.com": {}, "instagram.com": {},
	"tiktok.com": {}, "x.com": {}, "twitter.com": {},
	"reddit.com": {}, "quora.com": {},
	"linkedin.com": {}, "threads.net": {}, "tumblr.com": {},
	"snapchat.com": {}, "vk.com": {},
}

var fetchHostDenySuffix = []string{
	".facebook.com", ".fb.com", ".instagram.com",
	".pinterest.com", ".pinterest.co.kr", ".tiktok.com",
	".x.com", ".twitter.com", ".reddit.com", ".quora.com",
	".linkedin.com", ".threads.net", ".tumblr.com",
	".snapchat.com", ".vk.com",
}

// isSupportedSocialFetchURL reports whether a URL has a native read handler
// (web_reddit.go / web_x.go) and so should be exempt from the social host
// denylist during search+fetch candidate selection.
func isSupportedSocialFetchURL(raw string) bool {
	if isRedditURL(raw) {
		return true
	}
	_, ok := isXStatusURL(raw)
	return ok
}

func isDeniedFetchHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	if _, ok := fetchHostDenyExact[host]; ok {
		return true
	}
	if strings.HasPrefix(host, "pinterest.") {
		return true
	}
	for _, suf := range fetchHostDenySuffix {
		if strings.HasSuffix(host, suf) {
			return true
		}
	}
	return false
}

func isDeniedYouTubeURL(u *url.URL) bool {
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
	default:
		return false
	}
	return !media.IsYouTubeURL(u.String())
}

var lowQualityFetchPathExact = map[string]struct{}{
	"/cart": {}, "/checkout": {}, "/login": {}, "/signin": {},
	"/signup": {}, "/register": {}, "/account": {}, "/account/login": {},
}

func isLowQualityFetchPath(u *url.URL) bool {
	p := strings.ToLower(path.Clean("/" + strings.TrimSpace(u.Path)))
	if _, ok := lowQualityFetchPathExact[p]; ok {
		return true
	}
	for prefix := range lowQualityFetchPathExact {
		if strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	if strings.Contains(p, "/search") && u.RawQuery != "" {
		return true
	}
	return false
}

var nonDocumentMediaExt = map[string]struct{}{
	".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {}, ".svg": {}, ".ico": {},
	".mp4": {}, ".webm": {}, ".mov": {}, ".avi": {}, ".mkv": {},
	".mp3": {}, ".wav": {}, ".m4a": {}, ".flac": {}, ".ogg": {},
}

func isNonDocumentMediaURL(u *url.URL) bool {
	ext := strings.ToLower(path.Ext(u.Path))
	_, ok := nonDocumentMediaExt[ext]
	return ok
}

const usableFetchMinChars = 400

var thinFetchSignals = []string{"js_required", "empty_body", "low_content_yield"}

// assessMetaBody builds a usability verdict from fetch-path metadata + body.
func assessMetaBody(signals []string, body string) fetchUsability {
	bodyChars := len(strings.TrimSpace(body))
	thin := false
	for _, want := range thinFetchSignals {
		for _, got := range signals {
			if got == want {
				thin = true
				break
			}
		}
		if thin {
			break
		}
	}
	u := fetchUsability{
		Thin:      thin,
		BodyChars: bodyChars,
		Signals:   append([]string(nil), signals...),
	}
	if thin {
		u.Usable = bodyChars >= usableFetchMinChars
		return u
	}
	u.Usable = bodyChars > 0
	return u
}

// assessFetchResult falls back to envelope parsing (cache hits / legacy tests).
func assessFetchResult(content string, err error) fetchUsability {
	if err != nil {
		return fetchUsability{HasError: true}
	}
	if content == "" || strings.Contains(content, "<error>") {
		return fetchUsability{HasError: true}
	}
	return assessMetaBody(fetchResultSignalList(content), fetchResultBody(content))
}

func isUsableFetchContent(content string) bool {
	return assessFetchResult(content, nil).Usable
}

func selectUsableFetches(candidates []string, results []searchFetchOutcome, fetchTop int) []fetchedPage {
	out := make([]fetchedPage, 0, fetchTop)
	for i := range candidates {
		if i >= len(results) || len(out) >= fetchTop {
			break
		}
		a := assessFetchResult(results[i].content, results[i].err)
		if !a.Usable {
			continue
		}
		out = append(out, fetchedPage{url: candidates[i], content: results[i].content})
	}
	return out
}

type urlFetchDetailedFunc func(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore, targetURL string, maxChars int, focus string) (fetchOutcome, error)

const hybridFillWave = 2

// fillUsableFetches fetches a first parallel wave, then continues sequentially
// until fetchTop usable pages are collected (early stop).
func fillUsableFetches(
	ctx context.Context,
	cache *FetchCache,
	localAI *LocalAIExtractor,
	spill tooldeps.SpilloverStore,
	candidates []string,
	fetchTop, perCandidateChars int,
	focus string,
	fetch urlFetchDetailedFunc,
) []fetchedPage {
	if fetch == nil {
		fetch = webFetchURLDetailed
	}
	out := make([]fetchedPage, 0, fetchTop)
	var skippedThin, skippedErr, tried int

	consider := func(u string, outc fetchOutcome, err error) {
		tried++
		a := outc.Assess
		if err != nil {
			a = fetchUsability{HasError: true}
		} else if !a.Usable && !a.Thin && !a.HasError && a.BodyChars == 0 && len(a.Signals) == 0 {
			// Zero assess (cache/legacy) — derive from envelope.
			a = assessFetchResult(outc.Content, nil)
		}
		if !a.Usable {
			if a.HasError {
				skippedErr++
			} else if a.Thin {
				skippedThin++
			} else {
				skippedErr++
			}
			return
		}
		out = append(out, fetchedPage{url: u, content: outc.Content})
	}

	wave := hybridFillWave
	if wave > len(candidates) {
		wave = len(candidates)
	}
	if wave > 0 && len(out) < fetchTop {
		type indexed struct {
			url string
			out fetchOutcome
			err error
		}
		batch := make([]indexed, wave)
		var wg sync.WaitGroup
		for i := 0; i < wave; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				o, e := fetch(ctx, cache, localAI, spill, candidates[idx], perCandidateChars, focus)
				batch[idx] = indexed{url: candidates[idx], out: o, err: e}
			}(i)
		}
		wg.Wait()
		for i := 0; i < wave && len(out) < fetchTop; i++ {
			consider(batch[i].url, batch[i].out, batch[i].err)
		}
	}

	for i := wave; i < len(candidates) && len(out) < fetchTop; i++ {
		o, e := fetch(ctx, cache, localAI, spill, candidates[i], perCandidateChars, focus)
		consider(candidates[i], o, e)
	}

	slog.Info(
		"web search+fetch fill",
		"wanted", fetchTop,
		"filled", len(out),
		"tried", tried,
		"pool", len(candidates),
		"skipped_thin", skippedThin,
		"skipped_error", skippedErr,
		"wave", wave,
	)
	return out
}

func fetchResultSignalList(content string) []string {
	raw := fetchResultSignals(content)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fetchResultSignals(content string) string {
	const prefix = "Signals: "
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line[len(prefix):]
		}
	}
	return ""
}

func fetchResultBody(content string) string {
	start := strings.Index(content, "<content>\n")
	if start < 0 {
		return ""
	}
	body := content[start+len("<content>\n"):]
	body = strings.TrimSuffix(body, "\n</content>")
	body = strings.TrimSuffix(body, "</content>")
	return body
}
