package tools

import "context"

// TranscribeAudio exposes the package-private VibeVoice-ASR entry point so the
// Mini App / native-client audio-capture RPC can transcribe a shared recording
// (voice memo, meeting audio) into a diarized, timestamped transcript. Mirrors
// OcrImageBytes — one thin wrapper keeps the tools surface narrow.
func TranscribeAudio(ctx context.Context, audio []byte, mimeType string) (string, error) {
	return transcribeAudioText(ctx, audio, mimeType)
}
