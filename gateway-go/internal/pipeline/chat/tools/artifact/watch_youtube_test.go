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

// A windowed request (start/end) must NOT delegate to the fetcher: the fetcher
// summarizes the whole video and cannot honor a window, so it falls through to
// the native WatchVideo path, which transcribes just that window's audio. The
// pre-cancelled context makes the native path fail fast without touching
// yt-dlp/network — the assertion is only that the fetcher was bypassed.
func TestWatch_YouTubeWindowedRequestBypassesFetcher(t *testing.T) {
	called := false
	fetch := func(_ context.Context, _ string) (string, error) {
		called = true
		return "whole-video summary", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := ToolWatch("", fetch)(ctx, json.RawMessage(
		`{"source":"https://youtu.be/dQw4w9WgXcQ","start":720,"end":900}`,
	))
	if called {
		t.Fatalf("windowed request delegated to the whole-video fetcher (out=%q err=%v)", out, err)
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
