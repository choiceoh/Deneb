package web

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

func TestShouldEscalateThinContent(t *testing.T) {
	tests := []struct {
		name string
		meta webFetchMeta
		want bool
	}{
		{
			"thin + js_required → escalate",
			webFetchMeta{ExtractChars: 10, Signals: []string{"js_required"}},
			true,
		},
		{
			"thin + empty_body → escalate",
			webFetchMeta{ExtractChars: 0, Signals: []string{"empty_body"}},
			true,
		},
		{
			"thin but no JS/empty signal → keep",
			webFetchMeta{ExtractChars: 10, Signals: []string{"cookie_consent"}},
			false,
		},
		{
			"rich content + js_required → keep (already got content)",
			webFetchMeta{ExtractChars: thinContentThreshold + 1, Signals: []string{"js_required"}},
			false,
		},
		{
			"thin + bot_blocked → keep (stealth handles bot walls, not us)",
			webFetchMeta{ExtractChars: 5, Signals: []string{"bot_blocked"}},
			false,
		},
		{
			"thin + no signals → keep",
			webFetchMeta{ExtractChars: 5},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldEscalateThinContent(&tt.meta); got != tt.want {
				t.Errorf("shouldEscalateThinContent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldEscalateThinContent_NilSafe(t *testing.T) {
	if shouldEscalateThinContent(nil) {
		t.Error("nil meta must not escalate")
	}
}

// withMockJina swaps the headless backend for the duration of a test, restoring
// the real one afterward, and returns a counter of how many times it was called.
func withMockJina(t *testing.T, fn func(ctx context.Context, url string, maxBytes int64) (*media.FetchResult, error)) *int32 {
	t.Helper()
	var calls int32
	orig := jinaFetchFn
	jinaFetchFn = func(ctx context.Context, url string, maxBytes int64) (*media.FetchResult, error) {
		atomic.AddInt32(&calls, 1)
		return fn(ctx, url, maxBytes)
	}
	t.Cleanup(func() { jinaFetchFn = orig })
	return &calls
}

func TestEscalateThinContent_AdoptsRicherResult(t *testing.T) {
	rendered := strings.Repeat("실제 본문 내용입니다. ", 200) // well over the threshold
	calls := withMockJina(t, func(_ context.Context, _ string, _ int64) (*media.FetchResult, error) {
		return &media.FetchResult{
			Data:        []byte(rendered),
			ContentType: "text/plain",
			Size:        len(rendered),
		}, nil
	})

	meta := webFetchMeta{
		URL:          "https://spa.example.com/app",
		ExtractChars: 12, // thin original
		Signals:      []string{"js_required"},
	}
	content, ok := escalateThinContent(context.Background(), meta.URL, 1<<20, &LocalAIExtractor{}, &meta)
	if !ok {
		t.Fatal("expected escalation to succeed with richer content")
	}
	if !strings.Contains(content, "실제 본문") {
		t.Errorf("escalated content missing rendered body:\n%s", content[:min(200, len(content))])
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("headless backend called %d times, want exactly 1", got)
	}
	// Metadata must reflect the recovery.
	if meta.ExtractChars <= 12 {
		t.Errorf("ExtractChars not updated after escalation: %d", meta.ExtractChars)
	}
	if !containsStr(meta.Signals, "escalated_headless") {
		t.Errorf("missing escalated_headless signal: %v", meta.Signals)
	}
}

func TestEscalateThinContent_KeepsOriginalWhenBackendEmpty(t *testing.T) {
	calls := withMockJina(t, func(_ context.Context, _ string, _ int64) (*media.FetchResult, error) {
		return &media.FetchResult{Data: nil, ContentType: "text/plain"}, nil
	})

	meta := webFetchMeta{URL: "https://x.example/app", ExtractChars: 12, Signals: []string{"empty_body"}}
	content, ok := escalateThinContent(context.Background(), meta.URL, 1<<20, &LocalAIExtractor{}, &meta)
	if ok || content != "" {
		t.Fatalf("expected no escalation on empty backend result, got ok=%v content=%q", ok, content)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("backend called %d times, want 1", got)
	}
	if containsStr(meta.Signals, "escalated_headless") {
		t.Error("must not mark escalated when backend gave nothing")
	}
}

func TestEscalateThinContent_KeepsOriginalWhenBackendErrors(t *testing.T) {
	withMockJina(t, func(_ context.Context, _ string, _ int64) (*media.FetchResult, error) {
		return nil, errors.New("jina down")
	})
	meta := webFetchMeta{URL: "https://x.example/app", ExtractChars: 12, Signals: []string{"js_required"}}
	if _, ok := escalateThinContent(context.Background(), meta.URL, 1<<20, &LocalAIExtractor{}, &meta); ok {
		t.Fatal("backend error must not yield escalation")
	}
}

func TestEscalateThinContent_KeepsOriginalWhenNotRicher(t *testing.T) {
	// Backend returns text shorter than the original extraction → reject.
	calls := withMockJina(t, func(_ context.Context, _ string, _ int64) (*media.FetchResult, error) {
		return &media.FetchResult{Data: []byte("tiny"), ContentType: "text/plain"}, nil
	})
	meta := webFetchMeta{URL: "https://x.example/app", ExtractChars: 100, Signals: []string{"js_required"}}
	if _, ok := escalateThinContent(context.Background(), meta.URL, 1<<20, &LocalAIExtractor{}, &meta); ok {
		t.Fatal("must not adopt a result that is not strictly richer")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("backend called %d times, want 1", got)
	}
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
