// web_x.go — X (Twitter) single-tweet read handler.
//
// X's public web pages are login-walled SPA shells: the stealth fetcher gets an
// empty React root, never the tweet text. The one reliable no-auth path is the
// syndication endpoint (cdn.syndication.twimg.com/tweet-result) that powers
// embedded tweets — it returns structured JSON (text, author, media, quoted /
// parent tweet) for a single status ID. It requires a `token` derived from the
// ID; we reproduce the well-known react-tweet derivation (Vercel's official
// embed library) below.
//
// Scope is deliberately narrow and honest: single tweet / reply-parent / quoted
// tweet READ only. Search, timelines, and full downstream threads are NOT
// reachable without authentication, by design of the endpoint. Any failure
// (bad token, deleted/protected tweet, tombstone) degrades to an informative
// <error> envelope — never a Go error, and never worse than today's SPA-shell
// result.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	xUserAgent = "Mozilla/5.0 (compatible; deneb-web-read/1.0; read-only)"
	xMaxBytes  = 512 * 1024
)

// xSyndicationFeatures mirrors the feature flags react-tweet sends; the endpoint
// is picky about their presence for some tweets.
const xSyndicationFeatures = "tfw_timeline_list:;tfw_follower_count_sunset:true;" +
	"tfw_tweet_edit_backend:on;tfw_refsrc_session:on;tfw_fosnr_soft_interventions_enabled:on;" +
	"tfw_show_birdwatch_pivots_enabled:on;tfw_show_business_verified_badge:on;" +
	"tfw_duplicate_scribes_to_settings:on;tfw_use_profile_image_shape_enabled:on;" +
	"tfw_show_blue_verified_badge:on;tfw_legacy_timeline_sunset:on;" +
	"tfw_show_gov_verified_badge:on;tfw_show_business_affiliate_badge:on;tfw_tweet_edit_frontend:on"

var xStatusRe = regexp.MustCompile(`(?:^|/)status(?:es)?/(\d+)`)

// isXStatusURL reports whether a URL is an x.com/twitter.com single-tweet page
// and, if so, returns the status ID.
func isXStatusURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	h := strings.ToLower(u.Hostname())
	isX := h == "x.com" || h == "twitter.com" ||
		strings.HasSuffix(h, ".x.com") || strings.HasSuffix(h, ".twitter.com")
	if !isX {
		return "", false
	}
	m := xStatusRe.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// syndicationToken reproduces react-tweet's getToken:
