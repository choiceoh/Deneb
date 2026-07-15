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

func TestSyndicationTokenDeterministic(t *testing.T) {
	// Token must be stable and contain no '0' or '.' characters (per react-tweet).
	tok := syndicationToken("1349129669258448897")
	if tok == "" {
		t.Fatal("empty token")
	}
	if strings.ContainsAny(tok, "0.") {
		t.Errorf("token contains stripped chars: %q", tok)
	}
	if got := syndicationToken("1349129669258448897"); got != tok {
		t.Errorf("token not deterministic: %q vs %q", tok, got)
	}
	// Different IDs should generally yield different tokens.
	if syndicationToken("20") == tok {
		t.Errorf("distinct IDs produced identical tokens")
	}
	if syndicationToken("notanumber") != "" {
		t.Errorf("non-numeric ID should yield empty token")
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
