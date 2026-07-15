package web

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Live smoke for the speed-improvement paths. Opt-in: DENEB_WEB_LIVE=1.
func TestLiveWebFetchPaths(t *testing.T) {
	if os.Getenv("DENEB_WEB_LIVE") != "1" {
		t.Skip("set DENEB_WEB_LIVE=1 for live network smoke")
	}

	cache := NewFetchCache()
	localAI := NewLocalAIExtractor()
	tool := Tool(cache, localAI, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 1) Normal HTML (Serper scrape when key present, else stealth).
	out, err := tool(ctx, []byte(`{"url":"https://example.com/","maxChars":3000}`))
	if err != nil {
		t.Fatalf("example.com: %v", err)
	}
	if !strings.Contains(out, "FetchMs:") || !strings.Contains(out, "Provider:") {
		t.Fatalf("example.com missing latency metadata:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "example") {
		t.Fatalf("example.com unexpected body:\n%s", out)
	}
	t.Logf("example.com ok provider present; len=%d", len(out))

	// Cache hit should succeed without error.
	out2, err := tool(ctx, []byte(`{"url":"https://example.com/","maxChars":3000}`))
	if err != nil || out2 == "" {
		t.Fatalf("cache hit failed: err=%v len=%d", err, len(out2))
	}

	// 2) Larger HTML page — exercise stealth/serper with a higher byte budget.
	out3, err := tool(ctx, []byte(`{"url":"https://www.iana.org/domains/reserved","maxChars":50000}`))
	if err != nil {
		t.Fatalf("iana reserved: %v", err)
	}
	if strings.Contains(out3, "<error>") {
		t.Fatalf("iana reserved errored:\n%s", out3)
	}
	t.Logf("iana ok len=%d provider=%s", len(out3), extractMetaLine(out3, "Provider: "))

	// 3) SPA-ish page: early-Jina / thin escalate may engage on stealth;
	//    Serper scrape usually returns clean markdown under budget.
	out4, err := tool(ctx, []byte(`{"url":"https://react.dev/","maxChars":200000}`))
	if err != nil {
		t.Fatalf("react.dev: %v", err)
	}
	if strings.Contains(out4, "<error>") && !strings.Contains(out4, "<content>") {
		t.Fatalf("react.dev hard error:\n%s", out4)
	}
	t.Logf("react.dev ok len=%d provider=%s signals=%s", len(out4), extractMetaLine(out4, "Provider: "), extractMetaLine(out4, "Signals: "))
}

func extractMetaLine(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}
