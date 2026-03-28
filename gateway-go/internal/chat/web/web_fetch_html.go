// web_fetch_html.go — HTML preprocessing for agent-oriented content extraction.
//
// Handles three critical tasks BEFORE the HTML→Markdown conversion:
//  1. Noise element stripping (nav, aside, footer, ads, cookie banners)
//  2. Metadata extraction (OG, JSON-LD, meta tags, charset)
//  3. Quality signal detection (login walls, SPA shells, bot blocks)
//
// This Go-side preprocessing is the first line of defense for content quality.
// It runs regardless of whether SGLang AI extraction is available.
package web

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// --- HTML noise element stripping ---

// noiseTagNames are block-level elements that typically contain non-content.
// Stripped entirely (open tag through close tag) before conversion.
var noiseTagNames = []string{
	"nav", "aside", "footer", "header",
	"noscript", "svg", "iframe", "form",
}

// noiseClassIDPatterns match class/id attribute values that indicate noise.
// Elements matching these are stripped as whole blocks.
var noiseClassIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)class=["'][^"']*(cookie[-_]?(?:banner|consent|notice|popup|bar)|gdpr|cc-window|CookieConsent)[^"']*["']`),
	regexp.MustCompile(`(?i)class=["'][^"']*(sidebar|side-bar|widget-area|related-(?:posts|articles)|recommended)[^"']*["']`),
	regexp.MustCompile(`(?i)class=["'][^"']*(comment(?:s|-section|-list|-area)|disqus|discourse)[^"']*["']`),
	regexp.MustCompile(`(?i)class=["'][^"']*(ad[-_]?(?:banner|container|wrapper|slot|unit)|advertisement|sponsored|promo[-_]?banner)[^"']*["']`),
	regexp.MustCompile(`(?i)class=["'][^"']*(social[-_]?(?:share|buttons|links|media)|share-buttons)[^"']*["']`),
	regexp.MustCompile(`(?i)class=["'][^"']*(breadcrumb|pagination|pager|page-navigation)[^"']*["']`),
	regexp.MustCompile(`(?i)id=["'](cookie[-_]?(?:banner|consent|notice)|gdpr|sidebar|comments|disqus_thread|ad[-_]container)["']`),
}

// stripNoiseElements removes non-content HTML elements before Markdown conversion.
// This is a critical preprocessing step: without it, nav menus, cookie banners,
// and ad blocks consume tokens that should be spent on actual content.
func stripNoiseElements(html string) string {
	// Phase 1: Strip known noise tag blocks.
	result := html
	for _, tag := range noiseTagNames {
		result = stripTagBlock(result, tag)
	}

	// Phase 2: Strip div/section blocks with noise class/id attributes.
	// We search for noise markers and strip the enclosing block.
	for _, pattern := range noiseClassIDPatterns {
		result = stripMatchingBlocks(result, pattern)
	}

	return result
}

// stripTagBlock removes all occurrences of <tag ...>...</tag> (case-insensitive).
// Handles nested tags of the same type by tracking depth.
func stripTagBlock(html string, tag string) string {
	lower := strings.ToLower(html)
	openPrefix := "<" + tag
	closeTag := "</" + tag + ">"

	var b strings.Builder
	b.Grow(len(html))
	cursor := 0

	for {
		idx := strings.Index(lower[cursor:], openPrefix)
		if idx < 0 {
			break
		}
		start := cursor + idx

		// Verify it's actually a tag boundary (not e.g., <navigate>).
		afterPrefix := start + len(openPrefix)
		if afterPrefix < len(html) {
			next := html[afterPrefix]
			if next != '>' && next != ' ' && next != '\t' && next != '\n' && next != '/' {
				b.WriteString(html[cursor : start+1])
				cursor = start + 1
				continue
			}
		}

		b.WriteString(html[cursor:start])

		// Find matching close tag, handling nesting.
		depth := 1
		searchFrom := afterPrefix
		for depth > 0 {
			nextOpen := strings.Index(lower[searchFrom:], openPrefix)
			nextClose := strings.Index(lower[searchFrom:], closeTag)

			if nextClose < 0 {
				// No closing tag found — strip to end.
				searchFrom = len(html)
				break
			}

			// Check if there's a nested open before this close.
			if nextOpen >= 0 && nextOpen < nextClose {
				absOpen := searchFrom + nextOpen + len(openPrefix)
				// Verify the nested open is a real tag.
				if absOpen < len(html) {
					ch := html[absOpen]
					if ch == '>' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '/' {
						depth++
					}
				}
				searchFrom += nextOpen + len(openPrefix)
			} else {
				depth--
				searchFrom += nextClose + len(closeTag)
			}
		}
		cursor = searchFrom
	}
	b.WriteString(html[cursor:])
	return b.String()
}

