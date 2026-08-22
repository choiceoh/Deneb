package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A live session's spills must survive the TTL sweep. Compaction stubs older
// tool results down to a read_spillover pointer and tells the model the full
// output is still available, so age-based deletion under a live session turns
// that promise into a dangling handle.
func TestCleanExpiredKeepsLiveSessionSpills(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(key string) bool { return key == "client:live" })

	liveID, err := store.Store("client:live", "exec", strings.Repeat("a", 100))
	if err != nil {
		t.Fatalf("store live: %v", err)
	}
	deadID, err := store.Store("client:gone", "exec", strings.Repeat("b", 100))
	if err != nil {
		t.Fatalf("store dead: %v", err)
	}

	// Age both past the TTL.
	store.mu.Lock()
	for _, e := range store.index {
		e.CreatedAt = time.Now().Add(-2 * SpilloverTTL)
	}
	livePath := store.index[liveID].Path
	deadPath := store.index[deadID].Path
	store.mu.Unlock()

	store.cleanExpired()

	if _, err := store.Load(liveID, "client:live"); err != nil {
		t.Errorf("live session spill was swept: %v", err)
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Errorf("live session spill file removed: %v", err)
	}
	if _, err := store.Load(deadID, "client:gone"); err == nil {
		t.Error("finished session spill survived the sweep")
	}
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Errorf("finished session spill file still on disk: %v", err)
	}
}

// Without an injected predicate the sweep must behave as it always did: pure
// age. A nil predicate means "no session manager wired", not "keep everything
// forever" — otherwise a crashed-session spill would leak until restart.
func TestCleanExpiredWithoutLivenessSweepsByAge(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())
	id, err := store.Store("client:test", "exec", strings.Repeat("a", 100))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.mu.Lock()
	store.index[id].CreatedAt = time.Now().Add(-2 * SpilloverTTL)
	store.mu.Unlock()

	store.cleanExpired()

	if _, err := store.Load(id, "client:test"); err == nil {
		t.Error("aged spill survived with no liveness predicate wired")
	}
}

// A spill younger than the TTL is never swept, live session or not.
func TestCleanExpiredKeepsFreshSpills(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())
	id, err := store.Store("client:test", "exec", strings.Repeat("a", 100))
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	store.cleanExpired()

	if _, err := store.Load(id, "client:test"); err != nil {
		t.Errorf("fresh spill swept: %v", err)
	}
}

// The preview header carries the structure the model needs to aim a grep or an
// offset: line count plus an outline of section markers with their line
// numbers. Head+tail alone leaves everything between them unaddressable.
func TestFormatPreviewCarriesOutline(t *testing.T) {
	var b strings.Builder
	b.WriteString("# 첫 섹션\n")
	for i := 0; i < 30; i++ {
		b.WriteString("본문 줄\n")
	}
	b.WriteString("=== 두번째 구간 ===\n")
	for i := 0; i < 30; i++ {
		b.WriteString("본문 줄\n")
	}

	preview := FormatPreview("sp_test", "exec", b.String())

	if !strings.Contains(preview, "lines]") {
		t.Errorf("preview header missing line count:\n%s", preview)
	}
	if !strings.Contains(preview, "첫 섹션") || !strings.Contains(preview, "두번째 구간") {
		t.Errorf("preview outline missing section markers:\n%s", preview)
	}
	if !strings.Contains(preview, "32: === 두번째 구간 ===") {
		t.Errorf("outline entry must carry its 1-based line number:\n%s", preview)
	}
	// compaction/protected.go parses this pointer with a regex — the outline
	// must not disturb its shape, or a stubbed result loses its handle.
	if !strings.Contains(preview, `read_spillover("sp_test")`) {
		t.Errorf("spillover pointer shape broken:\n%s", preview)
	}
}

// Unstructured content gets no outline: one stray marker is noise, not a map.
func TestFormatPreviewSkipsOutlineWithoutStructure(t *testing.T) {
	content := strings.Repeat("plain log line\n", 100)
	if got := FormatPreview("sp_test", "exec", content); strings.Contains(got, "구조 (offset으로 열기)") {
		t.Errorf("outline emitted for unstructured content:\n%s", got)
	}
}
