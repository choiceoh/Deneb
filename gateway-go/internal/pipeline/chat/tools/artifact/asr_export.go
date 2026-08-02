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
	merged := mergeHotwords(hotwords, asrHotwords())
	if asrProvider() == "gemini" {
		text, gerr := transcribeAudioGemini(ctx, audio, mimeType, merged, true)
		if gerr == nil {
			return text, nil
		}
		if !asrReady(ctx) {
			return "", gerr
		}
		// fall through to the reachable sidecar
	}
	r, err := transcribeAudio(ctx, audio, audioFilename(mimeType), merged)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", nil
	}
	return strings.TrimSpace(r.Transcription), nil
}

// ASRBaseURL exposes the configured ASR sidecar base (probe wiring and the
// in-package transcription client share it).
func ASRBaseURL() string {
	return asrBaseURL()
}

// ASRSidecarIsPrimary reports whether the MOSS sidecar is the PRIMARY
// transcription path. Since the Gemini cutover (#4422, DENEB_ASR_PROVIDER=
// gemini) the sidecar is only a fallback — its death no longer breaks
// transcription, so the operator heartbeat must not page for it (live
// 2026-08-03: a fallback-only sidecar outage produced repeated 다운 pushes
// the operator explicitly did not want). When the provider reverts to the
// sidecar, primary status — and the liveness watch — come back with it.
func ASRSidecarIsPrimary() bool {
	return asrProvider() != "gemini"
}
