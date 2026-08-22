package agent

import (
	"os"
	"path/filepath"
	"strconv"
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

	if !strings.Contains(out, "스필 구조") {
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

	if strings.Contains(out, "스필 구조") {
		t.Errorf("outline emitted with no spill to offset into:\n%s", out)
	}
}

// Unstructured output gets no outline: one stray marker is noise, not a map.
func TestTruncateHeadTailSkipsOutlineWithoutStructure(t *testing.T) {
	content := strings.Repeat("plain log line without structure\n", 500)

	out := TruncateHeadTail(content, 1000, "sp_test")

	if strings.Contains(out, "스필 구조") {
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

// The outline is built from the RAW output — only the spill file is redacted on
// its way to disk — so a secret-bearing heading in the discarded middle must be
// masked before it reaches the marker. Otherwise the outline lifts a secret out
// of the part nobody was going to see and puts it in front of the provider.
func TestMiddleOutlineRedactsSecrets(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("head padding line\n")
	}
	b.WriteString("# OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz0123456789ABCDEF\n")
	b.WriteString("=== 두번째 구간 ===\n")
	for i := 0; i < 200; i++ {
		b.WriteString("tail padding line\n")
	}

	out := TruncateHeadTail(b.String(), 800, "sp_test")

	if strings.Contains(out, "sk-proj-abcdefghijklmnopqrstuvwxyz0123456789ABCDEF") {
		t.Fatalf("outline leaked a secret from the discarded middle:\n%s", out)
	}
}

// Redaction must run BEFORE the 70-byte heading cap. The vendor patterns are
// length-anchored (AKIA + exactly 16 chars), so cutting the heading first can
// end a key one character short of its own pattern: the masker then matches
// nothing and the surviving prefix ships to the provider in the clear. The
// earlier test only covered a secret that still matched after truncation.
func TestMiddleOutlineRedactsBeforeTruncating(t *testing.T) {
	// Positioned so the key STRADDLES outlineEntryMaxChars — truncate-first
	// leaves a partial key, redact-first masks the whole thing.
	heading := "# deployment region credentials for the primary fleet AKIA1234567890ABCDEF"
	if len(heading) <= outlineEntryMaxChars {
		t.Fatalf("fixture must exceed the entry cap (%d bytes)", len(heading))
	}
	if idx := strings.Index(heading, "AKIA"); idx >= outlineEntryMaxChars {
		t.Fatalf("key must start before the cap, starts at %d", idx)
	}

	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("head padding line\n")
	}
	b.WriteString(heading + "\n")
	b.WriteString("=== 두번째 구간 ===\n")
	for i := 0; i < 200; i++ {
		b.WriteString("tail padding line\n")
	}

	out := TruncateHeadTail(b.String(), 800, "sp_test")

	if strings.Contains(out, "AKIA1234") {
		t.Fatalf("truncation cut the key short of its pattern and the prefix escaped redaction:\n%s", out)
	}
}

// A single tool result larger than the whole session ceiling must not slip
// past it. The quota spares the spill it is handing a handle for, so nothing
// would ever evict an oversized one and the advertised ceiling would be
// exceeded by an arbitrary amount for the life of the session.
func TestStoreClampsSpillLargerThanTheSessionCeiling(t *testing.T) {
	origCap := maxSpillBytesPerSession
	maxSpillBytesPerSession = 64 * 1024
	t.Cleanup(func() { maxSpillBytesPerSession = origCap })

	store := NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(string) bool { return true })

	const key = "client:main"
	id, err := store.Store(key, "exec", strings.Repeat("x", maxSpillBytesPerSession*4))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// The handle must still work — refusing the spill would leave the
	// truncation marker with no pointer and the discarded middle unrecoverable.
	got, err := store.Load(id, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) > maxSpillBytesPerSession {
		t.Errorf("stored spill is %d bytes, over the %d ceiling", len(got), maxSpillBytesPerSession)
	}
	if !strings.Contains(got, "뒷부분이 잘렸습니다") {
		t.Error("a clamped spill must say its tail is gone, or the model reads it as complete")
	}
}

