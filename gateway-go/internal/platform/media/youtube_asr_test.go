package media

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestYtPlayerClientArgs(t *testing.T) {
	// Default is empty — forcing a player client broke audio downloads and gave
	// no caption benefit, so the default path uses yt-dlp's own client choice.
	t.Setenv("DENEB_YTDLP_PLAYER_CLIENT", "")
	if got := ytPlayerClientArgs(); got != nil {
		t.Errorf("default args = %v, want nil", got)
	}

	t.Setenv("DENEB_YTDLP_PLAYER_CLIENT", "ios")
	got := strings.Join(ytPlayerClientArgs(), " ")
	if got != "--extractor-args youtube:player_client=ios" {
		t.Errorf("override args = %q", got)
	}
}

func TestAsrAudioCapSec(t *testing.T) {
	t.Setenv("DENEB_YT_ASR_CAP_SEC", "")
	if got := asrAudioCapSec(); got != 600 {
		t.Errorf("default cap = %d, want 600", got)
	}
	t.Setenv("DENEB_YT_ASR_CAP_SEC", "300")
	if got := asrAudioCapSec(); got != 300 {
		t.Errorf("override cap = %d, want 300", got)
	}
	t.Setenv("DENEB_YT_ASR_CAP_SEC", "garbage")
	if got := asrAudioCapSec(); got != 600 {
		t.Errorf("bad value should fall back to 600, got %d", got)
	}
}

func TestTranscriptViaASR_NoTranscriberIsNoop(t *testing.T) {
	// With no AudioTranscriber wired, the fallback must be a clean no-op (never
	// attempt a download) so captions-only deployments behave as before.
	prev := AudioTranscriber
	AudioTranscriber = nil
	defer func() { AudioTranscriber = prev }()

	text, lang := transcriptViaASR(context.Background(), "yt-dlp", "https://youtu.be/x", t.TempDir(), 0, 0, 0)
	if text != "" || lang != "" {
		t.Errorf("expected empty no-op, got text=%q lang=%q", text, lang)
	}
}

func TestTranscriptViaASR_SkipsWhenSidecarUnavailable(t *testing.T) {
	// When the readiness probe reports the sidecar down, ASR must skip before any
	// audio download (no wasted fetch budget) and never call the transcriber.
	called := false
	prevT, prevR := AudioTranscriber, AudioTranscriberReady
	AudioTranscriber = func(_ context.Context, _ string) (string, error) {
		called = true
		return "should not be called", nil
	}
	AudioTranscriberReady = func(_ context.Context) bool { return false }
	defer func() { AudioTranscriber, AudioTranscriberReady = prevT, prevR }()

	text, lang := transcriptViaASR(context.Background(), "yt-dlp", "https://youtu.be/x", t.TempDir(), 0, 0, 600)
	if text != "" || lang != "" {
		t.Errorf("expected skip, got text=%q lang=%q", text, lang)
	}
	if called {
		t.Error("AudioTranscriber must not run when the sidecar is unready")
	}
}

func TestTranscriptViaASR_SkipsWhenDeadlineTooClose(t *testing.T) {
	// The web fetch path bounds ExtractYouTubeTranscript to a tight deadline. When
	// too little time remains, ASR must skip up front — never download/transcribe
	// only to blow the caller's budget and return nothing.
	called := false
	prev := AudioTranscriber
	AudioTranscriber = func(_ context.Context, _ string) (string, error) {
		called = true
		return "should not be called", nil
	}
	defer func() { AudioTranscriber = prev }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	text, lang := transcriptViaASR(ctx, "yt-dlp", "https://youtu.be/x", t.TempDir(), 0, 0, 600)
	if text != "" || lang != "" {
		t.Errorf("expected skip, got text=%q lang=%q", text, lang)
	}
	if called {
		t.Error("AudioTranscriber must not run when the deadline is too close")
	}
}