// stripMatchingBlocks finds HTML elements whose opening tag matches the regex
// pattern and removes the entire element (open through close tag).
// Works on <div> and <section> elements only to limit blast radius.
func stripMatchingBlocks(html string, pattern *regexp.Regexp) string {
	// Find all matches of the noise pattern.
	locs := pattern.FindAllStringIndex(html, 20) // cap at 20 matches
	if len(locs) == 0 {
		return html
	}

	// For each match, walk backward to find the opening <div or <section,
	// then forward to find the matching close tag.
	type removeRange struct{ start, end int }
	var ranges []removeRange

	lower := strings.ToLower(html)
	for _, loc := range locs {
		matchStart := loc[0]

		// Walk backward to find <div or <section.
		tagStart := -1
		for i := matchStart - 1; i >= 0; i-- {
			if html[i] == '<' {
				prefix := lower[i:]
				if strings.HasPrefix(prefix, "<div") || strings.HasPrefix(prefix, "<section") {
					tagStart = i
				}
				break
			}
		}
		if tagStart < 0 {
			continue
		}

		// Determine the tag name.
		tag := "div"
		if strings.HasPrefix(lower[tagStart:], "<section") {
			tag = "section"
		}

		// Find the matching close tag from the match position.
		closeTag := "</" + tag + ">"
		openPrefix := "<" + tag

		depth := 1
		searchFrom := matchStart
		// Skip past the opening tag's >.
		if gt := strings.IndexByte(html[searchFrom:], '>'); gt >= 0 {
			searchFrom += gt + 1
		}

		for depth > 0 {
			nextOpen := strings.Index(lower[searchFrom:], openPrefix)
			nextClose := strings.Index(lower[searchFrom:], closeTag)

			if nextClose < 0 {
				searchFrom = len(html)
				break
			}

			if nextOpen >= 0 && nextOpen < nextClose {
				// Verify it's a real tag boundary.
				absAfter := searchFrom + nextOpen + len(openPrefix)
				if absAfter < len(html) {
					ch := html[absAfter]
					if ch == '>' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '/' {
						depth++
					}
				}
				searchFrom += nextOpen + len(openPrefix)
			} else {
				depth--
				searchFrom += nextClose + len(closeTag)
			}
		}

		ranges = append(ranges, removeRange{tagStart, searchFrom})
	}

	if len(ranges) == 0 {
		return html
	}

	// Build output, skipping removed ranges (handle overlaps).
	var b strings.Builder
	b.Grow(len(html))
	cursor := 0
	for _, r := range ranges {
		if r.start < cursor {
			continue // overlapping with previous removal
		}
		b.WriteString(html[cursor:r.start])
		cursor = r.end
	}
	b.WriteString(html[cursor:])
	return b.String()
}

// --- Enhanced metadata extraction ---

// webFetchMeta is defined in web_fetch.go. This file extends extraction logic.

