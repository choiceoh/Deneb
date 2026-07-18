package artifact

import "context"

// TranscribeAudio exposes the package-private ASR-sidecar entry point so the
// Mini App / native-client audio-capture RPC can transcribe a shared recording
// (voice memo, meeting audio) into a diarized, timestamped transcript. hotwords
// is an optional proper-noun bias list (the caller passes wiki people/company
// names; the operator's DENEB_ASR_HOTWORDS is merged in below). OCR is exposed
// separately by the document subpackage.
func TranscribeAudio(ctx context.Context, audio []byte, mimeType, hotwords string) (string, error) {
	return transcribeAudioText(ctx, audio, mimeType, hotwords)
}
