package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// TestYouTubeASR_Live exercises the full caption-less fallback chain on the host:
// download a capped audio clip from a real YouTube URL, transcribe it via the
// VibeVoice-ASR sidecar through the wired media.AudioTranscriber. Skipped in CI
// (no GPU/network). Run on the DGX host:
//
//	DENEB_YT_ASR_LIVE=1 DENEB_YT_ASR_URL=https://youtu.be/<id> \
//	  go test -run TestYouTubeASR_Live -timeout 300s ./internal/pipeline/chat/tools/
func TestYouTubeASR_Live(t *testing.T) {
	if os.Getenv("DENEB_YT_ASR_LIVE") == "" {
		t.Skip("set DENEB_YT_ASR_LIVE=1 (+ DENEB_YT_ASR_URL) to run against the live sidecar")
	}
	url := os.Getenv("DENEB_YT_ASR_URL")
	if url == "" {
		t.Skip("DENEB_YT_ASR_URL not set")
	}
	if media.AudioTranscriber == nil {
		t.Fatal("media.AudioTranscriber not wired (asr_youtube_wire.go init should set it)")
	}
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 280*time.Second)
	defer cancel()

	tmp := t.TempDir()
	audioPath, err := media.DownloadYouTubeAudio(ctx, ytdlp, url, tmp, 0, 60)
	if err != nil {
		t.Fatalf("audio download failed: %v", err)
	}
	t.Logf("audio: %s", audioPath)

	text, err := media.AudioTranscriber(ctx, audioPath)
	if err != nil {
		t.Fatalf("ASR transcription failed: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("ASR returned empty transcript")
	}
	t.Logf("transcript (%d chars): %.200s", len(text), text)
}
