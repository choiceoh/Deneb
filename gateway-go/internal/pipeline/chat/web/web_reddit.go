// web_reddit.go — Reddit read handler.
//
// Reddit exposes every public page as JSON by appending `.json` to the path:
// comment threads, subreddit listings, user pages, and search all work with no
// auth (a descriptive User-Agent is required — Reddit rate-limits blank/generic
// agents). The SPA HTML the stealth fetcher would otherwise get is a JS shell
// with no post body, so intercepting Reddit URLs here and reading the JSON API
// is both cheaper and far higher fidelity — the same rationale as the YouTube
// transcript path.
//
// This is read-only. Failures degrade to an informative <error> envelope (never
// a Go error) so the agent learns why a page could not be read, mirroring
// fetchYouTube.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// redditUserAgent is descriptive per Reddit's API etiquette; generic or
	// blank agents get throttled or blocked outright.
	redditUserAgent = "deneb:web-read:1.0 (personal assistant, read-only)"
	// redditMaxBytes caps the JSON body — comment threads can be large.
	redditMaxBytes = 3 * 1024 * 1024
	// redditMaxCommentDepth bounds reply nesting rendered inline.
	redditMaxCommentDepth = 4
	// redditCommentBodyLimit truncates an individual comment/selftext body.
	redditCommentBodyLimit = 600
	// redditMaxListItems bounds entries rendered from a subreddit/search listing.
	redditMaxListItems = 25
)

// isRedditURL reports whether a URL should be read via Reddit's JSON API.
// Covers reddit.com (and subdomains) plus the redd.it post shortener; the
// i.redd.it / v.redd.it media CDNs are intentionally excluded (they are images
// and video, not JSON-readable posts).
func isRedditURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "reddit.com" || strings.HasSuffix(h, ".reddit.com") {
		return true
	}
	return h == "redd.it"
}

// redditJSONURL rewrites a Reddit page URL to its .json variant, preserving the
// query string (so `/search?q=...` and listing sorts carry through). redd.it
// short links (redd.it/<id>) are expanded to the by-id comments endpoint, since
// appending .json to the shortener path does not resolve.
func redditJSONURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(u.Hostname(), "redd.it") {
		id := strings.Trim(u.Path, "/")
		if id == "" || strings.Contains(id, "/") {
			return "", fmt.Errorf("unsupported redd.it URL: %s", raw)
		}
		return "https://www.reddit.com/comments/" + id + ".json?raw_json=1", nil
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" {
		p = "/"
	}
	if !strings.HasSuffix(p, ".json") {
		p += ".json"
	}
	u.Path = p
	// Prefer the higher-fidelity `raw_json=1` (unescaped entities). Comment
	// volume and reply depth are bounded at render time (renderRedditComments),
	// not via a query-string cap.
	q := u.Query()
	q.Set("raw_json", "1")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Reddit JSON envelope types (only the fields we render).

type redditThing struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

type redditListing struct {
	Children []redditThing `json:"children"`
}

type redditPost struct {
	Subreddit    string  `json:"subreddit"`
	Title        string  `json:"title"`
	Author       string  `json:"author"`
	Selftext     string  `json:"selftext"`
	URL          string  `json:"url"`
	Score        int     `json:"score"`
	NumComments  int     `json:"num_comments"`
	Permalink    string  `json:"permalink"`
	CreatedUTC   float64 `json:"created_utc"`
	IsSelf       bool    `json:"is_self"`
	Over18       bool    `json:"over_18"`
	LinkFlair    string  `json:"link_flair_text"`
	CrosspostCnt int     `json:"num_crossposts"`
}

type redditComment struct {
	Author     string          `json:"author"`
	Body       string          `json:"body"`
	Score      int             `json:"score"`
	CreatedUTC float64         `json:"created_utc"`
	Replies    json.RawMessage `json:"replies"`
}

