package linkenrichment

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

func TestEngineEnrichMessageRendersNativeYouTubeContent(t *testing.T) {
	engine := testEngine(nil)
	engine.youtube = func(_ context.Context, url string) *media.YouTubeResult {
		return &media.YouTubeResult{
			Title:      "테스트 영상",
			Channel:    "테스트 채널",
			Transcript: "안녕하세요 영상 내용입니다",
			Language:   "ko",
			URL:        url,
			Chapters:   []media.YouTubeChapter{{StartSec: 0, Title: "인트로"}},
		}
	}

	result := engine.enrichMessage(context.Background(), "이거 봐 https://youtu.be/dQw4w9WgXcQ")
	for _, want := range []string{toolport.LinkEnrichmentHeader, "테스트 영상", "테스트 채널", "인트로", "안녕하세요 영상 내용입니다"} {
		if !strings.Contains(result, want) {
			t.Errorf("enriched output missing %q: %q", want, result)
		}
	}
}

func TestEngineEnrichMessageReturnsEmptyForUnavailableYouTube(t *testing.T) {
	engine := testEngine(nil)
	engine.youtube = func(context.Context, string) *media.YouTubeResult { return nil }
	if got := engine.enrichMessage(context.Background(), "https://youtu.be/dQw4w9WgXcQ"); got != "" {
		t.Fatalf("unavailable YouTube enrichment = %q, want empty", got)
	}
}

func TestEngineEnrichMessageConvertsHTMLAndTruncatesContent(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		kind     string
		contains []string
	}{
		{
			name:     "html metadata",
			body:     `<html><head><title>Example</title></head><body><p>Hello world</p></body></html>`,
			kind:     "text/html; charset=utf-8",
			contains: []string{"[Example](https://example.com)", "Hello world"},
		},
		{
			name:     "plain text budget",
			body:     strings.Repeat("a", maxCharsPerLink+1000),
			kind:     "text/plain",
			contains: []string{"[...truncated]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := testEngine(stubFetcher(map[string]fetchResponse{
				"https://example.com": {data: []byte(tt.body), contentType: tt.kind},
			}))
			got := engine.enrichMessage(context.Background(), "check https://example.com")
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("enriched output missing %q: %q", want, got)
				}
			}
		})
	}
}

func TestEngineEnrichMessageGracefullyDropsFailedOrPanickingFetches(t *testing.T) {
	tests := []struct {
		name  string
		fetch FetchFunc
	}{
		{
			name: "fetch error",
			fetch: func(context.Context, string) ([]byte, string, error) {
				return nil, "", context.DeadlineExceeded
			},
		},
		{
			name: "fetch panic",
			fetch: func(context.Context, string) ([]byte, string, error) {
				panic("broken fetcher")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := testEngine(tt.fetch)
			if got := engine.enrichMessage(context.Background(), "https://example.com"); got != "" {
				t.Fatalf("failed enrichment = %q, want empty", got)
			}
		})
	}
}

func TestEngineStartEligibilityAndJoinFallbacks(t *testing.T) {
	engine := testEngine(func(_ context.Context, _ string) ([]byte, string, error) {
		return []byte("Fetched body"), "text/plain", nil
	})
	if join := engine.Start(context.Background(), "링크 없는 일반 질문", identitySanitizer); join != nil {
		t.Fatal("linkless message started enrichment")
	}
	alreadyEnriched := "see https://example.com\n\n---\n" + toolport.LinkEnrichmentHeader + "\n\nstuff\n---"
	if join := engine.Start(context.Background(), alreadyEnriched, identitySanitizer); join != nil {
		t.Fatal("already-enriched message started enrichment")
	}

	const message = "이 링크 요약해줘 https://example.com/page"
	join := engine.Start(context.Background(), message, identitySanitizer)
	if join == nil {
		t.Fatal("linked message did not start enrichment")
	}
	got := join(context.Background())
	if !strings.HasPrefix(got, message) || !strings.Contains(got, toolport.LinkEnrichmentHeader) || !strings.Contains(got, "Fetched body") {
		t.Fatalf("joined message lost content contract: %q", got)
	}

	failing := testEngine(func(context.Context, string) ([]byte, string, error) {
		return nil, "", context.DeadlineExceeded
	})
	if got := failing.Start(context.Background(), message, identitySanitizer)(context.Background()); got != message {
		t.Fatalf("failed fetch join = %q, want original", got)
	}
}

func TestEngineStartCanceledJoinReturnsOriginal(t *testing.T) {
	fetchStarted := make(chan struct{})
	fetchCanceled := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	engine := testEngine(func(ctx context.Context, _ string) ([]byte, string, error) {
		close(fetchStarted)
		select {
		case <-release:
		case <-ctx.Done():
			close(fetchCanceled)
		}
		return nil, "", context.Canceled
	})
	const message = "느린 링크 https://example.com/slow"
	join := engine.Start(context.Background(), message, identitySanitizer)
	if join == nil {
		t.Fatal("linked message did not start enrichment")
	}
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("fetch did not start")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := join(canceled); got != message {
		t.Fatalf("canceled join = %q, want original", got)
	}
	select {
	case <-fetchCanceled:
	case <-time.After(time.Second):
		t.Fatal("canceled join did not cancel the fetch context")
	}
}

type fetchResponse struct {
	data        []byte
	contentType string
	err         error
}

func stubFetcher(responses map[string]fetchResponse) FetchFunc {
	return func(_ context.Context, url string) ([]byte, string, error) {
		response, ok := responses[url]
		if !ok {
			return nil, "", fmt.Errorf("not found: %s", url)
		}
		return response.data, response.contentType, response.err
	}
}

func testEngine(fetch FetchFunc) *Engine {
	return New(Config{
		Fetch:  fetch,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func identitySanitizer(text string) string { return text }