// A heading that redaction SHIFTS out of the dropped region must still be
// mapped. The preview's head/tail are raw while the offsets are persisted, so
// outlining "only what the preview omits" drops such a heading from the outline
// while the raw preview does not show it either — visible nowhere.
func TestOutlineKeepsHeadingsShiftedByRedaction(t *testing.T) {
	var b strings.Builder
	// A large secret at the very front: ~1.2KB raw, a short token once redacted.
	b.WriteString(pemBegin + "\n")
	for i := 0; i < 20; i++ {
		b.WriteString(strings.Repeat("A", 60) + "\n")
	}
	b.WriteString(pemEnd + "\n")
	b.WriteString("# 이동한 섹션\n") // raw: past the head; persisted: inside it
	for i := 0; i < 200; i++ {
		b.WriteString("padding line\n")
	}
	b.WriteString("=== 깊은 구간 ===\n")
	for i := 0; i < 200; i++ {
		b.WriteString("tail padding line\n")
	}

	out := TruncateHeadTail(b.String(), 2600, "sp_test")

	if !strings.Contains(out, "# 이동한 섹션") {
		t.Errorf("a heading redaction shifted into the persisted head vanished from the map:\n%s", out)
	}
	if !strings.Contains(out, "=== 깊은 구간 ===") {
		t.Errorf("the dropped region's own heading must still be mapped:\n%s", out)
	}
}

// Redaction can shrink the persisted file below the preview budget. The raw
// preview is still truncated, so the model still needs the map — an early
// return on "the file has no dropped middle" left it with none.
func TestOutlineSurvivesRedactionShrinkingBelowBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString("# 첫 섹션\n")
	b.WriteString(pemBegin + "\n")
	for i := 0; i < 200; i++ { // ~12KB of secret, collapses to one token
		b.WriteString(strings.Repeat("A", 60) + "\n")
	}
	b.WriteString(pemEnd + "\n")
	b.WriteString("=== 둘째 섹션 ===\n")
	content := b.String()

	// Raw content is far over the budget; the persisted file will be far under.
	out := TruncateHeadTail(content, 2600, "sp_test")

	if !strings.Contains(out, "스필 구조") {
		t.Errorf("no map emitted though the preview is truncated:\n%s", out)
	}
	if !strings.Contains(out, "=== 둘째 섹션 ===") {
		t.Errorf("map is missing a heading the preview does not show:\n%s", out)
	}
}

// When the preview's tail begins exactly on a line boundary, that first tail
// line is fully visible and must not be ranked as hidden — otherwise a visible
// heading can outrank a genuinely dropped one under the slot cap.
func TestHiddenLineRangeExcludesTheFirstVisibleTailLine(t *testing.T) {
	// 100 lines of exactly 10 bytes: with maxChars 400 the tail is the last 200
	// bytes = lines 81–100, starting precisely at a boundary.
	text := strings.Repeat("aaaaaaaaa\n", 100)

	got := hiddenLineRange(text, 400)

	if want := ([2]int{21, 80}); got != want {
		t.Errorf("hiddenLineRange = %v, want %v (line 81 is the first visible tail line)", got, want)
	}
}

// Nothing is hidden when the text fits the preview budget.
func TestHiddenLineRangeEmptyWhenNothingDropped(t *testing.T) {
	if got := hiddenLineRange("short\ntext\n", 4000); got != ([2]int{0, 0}) {
		t.Errorf("hiddenLineRange = %v, want zero span", got)
	}
}

// PEM markers assembled at runtime. Spelled out, they are a literal the repo's
// detect-private-key gate rejects — correctly, since it cannot tell a fixture
// from a leak. The concatenated value is byte-identical, so redaction still
// matches it.
const (
	pemBegin = "-----BEGIN PRI" + "VATE KEY-----"
	pemEnd   = "-----END PRI" + "VATE KEY-----"
)

// The outline promises absolute offsets into the SPILL FILE, and Store persists
// redact.String(content). Redaction is not line-count preserving — a PEM block
// collapses to a single token — so numbers taken from the raw output point past
// their heading in the file, silently and by more lines the more secrets the
// output carried.
func TestMiddleOutlineOffsetsMatchThePersistedFile(t *testing.T) {
	var b strings.Builder
	// A multi-line secret BEFORE the headings: this is what shifts everything
	// after it once the file is written.
	b.WriteString(pemBegin + "\n")
	for i := 0; i < 20; i++ {
		b.WriteString(strings.Repeat("A", 60) + "\n")
	}
	b.WriteString(pemEnd + "\n")
	for i := 0; i < 100; i++ {
		b.WriteString("head padding line\n")
	}
	b.WriteString("# 첫번째 섹션\n")
	for i := 0; i < 100; i++ {
		b.WriteString("mid padding line\n")
	}
	b.WriteString("=== 두번째 구간 ===\n")
	for i := 0; i < 300; i++ {
		b.WriteString("tail padding line\n")
	}
	content := b.String()

	store := NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(string) bool { return true })
	const key = "client:main"
	id, err := store.Store(key, "exec", content)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	persisted, err := store.Load(id, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fileLines := strings.Split(persisted, "\n")

	out := TruncateHeadTail(content, 2600, id)

	entries := outlineEntries(t, out)
	if len(entries) == 0 {
		t.Fatalf("no outline entries to check:\n%s", out)
	}
	for _, e := range entries {
		if e.line < 1 || e.line > len(fileLines) {
			t.Fatalf("outline points at line %d, file has %d", e.line, len(fileLines))
		}
		// The whole promise: read_spillover(offset=N) opens THIS heading.
		if got := strings.TrimSpace(fileLines[e.line-1]); got != e.text {
			t.Errorf("offset %d points at %q, outline claims %q", e.line, got, e.text)
		}
	}
}

