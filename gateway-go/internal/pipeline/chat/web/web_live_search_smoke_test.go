package web

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveWebSearchSmoke(t *testing.T) {
	if os.Getenv("DENEB_WEB_LIVE") != "1" {
		t.Skip("set DENEB_WEB_LIVE=1 for live network smoke")
	}
	tool := Tool(NewFetchCache(), NewLocalAIExtractor(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := tool(ctx, []byte(`{"query":"Deneb star","count":3}`))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(out, "<error>") || !strings.Contains(out, "http") {
		t.Fatalf("search unexpected:\n%s", out)
	}
	t.Logf("search ok len=%d", len(out))

	out2, err := tool(ctx, []byte(`{"query":"example.com","count":5,"fetch":2,"maxChars":8000}`))
	if err != nil {
		t.Fatalf("search+fetch: %v", err)
	}
	if !strings.Contains(out2, "<fetched") {
		t.Fatalf("expected fetched pages:\n%s", out2)
	}
	t.Logf("search+fetch ok len=%d", len(out2))

	// Korean query exercises Serper gl/hl + candidate ranking (no social hosts).
	out3, err := tool(ctx, []byte(`{"query":"덴브 항성","count":5,"fetch":2,"maxChars":8000}`))
	if err != nil {
		t.Fatalf("ko search+fetch: %v", err)
	}
	if !strings.Contains(out3, "<fetched") {
		t.Fatalf("ko search+fetch expected fetched pages:\n%s", out3)
	}
	for _, block := range strings.Split(out3, "<fetched") {
		if !strings.Contains(block, `url="`) {
			continue
		}
		for _, banned := range []string{"facebook.com", "pinterest.com", "twitter.com", "x.com/"} {
			if strings.Contains(block, banned) {
				t.Fatalf("ko search+fetch fetched social host %s:\n%s", banned, out3)
			}
		}
	}
	t.Logf("ko search+fetch ok len=%d", len(out3))
}
