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

// The truncation marker is what the model actually receives (capToolOutput →
// TruncateHeadTail); FormatPreview is a convenience wrapper with no runtime
// caller. The outline therefore has to live here to have any product effect.
func TestTruncateHeadTailCarriesMiddleOutline(t *testing.T) {
	var b strings.Builder
	b.WriteString("# 시작 섹션\n")
	for i := 0; i < 400; i++ {
		b.WriteString("본문 줄 padding padding padding\n")
	}
	b.WriteString("=== 가운데 구간 ===\n")
	for i := 0; i < 400; i++ {
		b.WriteString("본문 줄 padding padding padding\n")
	}
	b.WriteString("## 끝 섹션\n")
	for i := 0; i < 400; i++ {
		b.WriteString("본문 줄 padding padding padding\n")
	}
	content := b.String()

	out := TruncateHeadTail(content, 2000, "sp_test")

	if !strings.Contains(out, "생략 구간 구조") {
		t.Fatalf("truncation marker carries no outline of the dropped middle:\n%s", out)
	}
	if !strings.Contains(out, "=== 가운데 구간 ===") {
		t.Errorf("outline missing a marker from the discarded middle:\n%s", out)
	}
	// compaction/protected.go parses this pointer with a regex — the outline
	// must not disturb its shape, or a stubbed result loses its handle.
	if !strings.Contains(out, `read_spillover("sp_test")`) {
		t.Errorf("spillover pointer shape broken:\n%s", out)
	}
}

// Outline line numbers must be positions in the ORIGINAL content so they can be
// passed straight to read_spillover(offset=N). A number relative to the middle
// would send the model to the wrong place.
func TestMiddleOutlineLineNumbersAreAbsolute(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("head padding line\n")
	}
	b.WriteString("# 표적 섹션\n") // original line 101
	for i := 0; i < 100; i++ {
		b.WriteString("mid padding line\n")
	}
	b.WriteString("=== 두번째 ===\n")
	for i := 0; i < 100; i++ {
		b.WriteString("tail padding line\n")
	}
	content := b.String()

	out := TruncateHeadTail(content, 600, "sp_test")

	if !strings.Contains(out, "101: # 표적 섹션") {
		t.Fatalf("outline entry must carry its absolute 1-based line number:\n%s", out)
	}
}

// No spill, no outline: without a spill ID there is nothing to offset into.
func TestTruncateHeadTailWithoutSpillHasNoOutline(t *testing.T) {
	content := strings.Repeat("# 섹션\n"+strings.Repeat("본문\n", 50), 20)

	out := TruncateHeadTail(content, 500, "")

	if strings.Contains(out, "생략 구간 구조") {
		t.Errorf("outline emitted with no spill to offset into:\n%s", out)
	}
}

// Unstructured output gets no outline: one stray marker is noise, not a map.
func TestTruncateHeadTailSkipsOutlineWithoutStructure(t *testing.T) {
	content := strings.Repeat("plain log line without structure\n", 500)

	out := TruncateHeadTail(content, 1000, "sp_test")

	if strings.Contains(out, "생략 구간 구조") {
		t.Errorf("outline emitted for unstructured content:\n%s", out)
	}
}
