package wiki

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestScanDiariesReportsThenClearsBacklogRemainder verifies the near-term
// drain signal: a single diary larger than the per-cycle byte cap leaves
// MorePending set on the first scan, and a follow-up scan that consumes the
// remainder clears it.
func TestScanDiariesReportsThenClearsBacklogRemainder(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.MkdirAll(store.DiaryDir(), 0o755); err != nil {
		t.Fatalf("mkdir diary: %v", err)
	}
	// Comfortably larger than the 30KB per-cycle cap so the first scan reports a
	// remainder; the exact size is left loose (Hangul is 3 bytes/rune) and the
	// test instead drains chunk-by-chunk until the signal clears.
	big := "## 09:00\n\n" + strings.Repeat("항목 한 줄 컨텍스트 라인.\n", 2200)
	if len(big) <= 30000 {
		t.Fatalf("test diary too small (%d bytes) to exceed the cap", len(big))
	}
	if err := os.WriteFile(filepath.Join(store.DiaryDir(), "diary-2026-06-01.md"), []byte(big), 0o644); err != nil {
		t.Fatalf("write diary: %v", err)
	}

	scan1, err := wd.scanDiaries(context.Background())
	if err != nil || scan1 == nil {
		t.Fatalf("scan1: %v", err)
	}
	if !scan1.MorePending {
		t.Fatalf("scan1.MorePending = false, want true (cap hit with bytes remaining)")
	}
	if err := wd.saveDiaryProcessState(scan1.State); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// Drain the remaining chunks. MorePending must clear on the final scan, and
	// the loop must terminate — a stuck signal would spin the real drain forever.
	drained := false
	for i := 0; i < 12; i++ {
		scan, err := wd.scanDiaries(context.Background())
		if err != nil {
			t.Fatalf("drain scan %d: %v", i, err)
		}
		if scan == nil {
			t.Fatalf("drain scan %d returned nil before MorePending cleared", i)
		}
		if err := wd.saveDiaryProcessState(scan.State); err != nil {
			t.Fatalf("save state on drain %d: %v", i, err)
		}
		if !scan.MorePending {
			drained = true
			break
		}
	}
	if !drained {
		t.Errorf("backlog never drained: MorePending stayed set across 12 scans")
	}
}