// fetchReddit reads a Reddit URL via its JSON API and renders compact markdown.
func fetchReddit(ctx context.Context, rawURL string, maxChars int) (string, error) {
	apiURL, err := redditJSONURL(rawURL)
	if err != nil {
		//nolint:nilerr // tool contract: fetch failures render as result text for the model
		return formatFetchError(webFetchErr{
			Code: "reddit_bad_url", Message: err.Error(), URL: rawURL, Retryable: false,
		}), nil
	}

	body, status, truncated, err := socialGetJSON(ctx, apiURL, redditUserAgent, redditMaxBytes)
	if err != nil {
		return formatFetchError(classifyFetchError(err, rawURL)), nil
	}
	if truncated {
		return formatFetchError(webFetchErr{
			Code: "content_too_large", Message: "Reddit response exceeded the read limit",
			URL: rawURL, Retryable: false,
			Hint: "Fetch a specific comment permalink or a narrower listing (e.g. a single thread instead of a whole subreddit).",
		}), nil
	}
	if status != 200 {
		// Reddit's public .json API now 403s/429s datacenter-egress IPs even for
		// public content, while the JS-rendered HTML site still serves. Escalate
		// the block/throttle case to the resident browser sidecar, which drives a
		// real headful Chromium and retrieves the page the API refuses. Other
		// statuses (404 deleted, 5xx down) a re-fetch cannot help, so only the
		// 403/429 block path escalates.
		if status == 403 || status == 429 {
			// old.reddit.com first: it is server-rendered HTML, so it needs no
			// JS challenge, serves lang=en regardless of the fetcher's locale,
			// and carries the whole comment tree. Measured 2026-07-25 on a live
			// thread the .json API 403'd: 92K chars extracted with the full
			// comment tree, versus 1.4K of Korean-localized nav/ads/login-wall
			// from the browser sidecar on the same thread.
			if rendered, ok := fetchRedditViaOldHTML(ctx, rawURL, maxChars); ok {
				return rendered, nil
			}
			if rendered, ok := fetchRedditViaBrowser(ctx, rawURL, maxChars); ok {
				return rendered, nil
			}
		}
		hint := hintForHTTPStatus(status)
		if status == 403 || status == 429 {
			hint = "Reddit throttled or blocked the read. Retry later; private/quarantined subs need login."
		}
		return formatFetchError(webFetchErr{
			Code: fmt.Sprintf("reddit_http_%d", status), Message: "Reddit returned a non-OK status",
			URL: rawURL, Retryable: status >= 500 || status == 429, Hint: hint,
		}), nil
	}

	out := renderReddit(body, rawURL, maxChars)
	if out == "" {
		return formatFetchError(webFetchErr{
			Code: "reddit_empty", Message: "Reddit returned no readable content",
			URL: rawURL, Retryable: false,
			Hint: "The page may be deleted, private, or an unsupported Reddit URL shape.",
		}), nil
	}
	return out, nil
}

// Seam (browserRenderFn pattern): tests swap this to avoid network and DNS.
var oldRedditFetchFn = fetchWithRetry

// oldRedditURL rewrites a Reddit URL onto the server-rendered old.reddit.com
// host, preserving path and query. Returns ("", false) when the host is already
// old.reddit.com (no point re-fetching) or the URL will not parse.
func oldRedditURL(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	if strings.EqualFold(u.Host, "old.reddit.com") {
		return "", false
	}
	u.Host = "old.reddit.com"
	u.Scheme = "https"
	return u.String(), true
}

// fetchRedditViaOldHTML reads the thread from old.reddit.com when the .json API
// blocks the request. The modern site is a JS SPA behind a bot challenge that
// also localizes its chrome to the fetcher's language, so the browser sidecar
// comes back with translated navigation instead of the discussion; old.reddit is
// plain server-rendered HTML in English with the comment tree inline. Returns
// ("", false) on any failure so the caller can still try the browser sidecar.
func fetchRedditViaOldHTML(ctx context.Context, rawURL string, maxChars int) (string, bool) {
	target, ok := oldRedditURL(rawURL)
	if !ok {
		return "", false
	}
	// Read the whole page: old.reddit thread HTML runs ~750KB for a busy thread
	// (measured) because the comment tree is inline, and a byte cap derived from
	// maxChars truncates it into an unextractable fragment. The same
	// redditMaxBytes ceiling the JSON path uses applies here; the extracted text
	// is truncated to maxChars below, after parsing.
	result, err := oldRedditFetchFn(ctx, target, redditMaxBytes)
	if err != nil || result == nil || len(result.Data) == 0 {
		return "", false
	}
	meta := webFetchMeta{}
	text := strings.TrimSpace(processHTML(ctx, string(result.Data), target, nil, &meta))
	if text == "" {
		return "", false
	}
	return applyTruncation(formatFetchResult(meta, text), maxChars), true
}

// fetchRedditViaBrowser renders the original (HTML) Reddit URL through the
// resident browser sidecar when the .json API blocks the read. It reuses
// browserRenderFn — the same injectable renderer the general fetch escalation
// uses — so the browser fingerprint and SSRF guard stay identical. Returns
// (text, true) on a non-empty render; ("", false) on any failure (sidecar down,
// empty render) so the caller falls back to the original block error.
func fetchRedditViaBrowser(ctx context.Context, rawURL string, maxChars int) (string, bool) {
	maxBytes := int64(maxChars) * 2
	if maxBytes <= 0 || maxBytes > redditMaxBytes {
		maxBytes = redditMaxBytes
	}
	result, err := browserRenderFn(ctx, rawURL, maxBytes)
	if err != nil || result == nil {
		return "", false
	}
	text := strings.TrimSpace(string(result.Data))
	if text == "" {
		return "", false
	}
	return text, true
}

// renderReddit dispatches on the JSON shape: a 2-element array is a comment
// thread (post + comments); a single object is a listing (subreddit/search/user).
func renderReddit(body []byte, srcURL string, maxChars int) string {
	trimmed := strings.TrimLeft(string(body), " \t\r\n")
	if trimmed == "" {
		return ""
	}
	switch trimmed[0] {
	case '[':
		return renderRedditThread(body, srcURL, maxChars)
	case '{':
		return renderRedditListing(body, srcURL, maxChars)
	default:
		return ""
	}
}

