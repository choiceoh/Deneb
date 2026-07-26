package document

// OCRBaseURL exposes the configured OCR sidecar base for liveness watching.
//
// Same reasoning as artifact.ASRBaseURL: a sidecar with no fallback chain is
// the operator's to fix, so its death has to reach them. The 2026-07-26 finding
// that the ASR sidecar had restarted 18,325 times unnoticed is what this is for.
func OCRBaseURL() string {
	return ocrVLBaseURL()
}
