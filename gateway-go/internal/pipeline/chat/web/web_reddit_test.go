package web

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// fetchRedditViaBrowser is the .json-block fallback: it renders the HTML Reddit
// URL through the injectable browser sidecar and reports whether it recovered
// readable content. These cases pin the recover/decline contract the caller in
// fetchReddit relies on to decide between rendered text and the block error.
func TestFetchRedditViaBrowser(t *testing.T) {
	const rawURL = "https://www.reddit.com/r/LocalLLaMA/comments/abc123/some_thread/"

	t.Run("recovers rendered text on success", func(t *testing.T) {
		orig := browserRenderFn
		var gotURL string
		browserRenderFn = func(_ context.Context, url string, _ int64) (*media.FetchResult, error) {
			gotURL = url
			return &media.FetchResult{Data: []byte("  rendered reddit thread body  ")}, nil
		}
		t.Cleanup(func() { browserRenderFn = orig })

		text, ok := fetchRedditViaBrowser(context.Background(), rawURL, 20000)
		if !ok {
			t.Fatal("expected ok=true on a non-empty render")
		}
		if text != "rendered reddit thread body" {
			t.Fatalf("text not trimmed/returned: %q", text)
		}
		// The browser must render the original HTML URL, never the .json variant.
		if gotURL != rawURL {
			t.Fatalf("browser rendered %q, want the original HTML URL %q", gotURL, rawURL)
		}
	})

	t.Run("declines when sidecar errors", func(t *testing.T) {
		orig := browserRenderFn
		browserRenderFn = func(context.Context, string, int64) (*media.FetchResult, error) {
			return nil, errors.New("browser sidecar unreachable")
		}
		t.Cleanup(func() { browserRenderFn = orig })

		if _, ok := fetchRedditViaBrowser(context.Background(), rawURL, 20000); ok {
			t.Fatal("expected ok=false when the sidecar errors")
		}
	})

	t.Run("declines on empty render", func(t *testing.T) {
		orig := browserRenderFn
		browserRenderFn = func(context.Context, string, int64) (*media.FetchResult, error) {
			return &media.FetchResult{Data: []byte("   \n  ")}, nil
		}
		t.Cleanup(func() { browserRenderFn = orig })

		if _, ok := fetchRedditViaBrowser(context.Background(), rawURL, 20000); ok {
			t.Fatal("expected ok=false when the render is blank")
		}
	})
}

// old.reddit.com is the first escalation when the .json API blocks the read:
// server-rendered HTML, no JS challenge, always English, comment tree inline.
// Measured 2026-07-25 against a live thread the API 403'd — 92K chars with the
// full comment tree, vs 1.4K of Korean-localized chrome from the sidecar.
func TestOldRedditURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			"www host is rewritten, path and query preserved",
			"https://www.reddit.com/r/LocalLLaMA/comments/abc/x/?sort=top",
			"https://old.reddit.com/r/LocalLLaMA/comments/abc/x/?sort=top", true,
		},
		{
			"bare host is rewritten", "https://reddit.com/r/golang/",
			"https://old.reddit.com/r/golang/", true,
		},
		{
			"already old.reddit is declined (no re-fetch loop)",
			"https://old.reddit.com/r/golang/", "", false,
		},
		{"unparseable url is declined", "://nope", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := oldRedditURL(c.in)
			if ok != c.ok || got != c.want {
				t.Fatalf("oldRedditURL(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestFetchRedditViaOldHTML(t *testing.T) {
	const rawURL = "https://www.reddit.com/r/LocalLLaMA/comments/abc123/some_thread/"

	t.Run("extracts readable text and targets old.reddit", func(t *testing.T) {
		orig := oldRedditFetchFn
		t.Cleanup(func() { oldRedditFetchFn = orig })
		var gotURL string
		oldRedditFetchFn = func(_ context.Context, u string, _ int64) (*media.FetchResult, error) {
			gotURL = u
			return &media.FetchResult{
				Data:        []byte("<html><head><title>스레드</title></head><body><div class=\"md\"><p>real discussion body</p></div></body></html>"),
				ContentType: "text/html",
			}, nil
		}
		out, ok := fetchRedditViaOldHTML(context.Background(), rawURL, 20000)
		if !ok {
			t.Fatal("expected a successful old.reddit read")
		}
		if gotURL != "https://old.reddit.com/r/LocalLLaMA/comments/abc123/some_thread/" {
			t.Fatalf("fetched %q, want the old.reddit rewrite", gotURL)
		}
		if !strings.Contains(out, "real discussion body") {
			t.Fatalf("post body missing from extraction: %q", out)
		}
	})

	t.Run("declines on fetch error so the browser tier still runs", func(t *testing.T) {
		orig := oldRedditFetchFn
		t.Cleanup(func() { oldRedditFetchFn = orig })
		oldRedditFetchFn = func(context.Context, string, int64) (*media.FetchResult, error) {
			return nil, errors.New("blocked")
		}
		if _, ok := fetchRedditViaOldHTML(context.Background(), rawURL, 20000); ok {
			t.Fatal("a fetch error must decline, not report success")
		}
	})

	t.Run("declines on empty body", func(t *testing.T) {
		orig := oldRedditFetchFn
		t.Cleanup(func() { oldRedditFetchFn = orig })
		oldRedditFetchFn = func(context.Context, string, int64) (*media.FetchResult, error) {
			return &media.FetchResult{Data: nil, ContentType: "text/html"}, nil
		}
		if _, ok := fetchRedditViaOldHTML(context.Background(), rawURL, 20000); ok {
			t.Fatal("an empty body must decline")
		}
	})
}