func renderRedditThread(body []byte, srcURL string, maxChars int) string {
	var top []redditThing
	if err := json.Unmarshal(body, &top); err != nil || len(top) < 1 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<content>\nSource: ")
	b.WriteString(srcURL)
	b.WriteString(" (reddit)\n\n")

	// Post is the first listing's first child.
	var postListing redditListing
	if json.Unmarshal(top[0].Data, &postListing) == nil && len(postListing.Children) > 0 {
		var p redditPost
		if json.Unmarshal(postListing.Children[0].Data, &p) == nil {
			writeRedditPostHeader(&b, &p)
		}
	}

	// Comments are the second listing's children.
	if len(top) >= 2 {
		var cl redditListing
		if json.Unmarshal(top[1].Data, &cl) == nil && len(cl.Children) > 0 {
			b.WriteString("\n## Comments\n")
			budget := maxChars - b.Len()
			renderRedditComments(&b, cl.Children, 0, &budget)
		}
	}

	b.WriteString("\n</content>")
	return applyTruncation(b.String(), maxChars)
}

func writeRedditPostHeader(b *strings.Builder, p *redditPost) {
	fmt.Fprintf(b, "# %s\n", collapseWS(p.Title, 300))
	fmt.Fprintf(b, "r/%s · u/%s · ▲%d · %d comments", p.Subreddit, p.Author, p.Score, p.NumComments)
	if p.LinkFlair != "" {
		fmt.Fprintf(b, " · [%s]", collapseWS(p.LinkFlair, 100))
	}
	if p.Over18 {
		b.WriteString(" · NSFW")
	}
	if ts := formatUnix(p.CreatedUTC); ts != "" {
		fmt.Fprintf(b, " · %s", ts)
	}
	b.WriteString("\n")
	if !p.IsSelf && p.URL != "" {
		fmt.Fprintf(b, "Link: %s\n", p.URL)
	}
	if strings.TrimSpace(p.Selftext) != "" {
		fmt.Fprintf(b, "\n%s\n", collapseWS(p.Selftext, 2000))
	}
}

func renderRedditComments(b *strings.Builder, children []redditThing, depth int, budget *int) {
	for _, c := range children {
		if *budget <= 0 {
			return
		}
		if c.Kind != "t1" {
			continue // skip "more" placeholders and non-comment kinds
		}
		var cm redditComment
		if json.Unmarshal(c.Data, &cm) != nil {
			continue
		}
		if cm.Author == "" && strings.TrimSpace(cm.Body) == "" {
			continue
		}
		indent := strings.Repeat("  ", depth)
		line := fmt.Sprintf("%s- u/%s (▲%d): %s\n", indent, cm.Author, cm.Score, collapseWS(cm.Body, redditCommentBodyLimit))
		b.WriteString(line)
		*budget -= len(line)
		if depth < redditMaxCommentDepth && len(cm.Replies) > 0 && cm.Replies[0] == '{' {
			var rt redditThing
			if json.Unmarshal(cm.Replies, &rt) == nil {
				var rl redditListing
				if json.Unmarshal(rt.Data, &rl) == nil {
					renderRedditComments(b, rl.Children, depth+1, budget)
				}
			}
		}
	}
}

func renderRedditListing(body []byte, srcURL string, maxChars int) string {
	var thing redditThing
	if json.Unmarshal(body, &thing) != nil {
		return ""
	}
	var listing redditListing
	if json.Unmarshal(thing.Data, &listing) != nil || len(listing.Children) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<content>\nSource: %s (reddit listing)\n\n", srcURL)

	n := 0
	for _, child := range listing.Children {
		if n >= redditMaxListItems || b.Len() >= maxChars {
			break
		}
		switch child.Kind {
		case "t3": // post
			var p redditPost
			if json.Unmarshal(child.Data, &p) != nil {
				continue
			}
			n++
			fmt.Fprintf(&b, "%d. %s\n", n, collapseWS(p.Title, 200))
			fmt.Fprintf(&b, "   r/%s · u/%s · ▲%d · %d comments", p.Subreddit, p.Author, p.Score, p.NumComments)
			if ts := formatUnix(p.CreatedUTC); ts != "" {
				fmt.Fprintf(&b, " · %s", ts)
			}
			b.WriteString("\n")
			if p.Permalink != "" {
				fmt.Fprintf(&b, "   https://www.reddit.com%s\n", p.Permalink)
			}
		case "t1": // comment (user pages)
			var cm redditComment
			if json.Unmarshal(child.Data, &cm) != nil {
				continue
			}
			n++
			fmt.Fprintf(&b, "%d. u/%s (▲%d): %s\n", n, cm.Author, cm.Score, collapseWS(cm.Body, 300))
		}
	}

	if n == 0 {
		return ""
	}
	b.WriteString("</content>")
	return applyTruncation(b.String(), maxChars)
}

// formatUnix renders a Reddit/X epoch seconds value as a UTC date, or "" for 0.
func formatUnix(sec float64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(int64(sec), 0).UTC().Format("2006-01-02")
}
