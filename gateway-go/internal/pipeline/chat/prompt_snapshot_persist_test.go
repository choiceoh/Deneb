package chat

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

// clearSessionStores drops a key from every live snapshot store so a test can
// simulate the empty-memory state of a freshly restarted process.
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

// TestPromptSnapshot_PruneVanishedSession confirms load drops entries whose
// session no longer exists (deleted/expired) and rewrites the file, bounding
// growth without an explicit per-delete hook.
func TestPromptSnapshot_PruneVanishedSession(t *testing.T) {
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

// TestPromptSnapshot_Forget drops a session from disk (the /reset path).
func TestPromptSnapshot_Forget(t *testing.T) {
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

// TestPromptSnapshot_DisabledIsNoOp confirms an empty state dir keeps the
// feature dormant (in-memory only), matching autonomous's SetStateDir contract.
func TestPromptSnapshot_DisabledIsNoOp(t *testing.T) {
	const key = "client:main:persist-disabled"
	t.Cleanup(func() { clearSessionStores(key) })

	p := &promptSnapshotPersister{dir: "", logger: discardLogger()}
	p.record(key, "wiki", sampleCtxFiles(), nil) // must not panic, must not write
	if n := p.load(func(string) bool { return true }); n != 0 {
		t.Fatalf("disabled load restored %d, want 0", n)
	}
}
