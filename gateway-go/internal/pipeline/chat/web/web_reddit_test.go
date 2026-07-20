package web

import (
	"context"
	"errors"
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