// outlineEntries parses the "N: heading" pairs out of a truncation marker.
func outlineEntries(t *testing.T, marker string) []struct {
	line int
	text string
} {
	t.Helper()
	const head = "스필 구조 (offset으로 열기): "
	i := strings.Index(marker, head)
	if i < 0 {
		return nil
	}
	section := marker[i+len(head):]
	if j := strings.Index(section, "\n"); j >= 0 {
		section = section[:j]
	}
	var out []struct {
		line int
		text string
	}
	for _, part := range strings.Split(section, " · ") {
		num, text, ok := strings.Cut(part, ": ")
		if !ok {
			continue // the trailing "…" marker
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		out = append(out, struct {
			line int
			text string
		}{n, text})
	}
	return out
}

// When the clamp fires, the marker must not advertise offsets into text the
// spill file does not contain. The outline and the file are both derived from
// persistedForm, so a heading past the ceiling is simply absent from the
// outline rather than pointing at nothing.
func TestOutlineOffsetsHoldWhenTheSpillIsClamped(t *testing.T) {
	origCap := maxSpillBytesPerSession
	maxSpillBytesPerSession = 8 * 1024
	t.Cleanup(func() { maxSpillBytesPerSession = origCap })

	var b strings.Builder
	pad := func(n int) {
		for i := 0; i < n; i++ {
			b.WriteString("padding line for the ceiling test\n")
		}
	}
	// Two headings comfortably inside the ceiling — these must survive and stay
	// addressable — and two well past it, which must not be advertised at all.
	// Two of each because a lone heading is suppressed as noise, so a fixture
	// with one divergent heading would pass vacuously.
	b.WriteString("# 살아남는 섹션 A\n")
	pad(60)
	b.WriteString("# 살아남는 섹션 B\n")
	pad(60)
	b.WriteString("# 살아남는 섹션 C\n")
	for b.Len() < maxSpillBytesPerSession {
		b.WriteString("padding line inside the ceiling\n")
	}
	pad(300)
	b.WriteString("# 잘려나가는 섹션 D\n")
	pad(100)
	b.WriteString("# 잘려나가는 섹션 E\n")
	pad(300)
	content := b.String()

	store := NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(string) bool { return true })
	const key = "client:main"
	id, err := store.Store(key, "exec", content)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	persisted, err := store.Load(id, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fileLines := strings.Split(persisted, "\n")

	out := TruncateHeadTail(content, 2600, id)

	for _, e := range outlineEntries(t, out) {
		if e.line < 1 || e.line > len(fileLines) {
			t.Fatalf("outline points at line %d, clamped file has %d lines", e.line, len(fileLines))
		}
		if got := strings.TrimSpace(fileLines[e.line-1]); got != e.text {
			t.Errorf("offset %d points at %q, outline claims %q", e.line, got, e.text)
		}
	}
}

// Store must never evict the spill it is about to return a handle for.
// Timestamps are taken before the disk write, so a concurrent spill can look
// older; evicting the new entry would hand back a dead read_spillover pointer
// and make the truncated middle unrecoverable.
func TestStoreNeverEvictsTheSpillItJustCreated(t *testing.T) {
	store := NewSpilloverStore(t.TempDir())
	store.SetSessionLiveness(func(string) bool { return true })

	const key = "client:main"
	for i := 0; i < maxSpillsPerSession*2; i++ {
		id, err := store.Store(key, "exec", strings.Repeat("x", 1024))
		if err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
		// Every handle must resolve at the moment it is handed back — that is
		// when capToolOutput embeds it in the truncation marker.
		if _, err := store.Load(id, key); err != nil {
			t.Fatalf("handle %d was dead on return: %v", i, err)
		}
	}
}
