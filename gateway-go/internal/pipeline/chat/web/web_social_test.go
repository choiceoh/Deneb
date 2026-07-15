package web

import (
	"strings"
	"testing"
)

func TestIsRedditURL(t *testing.T) {
	cases := map[string]bool{
		"https://www.reddit.com/r/golang/comments/abc/title/": true,
		"https://reddit.com/r/golang":                         true,
		"https://old.reddit.com/r/golang/":                    true,
		"https://np.reddit.com/r/golang/":                     true,
		"https://notreddit.com/r/golang":                      false,
		"https://reddit.com.evil.com/x":                       false,
		"https://youtube.com/watch?v=x":                       false,
	}
	for u, want := range cases {
		if got := isRedditURL(u); got != want {
			t.Errorf("isRedditURL(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestRedditJSONURL(t *testing.T) {
	cases := map[string]string{
		"https://www.reddit.com/r/golang/comments/abc/title/": "https://www.reddit.com/r/golang/comments/abc/title.json?raw_json=1",
		"https://www.reddit.com/r/golang":                     "https://www.reddit.com/r/golang.json?raw_json=1",
	}
	for in, want := range cases {
		got, err := redditJSONURL(in)
		if err != nil {
			t.Fatalf("redditJSONURL(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("redditJSONURL(%q) = %q, want %q", in, got, want)
		}
	}
	// search query is preserved
	got, _ := redditJSONURL("https://www.reddit.com/r/golang/search?q=generics&restrict_sr=1")
	if !strings.Contains(got, "q=generics") || !strings.HasPrefix(got, "https://www.reddit.com/r/golang/search.json?") {
		t.Errorf("search rewrite lost query or path: %q", got)
	}
}

func TestRenderRedditThread(t *testing.T) {
	body := []byte(`[
      {"kind":"Listing","data":{"children":[
        {"kind":"t3","data":{"subreddit":"golang","title":"Go 1.99 released","author":"gopher","selftext":"Big news today.","score":1234,"num_comments":2,"is_self":true,"created_utc":1700000000}}
      ]}},
      {"kind":"Listing","data":{"children":[
        {"kind":"t1","data":{"author":"alice","body":"Great!","score":42,"replies":{"kind":"Listing","data":{"children":[
          {"kind":"t1","data":{"author":"bob","body":"Agreed","score":7,"replies":""}}
        ]}}}},
        {"kind":"more","data":{"count":5}}
      ]}}
    ]`)
	out := renderReddit(body, "https://www.reddit.com/r/golang/comments/abc/go_199", 20000)
	for _, want := range []string{"Go 1.99 released", "r/golang", "u/gopher", "Big news today", "u/alice", "u/bob", "Agreed"} {
		if !strings.Contains(out, want) {
			t.Errorf("thread render missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, `"count"`) { // "more" placeholder must be skipped
		t.Errorf("thread render leaked 'more' placeholder:\n%s", out)
	}
	// nested reply must be indented
	if !strings.Contains(out, "  - u/bob") {
		t.Errorf("nested reply not indented:\n%s", out)
	}
}

func TestRenderRedditListing(t *testing.T) {
	body := []byte(`{"kind":"Listing","data":{"children":[
      {"kind":"t3","data":{"subreddit":"golang","title":"First post","author":"a","score":10,"num_comments":3,"permalink":"/r/golang/comments/1/first/"}},
      {"kind":"t3","data":{"subreddit":"golang","title":"Second post","author":"b","score":20,"num_comments":5,"permalink":"/r/golang/comments/2/second/"}}
    ]}}`)
	out := renderReddit(body, "https://www.reddit.com/r/golang", 20000)
	for _, want := range []string{"First post", "Second post", "https://www.reddit.com/r/golang/comments/1/first/"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing render missing %q\n---\n%s", want, out)
		}
	}
}

func TestIsXStatusURL(t *testing.T) {
	cases := []struct {
		url    string
		id     string
		wantOK bool
	}{
		{"https://x.com/jack/status/20", "20", true},
		{"https://twitter.com/jack/status/1349129669258448897", "1349129669258448897", true},
		{"https://mobile.twitter.com/user/statuses/12345", "12345", true},
		{"https://x.com/jack", "", false},
		{"https://example.com/user/status/1", "", false},
	}
	for _, c := range cases {
		id, ok := isXStatusURL(c.url)
		if ok != c.wantOK || id != c.id {
			t.Errorf("isXStatusURL(%q) = (%q,%v), want (%q,%v)", c.url, id, ok, c.id, c.wantOK)
		}
	}
}

func TestSyndicationTokenMatchesJS(t *testing.T) {
	// Oracle from Node: ((Number(id)/1e15)*Math.PI).toString(36).replace(/(0+|\.)/g,'').
	// The base-36 rendering must match JS shortest-round-trip exactly or the
	// syndication endpoint rejects the token.
	if got := syndicationToken("1349129669258448897"); got != "39qeyy97t9x" {
		t.Errorf("syndicationToken = %q, want %q (JS oracle)", got, "39qeyy97t9x")
	}
	if strings.ContainsAny(syndicationToken("1349129669258448897"), "0.") {
		t.Error("token must not contain '0' or '.'")
	}
	if syndicationToken("20") == syndicationToken("1349129669258448897") {
		t.Error("distinct IDs produced identical tokens")
	}
	if syndicationToken("notanumber") != "" {
		t.Error("non-numeric ID should yield empty token")
	}
}

func TestReddItShortLink(t *testing.T) {
	if !isRedditURL("https://redd.it/abc123") {
		t.Error("redd.it short link should be recognized")
	}
	if isRedditURL("https://i.redd.it/photo.jpg") {
		t.Error("i.redd.it media CDN must NOT be treated as a post")
	}
	got, err := redditJSONURL("https://redd.it/abc123")
	if err != nil {
		t.Fatalf("redditJSONURL(redd.it) error: %v", err)
	}
	if got != "https://www.reddit.com/comments/abc123.json?raw_json=1" {
		t.Errorf("redd.it rewrite = %q", got)
	}
}

func TestSupportedSocialFetchURLExemption(t *testing.T) {
	// Supported social URLs must be exempt from the fetch denylist so
	// search+fetch can reach the native handlers.
	for _, u := range []string{
		"https://www.reddit.com/r/golang/comments/1/x/",
		"https://x.com/jack/status/20",
		"https://redd.it/abc123",
	} {
		if !isSupportedSocialFetchURL(u) {
			t.Errorf("%q should be a supported social fetch URL", u)
		}
	}
	// An X profile (non-status) is NOT supported and stays denied.
	if isSupportedSocialFetchURL("https://x.com/jack") {
		t.Error("X profile URL should not be exempted")
	}
}

func TestEnvelopeTagNeutralization(t *testing.T) {
	// A Reddit post whose title/body contains "<error>" must not be misclassified
	// as a fetch-error envelope (assessFetchResult flags any "<error>" substring).
	body := []byte(`[
      {"kind":"Listing","data":{"children":[
        {"kind":"t3","data":{"subreddit":"go","title":"parsing <error> tags","author":"a","selftext":"see <error>foo</error>","score":1,"num_comments":0,"is_self":true}}
      ]}},
      {"kind":"Listing","data":{"children":[]}}
    ]`)
	out := renderReddit(body, "https://www.reddit.com/r/go/comments/1/x", 20000)
	if strings.Contains(out, "<error>") {
		t.Errorf("rendered Reddit content leaked a literal <error> tag:\n%s", out)
	}
	if !isUsableFetchContent(out) {
		t.Errorf("neutralized Reddit content wrongly marked unusable:\n%s", out)
	}

	// Same for a tweet body containing "<error>".
	var b strings.Builder
	b.WriteString("<content>\nSource: https://x.com/a/status/1 (x/twitter)\n\n")
	writeXTweet(&b, &xTweet{Text: "try <error> in your code", User: xUser{Name: "dev", ScreenName: "dev"}}, "")
	b.WriteString("\n</content>")
	if strings.Contains(b.String(), "<error>") {
		t.Errorf("rendered tweet leaked a literal <error> tag:\n%s", b.String())
	}
	if !isUsableFetchContent(b.String()) {
		t.Errorf("neutralized tweet content wrongly marked unusable:\n%s", b.String())
	}
}

func TestWriteXTweetRender(t *testing.T) {
	var b strings.Builder
	tw := &xTweet{
		Text:      "just setting up my twttr",
		CreatedAt: "2006-03-21T20:50:14.000Z",
		User:      xUser{Name: "jack", ScreenName: "jack"},
		Favorites: 100, Replies: 50,
	}
	writeXTweet(&b, tw, "")
	out := b.String()
	for _, want := range []string{"jack (@jack)", "just setting up my twttr", "♥100", "💬50"} {
		if !strings.Contains(out, want) {
			t.Errorf("x render missing %q\n---\n%s", want, out)
		}
	}
}

func TestXUnavailableEnvelope(t *testing.T) {
	out := xUnavailable("https://x.com/user/status/1", "tweet is deleted, protected, or age-restricted")
	if !strings.Contains(out, "x_unavailable") || !strings.Contains(out, "without authentication") {
		t.Errorf("unavailable envelope malformed:\n%s", out)
	}
}

func TestCollapseWS(t *testing.T) {
	if got := collapseWS("  a\n\n b\t c  ", 0); got != "a b c" {
		t.Errorf("collapseWS = %q", got)
	}
	if got := collapseWS("abcdef", 3); got != "abc…" {
		t.Errorf("collapseWS truncate = %q", got)
	}
}
