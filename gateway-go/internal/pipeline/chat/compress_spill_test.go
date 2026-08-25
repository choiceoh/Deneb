package chat

import (
	"strings"
	"testing"
)

// A compressed result replaces the output with a ≤30-line summary, so without a
// handle the detail is unrecoverable — the same reason the truncation path
// spills. Observed 2026-08-26: `compress: true` on a 32,089-char exec result
// left the model a 137-byte summary and no way back.
func TestCompressedOutputNamesTheSpillHandle(t *testing.T) {
	original := strings.Repeat("line of output\n", 2000)

	withHandle := compressMarkerFor(original, "sp_abc123", "요약본")
	if !strings.Contains(withHandle, "sp_abc123") || !strings.Contains(withHandle, "read_spillover") {
		t.Fatalf("marker must name the handle: %q", withHandle)
	}
	if !strings.Contains(withHandle, "요약본") {
		t.Fatalf("summary must survive: %q", withHandle)
	}

	withoutHandle := compressMarkerFor(original, "", "요약본")
	if strings.Contains(withoutHandle, "read_spillover") {
		t.Fatalf("no handle stored → no pointer: %q", withoutHandle)
	}
}

// compressMarkerFor exercises the marker construction without a live local
// model: compressToolOutput returns the original when compression is skipped,
// so the formatting itself is asserted here.
func compressMarkerFor(original, spillID, compressed string) string {
	if spillID != "" {
		return sprintfCompressed(original, spillID, compressed)
	}
	return sprintfCompressedPlain(original, compressed)
}
