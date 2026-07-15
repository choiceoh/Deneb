package chat

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/linkenrichment"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func TestStartLinkEnrichmentPreservesChatEligibilityGates(t *testing.T) {
	engine := linkenrichment.New(linkenrichment.Config{
		Fetch: func(context.Context, string) ([]byte, string, error) {
			return []byte("body"), "text/plain", nil
		},
		Logger: testLinkLogger(),
	})
	message := "see https://example.com"
	tests := []struct {
		name    string
		handler *Handler
		opts    *SyncOptions
	}{
		{
			name:    "briefcase mode",
			handler: &Handler{logger: testLinkLogger(), briefcaseMode: true, linkEnrichStart: func(ctx context.Context, message string, sanitize func(string) string) func(context.Context) string { return engine.Start(ctx, message, sanitize) }},
		},
		{
			name:    "caller-owned history",
			handler: &Handler{logger: testLinkLogger(), linkEnrichStart: func(ctx context.Context, message string, sanitize func(string) string) func(context.Context) string { return engine.Start(ctx, message, sanitize) }},
			opts:    &SyncOptions{Messages: []llm.Message{llm.NewTextMessage("user", "hi")}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if join := tt.handler.startLinkEnrichment(context.Background(), message, tt.opts); join != nil {
				t.Fatal("ineligible turn started link enrichment")
			}
		})
	}
}

func TestStartLinkEnrichmentUsesHandlerEngineAndChatSanitizer(t *testing.T) {
	engine := linkenrichment.New(linkenrichment.Config{
		Fetch: func(context.Context, string) ([]byte, string, error) {
			return []byte("Fetched\x00 body"), "text/plain", nil
		},
		Logger: testLinkLogger(),
	})
	handler := &Handler{logger: testLinkLogger(), linkEnrichStart: func(ctx context.Context, message string, sanitize func(string) string) func(context.Context) string { return engine.Start(ctx, message, sanitize) }}
	const typed = "이 링크 요약해줘 https://example.com/page"
	join := handler.startLinkEnrichment(context.Background(), typed, nil)
	if join == nil {
		t.Fatal("eligible turn did not start link enrichment")
	}
	got := join(context.Background())
	if !strings.HasPrefix(got, typed) || !strings.Contains(got, toolport.LinkEnrichmentHeader) || !strings.Contains(got, "Fetched body") {
		t.Fatalf("joined message lost enrichment contract: %q", got)
	}
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("fetched control byte escaped chat sanitizer: %q", got)
	}
}

func TestNewHandlerCreatesLinkEnrichmentEngine(t *testing.T) {
	handler := NewHandler(session.NewManager(), nil, testLinkLogger(), DefaultHandlerConfig())
	t.Cleanup(handler.Close)
	if handler.linkEnrichStart == nil {
		t.Fatal("NewHandler did not initialize its link enrichment lifecycle")
	}
	if join := handler.startLinkEnrichment(context.Background(), "링크 없는 일반 질문", nil); join != nil {
		t.Fatal("linkless message did not keep the normal persist-first path")
	}
}

// The enriched message must round-trip: what the enrichment join appends, the
// display strip removes so the user bubble shows only the typed text.
func TestLinkEnrichmentDisplayRoundTrip(t *testing.T) {
	engine := linkenrichment.New(linkenrichment.Config{
		Fetch: func(context.Context, string) ([]byte, string, error) {
			return []byte("Hello world"), "text/plain", nil
		},
		Logger: testLinkLogger(),
	})
	handler := &Handler{logger: testLinkLogger(), linkEnrichStart: func(ctx context.Context, message string, sanitize func(string) string) func(context.Context) string { return engine.Start(ctx, message, sanitize) }}
	const typed = "이 링크 요약해줘 https://example.com"
	join := handler.startLinkEnrichment(context.Background(), typed, nil)
	if join == nil {
		t.Fatal("eligible turn did not start link enrichment")
	}
	messages := []ChatMessage{
		toolport.NewTextChatMessage("user", join(context.Background()), 0),
		toolport.NewTextChatMessage("assistant", "요약입니다.", 0),
	}
	got := toolport.StripLinkEnrichmentForDisplay(messages)
	if got[0].TextContent() != typed || got[1].TextContent() != "요약입니다." {
		t.Fatalf("display round-trip = %+v, want typed user text and unchanged assistant", got)
	}
}

func testLinkLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
