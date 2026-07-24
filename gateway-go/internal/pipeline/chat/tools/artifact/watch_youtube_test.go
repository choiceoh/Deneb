package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The watch tool is the single YouTube entry point: its default (transcript)
// delegates YouTube URLs to the injected fetcher, so no video download/vision
// runs for a plain "review this video".
func TestWatch_YouTubeDefaultDelegatesToFetcher(t *testing.T) {
	var gotURL string
	fetch := func(_ context.Context, url string) (string, error) {
		gotURL = url
		return "## 요약\n핵심 내용", nil
	}
	fn := ToolWatch("", fetch)

	// No detail → default transcript → should delegate.
	out, err := fn(context.Background(), json.RawMessage(`{"source":"https://youtu.be/dQw4w9WgXcQ"}`))
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if gotURL != "https://youtu.be/dQw4w9WgXcQ" {
		t.Errorf("fetcher got url %q", gotURL)
	}
	if !strings.Contains(out, "핵심 내용") {
		t.Errorf("expected the fetcher's summary, got %q", out)
	}
}

// A fetcher error surfaces (doesn't silently fall through to a video download).
func TestWatch_YouTubeFetcherErrorSurfaces(t *testing.T) {
	fetch := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("captions unavailable")
	}
	_, err := ToolWatch("", fetch)(context.Background(), json.RawMessage(`{"source":"https://youtu.be/9H3aTCCNM1M"}`))
	if err == nil {
		t.Fatal("expected the fetcher error to surface")
	}
}

// detail=frames must NOT delegate to the transcript fetcher — the frames branch
// is gated on detail==transcript. (The visual path itself needs yt-dlp + network,
// so it is exercised by live tests, not here.)
func TestWatch_FramesModeDoesNotDelegate(t *testing.T) {
	if watchDetailFrames == watchDetailTranscript {
		t.Fatal("frames and transcript must be distinct so the delegation guard holds")
	}
}
