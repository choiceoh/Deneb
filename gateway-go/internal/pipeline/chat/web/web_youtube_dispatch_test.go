package web

import (
	"context"
	"strings"
	"testing"
)

// The web tool no longer fetches YouTube — it steers the model to the watch
// tool (the single YouTube entry point). This short-circuits before any network,
// so no fetch/summary runs.
func TestWebFetch_YouTubeURLRedirectsToWatch(t *testing.T) {
	cache := NewFetchCache()
	out, err := webFetchURLDetailed(context.Background(), cache, nil, nil, "https://youtu.be/9H3aTCCNM1M", 0, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out.Content, "watch") {
		t.Errorf("a YouTube URL should steer to the watch tool, got: %q", out.Content)
	}
}
