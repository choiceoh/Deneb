package chat

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
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

// TestCachedToolResult_CompressSpillsOnCacheHit: run cache stores pre-compression
// output. A repeat call with compress:true must spill before summarizing — the
// #4766 cold-path fix never reached the cache-hit path.
func TestCachedToolResult_CompressSpillsOnCacheHit(t *testing.T) {
	spillDir := t.TempDir()
	store := agent.NewSpilloverStore(spillDir)
	rc := NewRunCache()
	cached := strings.Repeat("line of grep output\n", 1200) // > compressThreshold
	rc.Set("grep:{}", cached)

	ctx := toolport.WithSessionKey(context.Background(), "client:main")
	got, ok := cachedToolResult(ctx, rc, "grep:{}", "grep", true, store)
	if !ok {
		t.Fatal("expected cache hit")
	}
	entries, err := os.ReadDir(spillDir)
	if err != nil {
		t.Fatalf("read spill dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("cache-hit compress must spill full output before summarizing")
	}
	if strings.Contains(got, "compressed by pilot") && !strings.Contains(got, "read_spillover") {
		t.Fatalf("compressed cache hit lost recovery handle: %q", got[:min(240, len(got))])
	}
}
