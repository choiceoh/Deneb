package agent

import (
	"os"
	"path/filepath"
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

// The index lives only in memory, so spills written by a previous process are
// invisible to the entry sweep. Now that a completed run no longer releases
// spills, those files would accumulate across every restart unless the sweep
// also looks at the disk.
func TestCleanExpiredSweepsOrphanFilesFromDisk(t *testing.T) {
	dir := t.TempDir()

	// A file no index entry claims, aged past the TTL — i.e. left by a previous
	// process.
	orphan := filepath.Join(dir, "sess_gone_1234_exec_sp_deadbeef.txt")
	if err := os.WriteFile(orphan, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	old := time.Now().Add(-2 * SpilloverTTL)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatalf("age orphan: %v", err)
	}

	// A fresh unindexed file must survive — it may be mid-write.
	fresh := filepath.Join(dir, "sess_gone_9999_exec_sp_cafebabe.txt")
	if err := os.WriteFile(fresh, []byte("new"), 0o644); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}

	store := NewSpilloverStore(dir)
	store.SetSessionLiveness(func(string) bool { return true })

	store.cleanExpired()

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("aged orphan file survived the sweep: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh unindexed file was swept: %v", err)
	}
}

// Provenance is a stored bit, not a tool-name lookup: code_action spills mail
// and web pages it read through its bridge under its own name, so the name
// alone would call attacker-authored content operator-owned.
func TestExternalOriginIsStoredNotDerivedFromToolName(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())

	plain, err := store.Store("client:test", "code_action", strings.Repeat("a", 100))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tainted, err := store.Store("client:test", "code_action", strings.Repeat("b", 100))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.MarkExternalOrigin(tainted)

	if store.IsExternalOrigin(plain, "client:test") {
		t.Error("unmarked spill reported external")
	}
	if !store.IsExternalOrigin(tainted, "client:test") {
		t.Error("spill marked external did not report external — nested provenance lost")
	}
	if store.IsExternalOrigin(tainted, "client:other") {
		t.Error("cross-session read reported external")
	}
	if store.IsExternalOrigin("sp_nope", "client:test") {
		t.Error("unknown spill ID reported external")
	}
}

// Provenance must be classified inside Store, not at each call site: the
// YouTube transcript path calls Store directly as "web" and would otherwise
// leave attacker-authored subtitles unmarked, holding the irreversible-tool
// gate open on a later read.
func TestStoreClassifiesExternalOriginByToolName(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())

	for _, tc := range []struct {
		tool string
		want bool
	}{
		{"web", true},
		{"mail_archive", true},
		{"ocr", true},
		{"browse", true},
		{"exec", false},
		{"read", false},
		{"grep", false},
	} {
		id, err := store.Store("client:test", tc.tool, strings.Repeat("x", 100))
		if err != nil {
			t.Fatalf("store %s: %v", tc.tool, err)
		}
		if got := store.IsExternalOrigin(id, "client:test"); got != tc.want {
			t.Errorf("spill from %q: IsExternalOrigin = %v, want %v", tc.tool, got, tc.want)
		}
	}
}

// Session keys nest: "client:main" is the parent of the native per-conversation
// chats "client:main:<uuid>", and ":" sanitizes to "_". Removing the parent must
// not sweep a live child's spill files — doing so would leave the child's index
// entries pointing at files that no longer exist.
func TestRemoveSessionDoesNotTouchNestedChildSessions(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	const parent = "client:main"
	const child = "client:main:11111111-2222-3333-4444-555555555555"

	parentID, err := store.Store(parent, "exec", strings.Repeat("p", 100))
	if err != nil {
		t.Fatalf("store parent: %v", err)
	}
	childID, err := store.Store(child, "exec", strings.Repeat("c", 100))
	if err != nil {
		t.Fatalf("store child: %v", err)
	}

	if err := store.RemoveSession(parent); err != nil {
		t.Fatalf("remove parent: %v", err)
	}

	if _, err := store.Load(parentID, parent); err == nil {
		t.Error("parent session spill survived its own removal")
	}
	if _, err := store.Load(childID, child); err != nil {
		t.Errorf("child session spill was swept by the parent's cleanup: %v", err)
	}
}

// A digit-only child segment must not defeat the disambiguation either.
func TestRemoveSessionDistinguishesNumericChildSegment(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())

	const parent = "client:main"
	const child = "client:main:12345"

	childID, err := store.Store(child, "exec", strings.Repeat("c", 100))
	if err != nil {
		t.Fatalf("store child: %v", err)
	}

	if err := store.RemoveSession(parent); err != nil {
		t.Fatalf("remove parent: %v", err)
	}

	if _, err := store.Load(childID, child); err != nil {
		t.Errorf("numeric-segment child spill was swept by the parent: %v", err)
	}
}

// Conversation rows are never evicted (domain/session/manager.go keeps them as
// the drawer's history), so "session is alive" is an unlimited lease and the
// TTL sweep can never reclaim their spills. A per-session cap is what keeps a
// busy long-lived conversation from growing the spill directory without bound.
func TestStoreEnforcesPerSessionSpillCap(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)
	store.SetSessionLiveness(func(string) bool { return true }) // never reclaimed by TTL

	const key = "client:main"
	var ids []string
	for i := 0; i < maxSpillsPerSession+12; i++ {
		id, err := store.Store(key, "exec", strings.Repeat("x", 1024))
		if err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	live := 0
	for _, id := range ids {
		if _, err := store.Load(id, key); err == nil {
			live++
		}
	}
	if live > maxSpillsPerSession {
		t.Errorf("session retains %d spills, over the %d cap", live, maxSpillsPerSession)
	}

	// Eviction is oldest-first: the newest handle — the one compaction most
	// recently quoted — must still resolve.
	if _, err := store.Load(ids[len(ids)-1], key); err != nil {
		t.Errorf("newest spill was evicted: %v", err)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(files) > maxSpillsPerSession {
		t.Errorf("%d files on disk, over the %d cap — evicted entries left their files behind", len(files), maxSpillsPerSession)
	}
}

// A sibling session's spills must not be touched by another session's quota.
func TestSessionQuotaIsPerSession(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(string) bool { return true })

	siblingID, err := store.Store("client:other", "exec", strings.Repeat("s", 1024))
	if err != nil {
		t.Fatalf("store sibling: %v", err)
	}
	for i := 0; i < maxSpillsPerSession+8; i++ {
		if _, err := store.Store("client:main", "exec", strings.Repeat("x", 1024)); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
	}

	if _, err := store.Load(siblingID, "client:other"); err != nil {
		t.Errorf("sibling session's spill evicted by another session's quota: %v", err)
	}
}
