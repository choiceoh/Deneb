// web_fetch_rank.go — search+fetch candidate ranking, filters, and usable fill.
package web

import (
	"context"
	"net/url"
	"path"
	"strings"
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

// rankFetchCandidates orders answer-box link first, then organic URLs scored by
// query overlap with diversity demotion, then filters denylist/media/YouTube
// non-video/host+eTLD diversity. limit caps the returned pool.
func rankFetchCandidates(query, answerLink string, organic []searchResult, limit int) []string {
	ordered := make([]string, 0, 1+len(organic))
	if link := strings.TrimSpace(answerLink); link != "" {
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
	// Stable: higher score first, then original index.
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

	// Greedy diversity: prefer distinct eTLD+1 and path prefix.
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
		// naive eTLD+1 (good enough for diversity; not PSL-accurate)
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

// filterFetchCandidates drops empty/javascript/media/denied-host/YouTube-non-video/
// low-quality-path URLs and duplicate hosts (first wins). PDF and Office stay.
func filterFetchCandidates(urls []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seenHost := make(map[string]struct{}, limit)
	seenETLD := make(map[string]struct{}, limit)
	out := make([]string, 0, limit)
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		if isNonDocumentMediaURL(parsed) || isDeniedFetchHost(parsed.Host) ||
			isDeniedYouTubeURL(parsed) || isLowQualityFetchPath(parsed) {
			continue
		}
		host := strings.ToLower(parsed.Host)
		if _, ok := seenHost[host]; ok {
			continue
		}
		etld := registrableDomain(host)
		if _, ok := seenETLD[etld]; ok {
			continue
		}
		seenHost[host] = struct{}{}
		seenETLD[etld] = struct{}{}
		out = append(out, raw)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// selectFetchURLs is the legacy entry used by tests; it filters without an
// answer-box link. Prefer rankFetchCandidates for search+fetch.
func selectFetchURLs(urls []string, limit int) []string {
	return filterFetchCandidates(urls, limit)
}

// Social/aggregator hosts rarely yield article body for research; skip them
// so fetchTop budget goes to document-like pages. YouTube video URLs are kept
// (transcript); channel/home URLs are denied separately.
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

// isDeniedYouTubeURL skips channel/home/playlist pages; keeps watch/shorts/live
// URLs that the transcript path can handle.
func isDeniedYouTubeURL(u *url.URL) bool {
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
	default:
		return false
	}
	if media.IsYouTubeURL(u.String()) {
		return false
	}
	return true
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
	// Site-internal search result pages are rarely useful article bodies.
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

const usableFetchMinChars = 400 // aligned with thinContentThreshold

var thinFetchSignals = []string{"js_required", "empty_body", "low_content_yield"}

// assessFetchResult returns a structured usability verdict for a fetch envelope.
func assessFetchResult(content string, err error) fetchUsability {
	if err != nil {
		return fetchUsability{HasError: true}
	}
	if content == "" || strings.Contains(content, "<error>") {
		return fetchUsability{HasError: true}
	}
	signals := fetchResultSignalList(content)
	bodyChars := len(strings.TrimSpace(fetchResultBody(content)))
	thin := false
	for _, s := range thinFetchSignals {
		for _, got := range signals {
			if got == s {
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
		Signals:   signals,
	}
	if !thin {
		u.Usable = true
		return u
	}
	u.Usable = bodyChars >= usableFetchMinChars
	return u
}

func isUsableFetchContent(content string) bool {
	return assessFetchResult(content, nil).Usable
}

// selectUsableFetches keeps up to fetchTop pages that are not errors/thin SPA
// shells, preserving original candidate order. Used by tests; production fill
// uses fillUsableFetches (sequential early-stop).
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

type urlFetchFunc func(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore, targetURL string, maxChars int) (string, error)

// fillUsableFetches fetches candidates in order until fetchTop usable pages are
// collected, then stops (saves scrape credits vs fetching the whole pool).
func fillUsableFetches(
	ctx context.Context,
	cache *FetchCache,
	localAI *LocalAIExtractor,
	spill tooldeps.SpilloverStore,
	candidates []string,
	fetchTop, perCandidateChars int,
	fetch urlFetchFunc,
) []fetchedPage {
	if fetch == nil {
		fetch = webFetchURL
	}
	out := make([]fetchedPage, 0, fetchTop)
	for _, u := range candidates {
		if len(out) >= fetchTop {
			break
		}
		content, err := fetch(ctx, cache, localAI, spill, u, perCandidateChars)
		if !assessFetchResult(content, err).Usable {
			continue
		}
		out = append(out, fetchedPage{url: u, content: content})
	}
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
