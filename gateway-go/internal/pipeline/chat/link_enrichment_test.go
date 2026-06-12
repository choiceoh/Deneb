package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// stubFetcher returns a fetchFunc that serves canned responses keyed by URL.
func stubFetcher(responses map[string]struct {
	data        []byte
	contentType string
	err         error
}) fetchFunc {
	return func(_ context.Context, url string) ([]byte, string, error) {
		resp, ok := responses[url]
		if !ok {
			return nil, "", fmt.Errorf("not found: %s", url)
		}
		return resp.data, resp.contentType, resp.err
	}
}

func TestEnrichMessageWithLinks_NoLinks(t *testing.T) {
	logger := slog.Default()
	result := enrichMessageWithLinks(context.Background(), "hello world", nil, logger)
	if result != "" {
		t.Fatalf("expected empty string for message without links, got: %q", result)
	}
}

func TestEnrichMessageWithLinks_SingleHTMLLink(t *testing.T) {
	logger := slog.Default()
	html := `<html><head><title>Example</title></head><body><p>Hello world</p></body></html>`

	fetch := stubFetcher(map[string]struct {
		data        []byte
		contentType string
		err         error
	}{
		"https://example.com": {[]byte(html), "text/html; charset=utf-8", nil},
	})

	result := enrichMessageWithLinks(context.Background(), "check https://example.com out", fetch, logger)

	if !strings.Contains(result, toolctx.LinkEnrichmentHeader) {
		t.Fatal("expected link summary header")
	}
	if !strings.Contains(result, "Example") {
		t.Fatal("expected title in output")
	}
	if !strings.Contains(result, "https://example.com") {
		t.Fatal("expected URL in output")
	}
}

func TestEnrichMessageWithLinks_FetchFailure(t *testing.T) {
	logger := slog.Default()
	fetch := stubFetcher(map[string]struct {
		data        []byte
		contentType string
		err         error
	}{
		"https://example.com": {nil, "", fmt.Errorf("connection refused")},
	})

	result := enrichMessageWithLinks(context.Background(), "https://example.com", fetch, logger)

	if result != "" {
		t.Fatalf("expected empty string when all fetches fail, got: %q", result)
	}
}

func TestEnrichMessageWithLinks_Truncation(t *testing.T) {
	logger := slog.Default()
	longContent := strings.Repeat("a", maxCharsPerLink+1000)

	fetch := stubFetcher(map[string]struct {
		data        []byte
		contentType string
		err         error
	}{
		"https://example.com": {[]byte(longContent), "text/plain", nil},
	})

	result := enrichMessageWithLinks(context.Background(), "https://example.com", fetch, logger)

	if !strings.Contains(result, "[...truncated]") {
		t.Fatal("expected truncation marker")
	}
}

func TestFormatLinkSummary_WithTitle(t *testing.T) {
	links := []linkContent{
		{URL: "https://example.com", Title: "Example Page", Content: "Hello world"},
	}
	result := formatLinkSummary(links)

	if !strings.Contains(result, "[Example Page](https://example.com)") {
		t.Fatal("expected markdown link with title")
	}
	if !strings.Contains(result, "Hello world") {
		t.Fatal("expected content")
	}
}

// The enriched message must round-trip: what maybeEnrichLinks appends, the
// History display strip removes — so the user bubble shows the typed text.
func TestStripLinkEnrichmentForDisplay_RoundTrip(t *testing.T) {
	typed := "이 링크 요약해줘 https://example.com"
	enriched := typed + "\n\n" + formatLinkSummary([]linkContent{
		{URL: "https://example.com", Title: "Example", Content: "Hello world"},
	})

	msgs := []ChatMessage{
		toolctx.NewTextChatMessage("user", enriched, 0),
		toolctx.NewTextChatMessage("assistant", "요약입니다.", 0),
	}
	out := toolctx.StripLinkEnrichmentForDisplay(msgs)

	if got := out[0].TextContent(); got != typed {
		t.Fatalf("user message not stripped to typed text:\ngot:  %q\nwant: %q", got, typed)
	}
	if got := out[1].TextContent(); got != "요약입니다." {
		t.Fatalf("assistant message must be untouched, got: %q", got)
	}
}

// A message that already carries an enrichment block must not be enriched
// again (idempotence guard in maybeEnrichLinks).
func TestMaybeEnrichLinks_AlreadyEnriched(t *testing.T) {
	h := &Handler{logger: slog.Default()}
	enriched := "see https://example.com\n\n---\n" + toolctx.LinkEnrichmentHeader + "\n\nstuff\n---"
	if got := h.maybeEnrichLinks(context.Background(), enriched, nil); got != enriched {
		t.Fatal("already-enriched message must pass through unchanged")
	}
}

// API traffic with caller-owned history is never enriched.
func TestMaybeEnrichLinks_SkipsPrebuiltMessages(t *testing.T) {
	h := &Handler{logger: slog.Default()}
	msg := "see https://example.com"
	opts := &SyncOptions{Messages: []llm.Message{llm.NewTextMessage("user", "hi")}}
	if got := h.maybeEnrichLinks(context.Background(), msg, opts); got != msg {
		t.Fatal("prebuilt-history turn must pass through unchanged")
	}
}
