package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// init wires the YouTube audio→ASR fallback: media.ExtractYouTubeTranscript and
// media.WatchVideo call media.AudioTranscriber when captions are unavailable
// (no captions, or YouTube's 429/no-JS block), transcribing the downloaded audio
// via the VibeVoice-ASR sidecar. Kept here rather than in the media package so
// that platform layer stays free of any chat/tools dependency — tools imports
// media, not the reverse, so this one-way injection avoids an import cycle.
func init() {
	media.AudioTranscriber = func(ctx context.Context, audioPath string) (string, error) {
		data, err := os.ReadFile(audioPath) //nolint:gosec // G703 — audioPath is a temp file produced by media.DownloadYouTubeAudio, not user input
		if err != nil {
			return "", err
		}
		return transcribeAudioText(ctx, data, audioMimeFromPath(audioPath), "")
	}
	// Readiness gate: media checks this before downloading audio, so when the
	// VibeVoice-ASR sidecar is down the fallback skips up front instead of
	// burning the fetch budget on a download that can't be transcribed.
	media.AudioTranscriberReady = asrReady
}

// audioMimeFromPath maps a downloaded audio file's extension to a MIME type for
// the ASR multipart upload. The ASR server decodes any container internally, so
// an approximate type is fine; default to m4a (yt-dlp's bestaudio default).
func audioMimeFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".webm":
		return "audio/webm"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	default:
		return "audio/mp4" // m4a / aac
	}
}
