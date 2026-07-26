package artifact

import (
	"context"
	"strings"
)

// TranscribeAudio exposes the package-private ASR-sidecar entry point so the
// Mini App / native-client audio-capture RPC can transcribe a shared recording
// (voice memo, meeting audio) into a diarized, timestamped transcript. hotwords
// is an optional proper-noun bias list (the caller passes wiki people/company
// names; the operator's DENEB_ASR_HOTWORDS is merged in below). OCR is exposed
// separately by the document subpackage.
func TranscribeAudio(ctx context.Context, audio []byte, mimeType, hotwords string) (string, error) {
	return transcribeAudioText(ctx, audio, mimeType, hotwords)
}

// TranscribeAudioPlain returns the flat transcript with no diarization markup.
//
// TranscribeAudio's "[00:05 S01] …" format is right for a meeting record and
// wrong for a HUD: on 576×288 the timestamps are noise, and a live caption
// sends 3-second windows whose clocks all restart at zero, so the numbers are
// not even meaningful. The sidecar already returns the flat text alongside the
// segments — this just stops throwing it away.
func TranscribeAudioPlain(ctx context.Context, audio []byte, mimeType, hotwords string) (string, error) {
	r, err := transcribeAudio(ctx, audio, audioFilename(mimeType), mergeHotwords(hotwords, asrHotwords()))
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", nil
	}
	return strings.TrimSpace(r.Transcription), nil
}

// ASRBaseURL exposes the configured ASR sidecar base so the gateway can watch
// its liveness. Unlike a model provider, this sidecar has NO fallback chain —
// if it is down, transcription simply fails — which is why it has to be probed
// rather than left to a retry path that does not exist.
func ASRBaseURL() string {
	return asrBaseURL()
}