//
//	((Number(id) / 1e15) * Math.PI).toString(36).replace(/(0+|\.)/g, '')
//
// i.e. base-36 of the float with every '0' and '.' character removed. The
// base-36 rendering must match JS Number.prototype.toString(36) EXACTLY
// (shortest round-trip), not an approximation — the endpoint rejects a token
// with the wrong trailing digits — so doubleToBase36 ports V8's algorithm.
func syndicationToken(id string) string {
	n, err := strconv.ParseFloat(id, 64)
	if err != nil {
		return ""
	}
	v := (n / 1e15) * math.Pi
	base36 := doubleToBase36(v)
	var b strings.Builder
	for _, c := range base36 {
		if c != '0' && c != '.' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// doubleToBase36 renders a finite non-negative float in base 36 exactly as JS
// Number.prototype.toString(36) does. It is a direct port of V8's
// DoubleToRadixCString (conversions.cc): fractional digits are emitted until the
// running half-ULP `delta` guarantees a shortest round-trip, with round-to-even
// and carry propagation back into the integer part. Reproducing this precisely
// is what makes the X syndication token match — a fixed-digit approximation
// produces spurious trailing digits and the endpoint rejects it.
func doubleToBase36(value float64) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	const radix = 36.0

	if value < 0 {
		value = -value
	}
	integer := math.Floor(value)
	fraction := value - integer

	// Fractional part. buf[0] is '.'; digits follow.
	buf := make([]byte, 0, 32)
	delta := 0.5 * (math.Nextafter(value, math.Inf(1)) - value)
	if delta < math.SmallestNonzeroFloat64 {
		delta = math.SmallestNonzeroFloat64
	}
	if fraction >= delta {
		buf = append(buf, '.')
		for {
			fraction *= radix
			delta *= radix
			digit := int(fraction)
			buf = append(buf, chars[digit])
			fraction -= float64(digit)
			if fraction > 0.5 || (fraction == 0.5 && digit&1 == 1) {
				if fraction+delta > 1 {
					// Round up: increment the last digit, propagating carry by
					// dropping trailing radix-1 digits, up into the integer part.
					for {
						if len(buf) == 1 { // only '.' left → carry into integer
							integer++
							break
						}
						last := buf[len(buf)-1]
						d := int(last - '0')
						if last > '9' {
							d = int(last-'a') + 10
						}
						if d+1 < int(radix) {
							buf[len(buf)-1] = chars[d+1]
							break
						}
						buf = buf[:len(buf)-1] // drop radix-1 digit, keep carrying
					}
					break
				}
			}
			if fraction < delta {
				break
			}
		}
	}

	// Integer part (emitted most-significant first).
	var ip []byte
	if integer == 0 {
		ip = append(ip, '0')
	} else {
		for integer > 0 {
			r := int(math.Mod(integer, radix))
			ip = append(ip, chars[r])
			integer = math.Floor(integer / radix)
		}
		for i, j := 0, len(ip)-1; i < j; i, j = i+1, j-1 {
			ip[i], ip[j] = ip[j], ip[i]
		}
	}
	return string(ip) + string(buf)
}

// X syndication response types (only the fields we render).

type xUser struct {
	Name       string `json:"name"`
	ScreenName string `json:"screen_name"`
	Verified   bool   `json:"verified"`
}

type xMediaPhoto struct {
	URL string `json:"url"`
}

type xTweet struct {
	Typename    string        `json:"__typename"`
	Text        string        `json:"text"`
	CreatedAt   string        `json:"created_at"`
	User        xUser         `json:"user"`
	Favorites   int           `json:"favorite_count"`
	Replies     int           `json:"conversation_count"`
	Lang        string        `json:"lang"`
	Photos      []xMediaPhoto `json:"photos"`
	Video       *struct{}     `json:"video"`
	QuotedTweet *xTweet       `json:"quoted_tweet"`
	Parent      *xTweet       `json:"parent"`
}

// fetchXTweet reads a single tweet via the syndication endpoint.
func fetchXTweet(ctx context.Context, rawURL, id string, maxChars int) (string, error) {
	tok := syndicationToken(id)
	if tok == "" {
		return xUnavailable(rawURL, "could not derive syndication token"), nil
	}

	q := url.Values{}
	q.Set("id", id)
	q.Set("lang", "en")
	q.Set("features", xSyndicationFeatures)
	q.Set("token", tok)
	apiURL := "https://cdn.syndication.twimg.com/tweet-result?" + q.Encode()

	body, status, truncated, err := socialGetJSON(ctx, apiURL, xUserAgent, xMaxBytes)
	if err != nil {
		return formatFetchError(classifyFetchError(err, rawURL)), nil
	}
	if truncated {
		return xUnavailable(rawURL, "syndication response exceeded the read limit"), nil
	}
	if status != 200 {
		return xUnavailable(rawURL, fmt.Sprintf("syndication returned HTTP %d", status)), nil
	}

	var tw xTweet
	if json.Unmarshal(body, &tw) != nil {
		//nolint:nilerr // tool contract: fetch failures render as result text for the model
		return xUnavailable(rawURL, "could not parse syndication response"), nil
	}
	if tw.Typename == "TweetTombstone" || strings.TrimSpace(tw.Text) == "" {
		return xUnavailable(rawURL, "tweet is deleted, protected, or age-restricted"), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<content>\nSource: %s (x/twitter)\n\n", rawURL)
	if tw.Parent != nil && strings.TrimSpace(tw.Parent.Text) != "" {
		b.WriteString("↳ In reply to:\n")
		writeXTweet(&b, tw.Parent, "> ")
		b.WriteString("\n")
	}
	writeXTweet(&b, &tw, "")
	if tw.QuotedTweet != nil && strings.TrimSpace(tw.QuotedTweet.Text) != "" {
		b.WriteString("\n┌ Quoting:\n")
		writeXTweet(&b, tw.QuotedTweet, "│ ")
	}
	b.WriteString("\n</content>")
	return applyTruncation(b.String(), maxChars), nil
}

func writeXTweet(b *strings.Builder, tw *xTweet, prefix string) {
	handle := tw.User.ScreenName
	name := neutralizeAngleBrackets(tw.User.Name)
	switch {
	case name != "" && handle != "":
		fmt.Fprintf(b, "%s%s (@%s)", prefix, name, handle)
	case handle != "":
		fmt.Fprintf(b, "%s@%s", prefix, handle)
	default:
		fmt.Fprintf(b, "%s(unknown author)", prefix)
	}
	if tw.CreatedAt != "" {
		fmt.Fprintf(b, " · %s", tw.CreatedAt)
	}
	b.WriteString("\n")
	// Neutralize angle brackets so a tweet body containing "<error>" (common in
	// code snippets) cannot collide with the web tool's envelope tags.
	for _, line := range strings.Split(neutralizeAngleBrackets(tw.Text), "\n") {
		fmt.Fprintf(b, "%s%s\n", prefix, line)
	}
	if len(tw.Photos) > 0 {
		fmt.Fprintf(b, "%s[%d image(s)]\n", prefix, len(tw.Photos))
	}
	if tw.Video != nil {
		fmt.Fprintf(b, "%s[video]\n", prefix)
	}
	if tw.Favorites > 0 || tw.Replies > 0 {
		fmt.Fprintf(b, "%s♥%d · 💬%d\n", prefix, tw.Favorites, tw.Replies)
	}
}

// xUnavailable renders the honest "no-auth read failed" envelope. The text is
// deliberately explicit that X gates most reads behind login, so the agent does
// not keep retrying a fundamentally unreachable page.
func xUnavailable(rawURL, reason string) string {
	return formatFetchError(webFetchErr{
		Code:      "x_unavailable",
		Message:   "Could not read this tweet without authentication: " + reason,
		URL:       rawURL,
		Retryable: false,
		Hint:      "X allows no-auth reads only for public single tweets. Search, timelines, and protected/deleted tweets require login and are not available.",
	})
}
