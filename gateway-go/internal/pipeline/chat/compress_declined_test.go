package chat

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

// The system prompt advertises `compress: true` for "any tool input", but seven
// tools are on the skip list. A silent decline reads as "compressed" and the
// caller keeps a full-size result it believes is small.
func TestCompressDeclineOnSkipListIsAnnounced(t *testing.T) {
	big := strings.Repeat("가", compressThreshold+100)

	got := compressToolOutput(context.Background(), "wiki", big, "sp_abc", quietChatLogger())

	if !strings.HasPrefix(got, big) {
		t.Fatal("the original output must come back untouched")
	}
	if !strings.Contains(got, "미적용") || !strings.Contains(got, "wiki") {
		t.Fatalf("the decline must be announced: %q", got[len(big):])
	}
	if !strings.Contains(got, "sp_abc") {
		t.Fatalf("an existing spill handle should ride along: %q", got[len(big):])
	}
}

func TestSmallOutputDeclineStaysSilent(t *testing.T) {
	small := "짧은 결과"

	if got := compressToolOutput(context.Background(), "wiki", small, "", quietChatLogger()); got != small {
		t.Fatalf("below the threshold nothing was lost, so nothing is said: %q", got)
	}
}

func quietChatLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, nil))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
