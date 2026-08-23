package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

// clearSessionStores drops a key from every live snapshot store to simulate a fresh process.
func clearSessionStores(key string) {
	clearTier1Wiki(key)
	prompt.Cache.ClearSession(key)
}

func sampleCtxFiles() []prompt.ContextFile {
	return []prompt.ContextFile{
		{Path: "MEMORY.md", Content: "## 기억\n- 한국어 바이트 보존 테스트 ✅"},
		{Path: "AGENTS.md", Content: "rules here"},
	}
}

func sampleTopic() *prompt.TopicKnowledge {
	return &prompt.TopicKnowledge{
		Key:     "coding",
		Content: "토픽 본문",
		Hash:    "abc123def456",
		Path:    "/ws/topics/coding.md",
	}
}

// TestPromptSnapshot_RoundTripRestoresExactBytes is the core cache-doctrine
// guarantee: persist a session's frozen inputs, then load them in a fresh
// persister (simulating a restart with empty memory) and confirm the live
// stores come back byte-for-byte identical.
func TestPromptSnapshot_RoundTripRestoresExactBytes(t *testing.T) {
	const key = "client:main:persist-rt"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(key) })

	tier1 := "## 핵심 지식\n중요한 위키 내용"
	ctxFiles := sampleCtxFiles()
	topic := sampleTopic()

	writer := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	writer.record(key, tier1, ctxFiles, topic)

	if _, err := os.Stat(filepath.Join(dir, promptSnapshotFileName)); err != nil {
		t.Fatalf("snapshot file not written: %v", err)
	}

	// Simulate a restart: empty the in-memory stores, then load from disk.
	clearSessionStores(key)
	if _, ok := cachedTier1Wiki(key); ok {
		t.Fatal("precondition: tier1 store should be empty after clear")
	}

	reader := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	if n := reader.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("load restored %d sessions, want 1", n)
	}

	if got, ok := cachedTier1Wiki(key); !ok || got != tier1 {
		t.Fatalf("tier1 = (%q, %v), want (%q, true)", got, ok, tier1)
	}
	gotCtx, ok := prompt.Cache.SessionSnapshot(key)
	if !ok || !reflect.DeepEqual(gotCtx, ctxFiles) {
		t.Fatalf("context files = %#v (ok=%v), want %#v", gotCtx, ok, ctxFiles)
	}
	gotTopic, ok := prompt.Cache.TopicSnapshot(key)
	if !ok || gotTopic != *topic {
		t.Fatalf("topic = %#v (ok=%v), want %#v", gotTopic, ok, *topic)
	}
}

// TestPromptSnapshot_FirstWriteWins verifies a later turn cannot shift a field
// that was already frozen — the same invariant the in-memory stores hold.
func TestPromptSnapshot_FirstWriteWins(t *testing.T) {
	const key = "client:main:persist-fww"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(key) })

	p := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	p.record(key, "first", sampleCtxFiles(), nil)
	p.record(key, "second-ignored", nil, sampleTopic()) // tier1 already set; topic is new

	clearSessionStores(key)
	reader := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	reader.load(func(string) bool { return true })

	if got, _ := cachedTier1Wiki(key); got != "first" {
		t.Fatalf("tier1 = %q, want %q (first-write-wins)", got, "first")
	}
	// A field absent on the first record (topic) is still allowed to fill in on
	// a later turn — only already-set fields are frozen.
	if _, ok := prompt.Cache.TopicSnapshot(key); !ok {
		t.Fatal("topic added on a later turn should persist")
	}
}

// TestPromptSnapshot_EvictsVanishedSession confirms load drops entries whose
// session no longer exists (deleted/expired) and rewrites the file, bounding
// growth without an explicit per-delete hook.
func TestPromptSnapshot_EvictsVanishedSession(t *testing.T) {
	const live = "client:main:persist-live"
	const dead = "client:main:persist-dead"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(live); clearSessionStores(dead) })

	w := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	w.record(live, "live-wiki", nil, nil)
	w.record(dead, "dead-wiki", nil, nil)

	clearSessionStores(live)
	clearSessionStores(dead)

	reader := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	isLive := func(k string) bool { return k == live }
	if n := reader.load(isLive); n != 1 {
		t.Fatalf("load restored %d, want 1 (dead pruned)", n)
	}
	if _, ok := cachedTier1Wiki(dead); ok {
		t.Fatal("dead session must not be restored")
	}
	if _, ok := cachedTier1Wiki(live); !ok {
		t.Fatal("live session must be restored")
	}

	// The dead entry must also be gone from disk after the prune rewrite: a
	// second load that treats everything as live restores only the survivor.
	clearSessionStores(live)
	r2 := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	if n := r2.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("after prune, file holds %d sessions, want 1", n)
	}
}

// TestPromptSnapshot_DeletesSessionOnForget drops a session from disk (the /reset path).
func TestPromptSnapshot_DeletesSessionOnForget(t *testing.T) {
	const keep = "client:main:persist-keep"
	const drop = "client:main:persist-drop"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(keep); clearSessionStores(drop) })

	p := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	p.record(keep, "keep-wiki", nil, nil)
	p.record(drop, "drop-wiki", nil, nil)
	p.forget(drop)

	clearSessionStores(keep)
	clearSessionStores(drop)
	reader := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	if n := reader.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("after forget, restored %d, want 1", n)
	}
	if _, ok := cachedTier1Wiki(drop); ok {
		t.Fatal("forgotten session must not persist")
	}
}