var (
	// Title extraction: OG > <title>.
	ogTitleRe    = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
	ogTitleRevRe = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:title["']`)
	titleTagRe   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

	// Description: OG > meta name="description".
	ogDescRe      = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:description["'][^>]+content=["']([^"']+)["']`)
	ogDescRevRe   = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:description["']`)
	metaDescRe    = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["'][^>]+content=["']([^"']+)["']`)
	metaDescRevRe = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+name=["']description["']`)

	// Canonical URL.
	canonicalRe    = regexp.MustCompile(`(?i)<link[^>]+rel=["']canonical["'][^>]+href=["']([^"']+)["']`)
	canonicalRevRe = regexp.MustCompile(`(?i)<link[^>]+href=["']([^"']+)["'][^>]+rel=["']canonical["']`)

	// Language.
	htmlLangRe    = regexp.MustCompile(`(?i)<html[^>]+lang=["']([^"']+)["']`)
	contentLangRe = regexp.MustCompile(`(?i)<meta[^>]+http-equiv=["']content-language["'][^>]+content=["']([^"']+)["']`)

	// Publish date.
	publishRe    = regexp.MustCompile(`(?i)<meta[^>]+(?:property=["']article:published_time["']|name=["'](?:date|publish[_-]?date|DC\.date|datePublished)["'])[^>]+content=["']([^"']+)["']`)
	publishRevRe = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+(?:property=["']article:published_time["']|name=["'](?:date|publish[_-]?date|DC\.date|datePublished)["'])`)
	// Time element with datetime attribute (common in articles).
	timeDatetimeRe = regexp.MustCompile(`(?i)<time[^>]+datetime=["'](\d{4}-\d{2}-\d{2}[^"']*)["']`)

	// Author.
	authorRe    = regexp.MustCompile(`(?i)<meta[^>]+(?:name=["']author["']|property=["']article:author["'])[^>]+content=["']([^"']+)["']`)
	authorRevRe = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+(?:name=["']author["']|property=["']article:author["'])`)

	// Site name.
	siteNameRe    = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:site_name["'][^>]+content=["']([^"']+)["']`)
	siteNameRevRe = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:site_name["']`)

	// Content type (og:type).
	ogTypeRe    = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:type["'][^>]+content=["']([^"']+)["']`)
	ogTypeRevRe = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:type["']`)

	// JSON-LD script blocks.
	jsonLDRe = regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)

	// Charset from meta.
	metaCharsetRe     = regexp.MustCompile(`(?i)<meta[^>]+charset=["']?([^"'\s;>]+)["']?`)
	metaContentTypeRe = regexp.MustCompile(`(?i)<meta[^>]+content=["'][^"']*charset=([^"'\s;]+)`)
)

// extractHTMLMeta parses HTML meta tags, JSON-LD, and structural hints.
// Scans up to the </head> boundary or first 16K, whichever comes first.
func extractHTMLMeta(html string, meta *webFetchMeta) {
	scan := findHeadSection(html)

	// Title: OG > <title>.
	meta.Title = firstMatch(scan, ogTitleRe, ogTitleRevRe, titleTagRe)

	// Description: OG > meta.
	meta.Description = firstMatch(scan, ogDescRe, ogDescRevRe, metaDescRe, metaDescRevRe)

	// Canonical URL.
	meta.CanonicalURL = firstMatch(scan, canonicalRe, canonicalRevRe)

	// Language: html lang > content-language meta.
	meta.Language = firstMatch(scan, htmlLangRe, contentLangRe)

	// Publish date: meta tags > <time datetime>.
	meta.Published = firstMatch(scan, publishRe, publishRevRe)
	if meta.Published == "" {
		// Scan a bit further for <time> elements (often in body).
		timeScan := html
		if len(timeScan) > 32768 {
			timeScan = timeScan[:32768]
		}
		meta.Published = firstMatch(timeScan, timeDatetimeRe)
	}

	// Author.
	meta.Author = firstMatch(scan, authorRe, authorRevRe)

	// Site name.
	meta.SiteName = firstMatch(scan, siteNameRe, siteNameRevRe)

	// OG type (article, website, product, etc.).
	meta.OGType = firstMatch(scan, ogTypeRe, ogTypeRevRe)

	// JSON-LD structured data — rich metadata source.
	extractJSONLD(scan, meta)
}

// findHeadSection returns the portion of HTML up to </head> or first 16K.
func findHeadSection(html string) string {
	lower := strings.ToLower(html)
	if idx := strings.Index(lower, "</head>"); idx >= 0 && idx < 32768 {
		return html[:idx+7]
	}
	if len(html) > 16384 {
		return html[:16384]
	}
	return html
}

// firstMatch tries each regex against text and returns the first capture group
// of the first regex that matches.
func firstMatch(text string, patterns ...*regexp.Regexp) string {
	for _, re := range patterns {
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// extractJSONLD parses <script type="application/ld+json"> blocks for metadata.
// JSON-LD often contains richer data than meta tags: author details, dates,
// article sections, FAQ content, product info, etc.
func extractJSONLD(html string, meta *webFetchMeta) {
	matches := jsonLDRe.FindAllStringSubmatch(html, 5)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		raw := strings.TrimSpace(m[1])
		if raw == "" {
			continue
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			// Try as array (some sites wrap in []).
			var arr []map[string]any
			if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
				data = arr[0]
			} else {
				continue
			}
		}

		// Fill in missing metadata from JSON-LD.
		if meta.Title == "" {
			if v, ok := data["headline"].(string); ok && v != "" {
				meta.Title = v
			} else if v, ok := data["name"].(string); ok && v != "" {
				meta.Title = v
			}
		}
		if meta.Description == "" {
			if v, ok := data["description"].(string); ok && v != "" {
				meta.Description = v
			}
		}
		if meta.Published == "" {
			if v, ok := data["datePublished"].(string); ok && v != "" {
				meta.Published = v
			}
		}
		if meta.Author == "" {
			switch a := data["author"].(type) {
			case string:
				meta.Author = a
			case map[string]any:
				if name, ok := a["name"].(string); ok {
					meta.Author = name
				}
			case []any:
				if len(a) > 0 {
					if m, ok := a[0].(map[string]any); ok {
						if name, ok := m["name"].(string); ok {
							meta.Author = name
						}
					}
				}
			}
		}
		if meta.SiteName == "" {
			if pub, ok := data["publisher"].(map[string]any); ok {
				if name, ok := pub["name"].(string); ok {
					meta.SiteName = name
				}
			}
		}

		// Store first JSON-LD @type for content classification.
		if meta.OGType == "" {
			if v, ok := data["@type"].(string); ok && v != "" {
				meta.OGType = "ld:" + v
			}
		}

		// Extract word count if available (useful for token estimation).
		if wc, ok := data["wordCount"].(float64); ok && wc > 0 {
			meta.WordCount = int(wc)
		}
	}
}

// --- Enhanced quality signal detection ---

// Signal categories with specific patterns for precise detection.
var signalDetectors = []struct {
	signal   string
	patterns []string
	minMatch int // how many patterns must match (default 1)
}{
	{
		signal: "login_wall",
		patterns: []string{
			"login-wall", "paywall", "sign-in-gate", "subscribe-wall",
			"registration-wall", "loginrequired", "login_required",
			"metered-content", "premium-content", "subscriber-only",
			"regwall", "pw-gate", "locked-content",
		},
	},
	{
		signal: "soft_paywall",
		patterns: []string{
			"article-meter", "reading-limit", "free-article",
			"articles remaining", "subscribe to continue",
			"unlock this article", "member-only",
		},
	},
	{
		signal: "cookie_consent",
		patterns: []string{
			"cookie-consent", "cookie-banner", "cookie-notice",
			"cookieconsent", "cc-window", "gdpr-consent",
			"cookie-policy-banner", "onetrust-consent",
			"sp_choice_type", "cmp-container",
		},
	},
	{
		signal: "bot_blocked",
		patterns: []string{
			"blocked by cloudflare", "access denied",
			"please verify you are a human", "unusual traffic",
			"cf-challenge", "challenge-platform",
			"_cf_chl_opt", "ray id",
		},
	},
	{
		signal: "captcha_required",
		patterns: []string{
			"g-recaptcha", "h-captcha", "cf-turnstile",
			"captcha-container", "are you a robot",
			"prove you're human",
		},
	},
	{
		signal: "age_gate",
		patterns: []string{
			"age-gate", "age-verification", "age_gate",
			"verify your age", "confirm your age",
			"you must be 18", "you must be 21",
		},
	},
}

// SPA framework indicators: when these are present but text content is minimal,
// the page likely requires JavaScript rendering.
var spaIndicators = []string{
	`id="__next"`,     // Next.js
	`id="__nuxt"`,     // Nuxt.js
	`id="app"`,        // Vue.js default
	`id="root"`,       // React default
	`ng-app`,          // Angular
	`data-reactroot`,  // React
	`<app-root`,       // Angular
	`window.__NEXT_DATA__`,
	`window.__NUXT__`,
}

func detectSignals(html string) []string {
	lower := strings.ToLower(html)
	var signals []string

	// Pattern-based signal detection.
	for _, detector := range signalDetectors {
		for _, p := range detector.patterns {
			if strings.Contains(lower, p) {
				signals = appendUnique(signals, detector.signal)
				break
			}
		}
	}

	// JS-required detection: look for <noscript> with substantial content,
	// or SPA framework indicators with minimal visible text.
	jsRequired := false
	if idx := strings.Index(lower, "<noscript"); idx >= 0 {
		end := strings.Index(lower[idx:], "</noscript>")
		if end > 0 {
			noscriptContent := lower[idx : idx+end]
			// Substantial noscript message (not just tracking pixels).
			textLen := countVisibleChars(noscriptContent)
			if textLen > 50 {
				jsRequired = true
			}
		}
	}

	// SPA shell detection: framework markers + empty body.
	if !jsRequired {
		bodyText := extractBodyTextLen(lower)
		for _, indicator := range spaIndicators {
			if strings.Contains(lower, indicator) {
				if bodyText < 200 {
					jsRequired = true
				}
				break
			}
		}
	}
	if jsRequired {
		signals = appendUnique(signals, "js_required")
	}

	// Empty body detection.
	bodyText := extractBodyTextLen(lower)
	if len(html) > 5000 && bodyText < 100 {
		signals = appendUnique(signals, "empty_body")
	}

	// Meta refresh detection (server-side redirect to login).
	if strings.Contains(lower, `http-equiv="refresh"`) || strings.Contains(lower, `http-equiv='refresh'`) {
		if strings.Contains(lower, "login") || strings.Contains(lower, "signin") || strings.Contains(lower, "auth") {
			signals = appendUnique(signals, "redirect_to_login")
		}
	}

	return signals
}

// countVisibleChars counts non-tag, non-whitespace characters.
func countVisibleChars(html string) int {
	count := 0
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag && r > ' ' {
			count++
		}
	}
	return count
}

// extractBodyTextLen returns the visible text length inside <body>.
func extractBodyTextLen(lower string) int {
	bodyIdx := strings.Index(lower, "<body")
	if bodyIdx < 0 {
		return 0
	}
	return countVisibleChars(lower[bodyIdx:])
}

// --- Charset detection and normalization ---

// normalizeCharset detects non-UTF-8 encoding and converts to valid UTF-8.
// Handles the most common non-UTF-8 case: ISO-8859-1/Latin-1 (superset of ASCII).
func normalizeCharset(data []byte, contentType string) string {
	// Fast path: already valid UTF-8.
	if utf8.Valid(data) {
		return string(data)
	}

	// Detect charset from Content-Type header.
	cs := detectCharsetName(contentType, string(data))

	// ISO-8859-1/Latin-1: each byte maps directly to a Unicode code point.
	if cs == "iso-8859-1" || cs == "latin1" || cs == "windows-1252" || cs == "" {
		var b strings.Builder
		b.Grow(len(data))
		for _, by := range data {
			b.WriteRune(rune(by))
		}
		return b.String()
	}

	// Unknown charset: replace invalid UTF-8 sequences with replacement character.
	return strings.ToValidUTF8(string(data), "\uFFFD")
}

// detectCharsetName extracts charset name from Content-Type header or HTML meta.
func detectCharsetName(contentType, html string) string {
	lower := strings.ToLower(contentType)
	if idx := strings.Index(lower, "charset="); idx >= 0 {
		cs := lower[idx+8:]
		cs = strings.TrimLeft(cs, " \"'")
		if end := strings.IndexAny(cs, " \"';,"); end >= 0 {
			cs = cs[:end]
		}
		return cs
	}
	// Check HTML meta tag.
	scan := html
	if len(scan) > 4096 {
		scan = scan[:4096]
	}
	if m := metaCharsetRe.FindStringSubmatch(scan); len(m) > 1 {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	if m := metaContentTypeRe.FindStringSubmatch(scan); len(m) > 1 {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	return ""
}

// --- Section-aware truncation ---

// truncateAtSection truncates markdown content at a section boundary
// (heading or paragraph break) near the target length, rather than
// cutting mid-sentence. This preserves structural coherence.
func truncateAtSection(content string, maxChars int) (string, bool) {
	if len(content) <= maxChars {
		return content, false
	}

	// Search backward from maxChars for a good break point.
	// Priority: heading > double newline > single newline.
	searchStart := maxChars
	if searchStart > len(content) {
		searchStart = len(content)
	}

	// Look for the last heading within the limit.
	bestBreak := -1
	window := content[:searchStart]

	// Find last heading (# at start of line).
	for i := searchStart - 1; i > maxChars/2; i-- {
		if i > 0 && content[i] == '#' && content[i-1] == '\n' {
			bestBreak = i - 1
			break
		}
	}

	// If no heading found, look for last paragraph break (double newline).
	if bestBreak < 0 {
		if idx := strings.LastIndex(window[maxChars/2:], "\n\n"); idx >= 0 {
			bestBreak = maxChars/2 + idx
		}
	}

	// If still nothing, look for last single newline.
	if bestBreak < 0 {
		if idx := strings.LastIndex(window[maxChars*3/4:], "\n"); idx >= 0 {
			bestBreak = maxChars*3/4 + idx
		}
	}

	// Fallback: hard cut.
	if bestBreak < 0 {
		bestBreak = maxChars
	}

	return content[:bestBreak], true
}
