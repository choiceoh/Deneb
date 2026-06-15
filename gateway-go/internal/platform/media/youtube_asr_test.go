package media

import (
	"context"
	"strings"
	"testing"
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

	text, lang := transcriptViaASR(context.Background(), "yt-dlp", "https://youtu.be/x", t.TempDir())
	if text != "" || lang != "" {
		t.Errorf("expected empty no-op, got text=%q lang=%q", text, lang)
	}
}