// TestPromptSnapshot_GateRejectsNonRestorable ensures only client:main(:id)
// sessions are written, so cron/system keys never bloat the file.
func TestPromptSnapshot_GateRejectsNonRestorable(t *testing.T) {
	dir := t.TempDir()
	p := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	p.record("cron:daily", "should-not-persist", nil, nil)
	p.record("system:diary-heartbeat", "nope", nil, nil)

	if _, err := os.Stat(filepath.Join(dir, promptSnapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("non-restorable sessions must not create a file (err=%v)", err)
	}

	// The bare home session and explicit sub-conversations both qualify.
	for _, k := range []string{"client:main", "client:main:abc"} {
		if !isRestorablePromptSnapshotSession(k) {
			t.Errorf("%q should be persistable", k)
		}
	}
	for _, k := range []string{"cron:x", "system:y", "hook:z", "client:other"} {
		if isRestorablePromptSnapshotSession(k) {
			t.Errorf("%q should NOT be persistable", k)
		}
	}
}

func TestPromptSnapshot_FactInvalidationPersistsAndFencesInflightRefill(t *testing.T) {
	const key = "client:main:fact-invalidation"
	dir := t.TempDir()
	p := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	topic := sampleTopic()
	oldGeneration := p.currentGeneration()
	p.recordAtGeneration(key, "old-tier1", sampleCtxFiles(), topic, oldGeneration)

	p.clearFactDerived()
	p.mu.Lock()
	cleared := p.store[key]
	p.mu.Unlock()
	if cleared.Tier1Wiki != "" || len(cleared.ContextFiles) != 0 {
		t.Fatalf("fact-derived fields survived clear: %+v", cleared)
	}
	if cleared.TopicKnowledge == nil || cleared.TopicKnowledge.Key != topic.Key {
		t.Fatalf("independent topic was cleared: %+v", cleared.TopicKnowledge)
	}

	p.recordAtGeneration(key, "stale-tier1", sampleCtxFiles(), nil, oldGeneration)
	p.mu.Lock()
	staleRefill := p.store[key]
	p.mu.Unlock()
	if staleRefill.Tier1Wiki != "" || len(staleRefill.ContextFiles) != 0 {
		t.Fatalf("pre-invalidation turn repopulated persisted facts: %+v", staleRefill)
	}

	raw, err := os.ReadFile(filepath.Join(dir, promptSnapshotFileName))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]persistedPromptSnapshot
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if snap := persisted[key]; snap.Tier1Wiki != "" || len(snap.ContextFiles) != 0 || snap.TopicKnowledge == nil {
		t.Fatalf("on-disk invalidation = %+v", snap)
	}

	newGeneration := p.currentGeneration()
	p.recordAtGeneration(key, "fresh-tier1", []prompt.ContextFile{{Path: "MEMORY.md", Content: "fresh"}}, nil, newGeneration)
	p.mu.Lock()
	fresh := p.store[key]
	p.mu.Unlock()
	if fresh.Tier1Wiki != "fresh-tier1" || len(fresh.ContextFiles) != 1 || fresh.ContextFiles[0].Content != "fresh" {
		t.Fatalf("current generation did not refill persisted facts: %+v", fresh)
	}
}

func TestPromptSnapshot_FactInvalidationBeforeAsyncLoadSanitizesDisk(t *testing.T) {
	const key = "client:main:fact-invalidation-before-load"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(key) })
	topic := sampleTopic()

	writer := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	writer.record(key, "stale-tier1", sampleCtxFiles(), topic)
	clearSessionStores(key)

	// Simulate a fact mutation after the server is ready but before its async
	// session-restore goroutine calls load(). The in-memory mirror is empty in a
	// fresh process, so the invalidation must be remembered and applied to disk.
	reader := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	reader.clearFactDerived()
	if n := reader.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("load restored %d sessions, want topic-only survivor", n)
	}
	if got, ok := cachedTier1Wiki(key); ok || got != "" {
		t.Fatalf("stale tier1 restored after clear-before-load: (%q, %v)", got, ok)
	}
	if got, ok := prompt.Cache.SessionSnapshot(key); ok || len(got) != 0 {
		t.Fatalf("stale context files restored after clear-before-load: %#v", got)
	}
	if got, ok := prompt.Cache.TopicSnapshot(key); !ok || got.Key != topic.Key {
		t.Fatalf("independent topic was not restored: %#v (ok=%v)", got, ok)
	}

	raw, err := os.ReadFile(filepath.Join(dir, promptSnapshotFileName))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]persistedPromptSnapshot
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if snap := persisted[key]; snap.Tier1Wiki != "" || len(snap.ContextFiles) != 0 || snap.TopicKnowledge == nil {
		t.Fatalf("startup mirror was not sanitized: %+v", snap)
	}
}

// TestPromptSnapshot_IgnoresRecordWhenDisabled confirms an empty state dir keeps the
// feature dormant (in-memory only), matching autonomous's SetStateDir contract.
func TestPromptSnapshot_IgnoresRecordWhenDisabled(t *testing.T) {
	const key = "client:main:persist-disabled"
	t.Cleanup(func() { clearSessionStores(key) })

	p := &promptSnapshotPersister{dir: "", logger: discardLogger()}
	p.record(key, "wiki", sampleCtxFiles(), nil) // must not panic, must not write
	if n := p.load(func(string) bool { return true }); n != 0 {
		t.Fatalf("disabled load restored %d, want 0", n)
	}
}
