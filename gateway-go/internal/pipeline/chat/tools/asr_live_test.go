package tools

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestTranscribeAudio_Live exercises the VibeVoice-ASR client end-to-end against
// a real running server (multipart upload, JSON parse, diarized formatting).
// Skipped in CI (no GPU); run on the host:
//
//	DENEB_ASR_LIVE=1 DENEB_ASR_AUDIO=/path/to.wav DENEB_ASR_URL=http://127.0.0.1:18013 \
//	  go test -run TestTranscribeAudio_Live ./internal/pipeline/chat/tools/
func TestTranscribeAudio_Live(t *testing.T) {
	if os.Getenv("DENEB_ASR_LIVE") != "1" {
		t.Skip("set DENEB_ASR_LIVE=1 to run against a live VibeVoice-ASR server")
	}
	path := os.Getenv("DENEB_ASR_AUDIO")
	if path == "" {
		t.Skip("set DENEB_ASR_AUDIO to an audio file path")
	}
	audio, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audio %q: %v", path, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := transcribeAudioText(ctx, audio, "audio/wav")
	if err != nil {
		t.Fatalf("transcribeAudioText: %v", err)
	}
	if out == "" {
		t.Fatal("empty transcript")
	}
	t.Logf("transcript:\n%s", out)
}
