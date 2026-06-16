package mailarchive

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestReader_Live exercises ListSince + Search against a real archive. Gated.
func TestReader_Live(t *testing.T) {
	if os.Getenv("DENEB_ARCHIVE_IMAP_LIVE") == "" {
		t.Skip("set DENEB_ARCHIVE_IMAP_LIVE=1 to run against a live archive IMAP")
	}
	cfg := Config{
		Addr: envOr("DENEB_ARCHIVE_IMAP_ADDR", "127.0.0.1:1143"),
		User: os.Getenv("DENEB_ARCHIVE_IMAP_USER"),
		Pass: os.Getenv("DENEB_ARCHIVE_IMAP_PASS"),
	}
	ctx := context.Background()

	list, err := ListSince(ctx, cfg, "Gmail", time.Now().AddDate(0, 0, -120), 10)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	t.Logf("ListSince(120d) Gmail: %d summaries", len(list))
	if len(list) > 0 {
		t.Logf("newest: from=%q subj=%q snippet=%.60q", list[0].From, list[0].Subject, list[0].Snippet)
	}

	res, err := Search(ctx, cfg, "Gmail", "ZTT", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	t.Logf("Search 'ZTT' Gmail: %d matches", len(res))
}
