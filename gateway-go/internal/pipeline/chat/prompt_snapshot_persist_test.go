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

func approvedPromptSnapshotPersister(dir string, revision uint64) *promptSnapshotPersister {
	return &promptSnapshotPersister{
		dir: dir, logger: discardLogger(), factRevision: revision, factDerivedApproved: true,
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

	writer := approvedPromptSnapshotPersister(dir, 7)
	writer.record(key, tier1, ctxFiles, topic)

	if _, err := os.Stat(filepath.Join(dir, promptSnapshotFileName)); err != nil {
		t.Fatalf("snapshot file not written: %v", err)
	}

	// Simulate a restart: empty the in-memory stores, then load from disk.
	clearSessionStores(key)
	if _, ok := cachedTier1Wiki(key); ok {
		t.Fatal("precondition: tier1 store should be empty after clear")
	}

	reader := approvedPromptSnapshotPersister(dir, 7)
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

func TestPromptSnapshot_RevisionMismatchSanitizesFactDerivedFields(t *testing.T) {
	const key = "client:main:persist-revision-mismatch"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(key) })
	topic := sampleTopic()

	writer := approvedPromptSnapshotPersister(dir, 11)
	writer.record(key, "stale-tier1", sampleCtxFiles(), topic)
	clearSessionStores(key)

	// Simulate a fact commit followed by a crash before the live cache observer
	// ran. The journal is now revision 12 while disk still carries revision 11.
	reader := approvedPromptSnapshotPersister(dir, 12)
	if n := reader.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("load restored %d sessions, want topic-only survivor", n)
	}
	if got, ok := cachedTier1Wiki(key); ok || got != "" {
		t.Fatalf("stale tier1 restored across revision mismatch: (%q, %v)", got, ok)
	}
	if got, ok := prompt.Cache.SessionSnapshot(key); ok || len(got) != 0 {
		t.Fatalf("stale context restored across revision mismatch: %#v", got)
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
	if snap := persisted[key]; snap.Tier1Wiki != "" || len(snap.ContextFiles) != 0 || snap.FactRevision != nil || snap.TopicKnowledge == nil {
		t.Fatalf("revision-mismatched snapshot was not sanitized: %+v", snap)
	}
}

func TestPromptSnapshot_UnversionedFactDerivedFieldsAreRejectedAtRevisionZero(t *testing.T) {
	const key = "client:main:persist-unversioned"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(key) })
	topic := sampleTopic()

	// This is the exact pre-upgrade JSON shape. A zero canonical revision can
	// still represent a prose-only cutover, so a missing stamp must not compare
	// equal to an explicitly approved revision 0.
	persisted := map[string]persistedPromptSnapshot{
		key: {Tier1Wiki: "legacy-tier1", ContextFiles: sampleCtxFiles(), TopicKnowledge: topic},
	}
	raw, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, promptSnapshotFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	reader := approvedPromptSnapshotPersister(dir, 0)
	if n := reader.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("load restored %d sessions, want topic-only survivor", n)
	}
	if got, ok := cachedTier1Wiki(key); ok || got != "" {
		t.Fatalf("unversioned tier1 restored at revision zero: (%q, %v)", got, ok)
	}
	if got, ok := prompt.Cache.SessionSnapshot(key); ok || len(got) != 0 {
		t.Fatalf("unversioned context restored at revision zero: %#v", got)
	}
	if got, ok := prompt.Cache.TopicSnapshot(key); !ok || got.Key != topic.Key {
		t.Fatalf("independent topic was not restored: %#v (ok=%v)", got, ok)
	}
}

// TestPromptSnapshot_FirstWriteWins verifies a later turn cannot shift a field
// that was already frozen — the same invariant the in-memory stores hold.
func TestPromptSnapshot_FirstWriteWins(t *testing.T) {
	const key = "client:main:persist-fww"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(key) })

	p := approvedPromptSnapshotPersister(dir, 0)
	p.record(key, "first", sampleCtxFiles(), nil)
	p.record(key, "second-ignored", nil, sampleTopic()) // tier1 already set; topic is new

	clearSessionStores(key)
	reader := approvedPromptSnapshotPersister(dir, 0)
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

	w := approvedPromptSnapshotPersister(dir, 0)
	w.record(live, "live-wiki", nil, nil)
	w.record(dead, "dead-wiki", nil, nil)

	clearSessionStores(live)
	clearSessionStores(dead)

	reader := approvedPromptSnapshotPersister(dir, 0)
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
	r2 := approvedPromptSnapshotPersister(dir, 0)
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

	p := approvedPromptSnapshotPersister(dir, 0)
	p.record(keep, "keep-wiki", nil, nil)
	p.record(drop, "drop-wiki", nil, nil)
	p.forget(drop)

	clearSessionStores(keep)
	clearSessionStores(drop)
	reader := approvedPromptSnapshotPersister(dir, 0)
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
	p := approvedPromptSnapshotPersister(dir, 0)
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
	p := approvedPromptSnapshotPersister(dir, 0)
	topic := sampleTopic()
	oldGeneration := p.currentGeneration()
	p.recordAtGeneration(key, "old-tier1", sampleCtxFiles(), topic, oldGeneration)

	p.clearFactDerivedAtRevision(1, true)
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

	writer := approvedPromptSnapshotPersister(dir, 0)
	writer.record(key, "stale-tier1", sampleCtxFiles(), topic)
	clearSessionStores(key)

	// Simulate a fact mutation after the server is ready but before its async
	// session-restore goroutine calls load(). The in-memory mirror is empty in a
	// fresh process, so the invalidation must be remembered and applied to disk.
	reader := approvedPromptSnapshotPersister(dir, 0)
	reader.clearFactDerivedAtRevision(1, true)
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

func TestPromptSnapshot_ProjectionFailureRejectsFreshFactFieldsUntilHealthyRevision(t *testing.T) {
	const key = "client:main:projection-failure"
	dir := t.TempDir()
	t.Cleanup(func() { clearSessionStores(key) })
	topic := sampleTopic()

	writer := approvedPromptSnapshotPersister(dir, 3)
	writer.record(key, "revision-3-tier1", sampleCtxFiles(), topic)

	// Revision 4 committed canonically, but MEMORY.md/USER.md projection failed.
	// A new turn can still assemble bytes from those stale files; persistence must
	// reject them even though the turn carries the post-invalidation generation.
	writer.clearFactDerivedAtRevision(4, false)
	failedGeneration := writer.currentGeneration()
	writer.recordAtGeneration(
		key,
		"stale-projection-stamped-as-revision-4",
		[]prompt.ContextFile{{Path: "MEMORY.md", Content: "stale projection"}},
		nil,
		failedGeneration,
	)

	clearSessionStores(key)
	reader := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	reader.setFactRevision(4) // successful startup projection explicitly approves revision 4
	if n := reader.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("load restored %d sessions, want topic-only survivor", n)
	}
	if got, ok := cachedTier1Wiki(key); ok || got != "" {
		t.Fatalf("failed-projection tier1 survived restart: (%q, %v)", got, ok)
	}
	if got, ok := prompt.Cache.SessionSnapshot(key); ok || len(got) != 0 {
		t.Fatalf("failed-projection context survived restart: %#v", got)
	}
	if got, ok := prompt.Cache.TopicSnapshot(key); !ok || got.Key != topic.Key {
		t.Fatalf("independent topic was not preserved: %#v (ok=%v)", got, ok)
	}

	// A later healthy canonical mutation explicitly approves revision 5. Fresh
	// derived fields at that generation regain the normal restart restoration.
	reader.clearFactDerivedAtRevision(5, true)
	freshGeneration := reader.currentGeneration()
	freshContext := []prompt.ContextFile{{Path: "MEMORY.md", Content: "healthy projection"}}
	reader.recordAtGeneration(key, "revision-5-tier1", freshContext, nil, freshGeneration)

	clearSessionStores(key)
	restarted := &promptSnapshotPersister{dir: dir, logger: discardLogger()}
	restarted.setFactRevision(5)
	if n := restarted.load(func(string) bool { return true }); n != 1 {
		t.Fatalf("healthy revision restored %d sessions, want 1", n)
	}
	if got, ok := cachedTier1Wiki(key); !ok || got != "revision-5-tier1" {
		t.Fatalf("healthy tier1 = (%q, %v), want restored revision 5", got, ok)
	}
	if got, ok := prompt.Cache.SessionSnapshot(key); !ok || !reflect.DeepEqual(got, freshContext) {
		t.Fatalf("healthy context = %#v (ok=%v), want %#v", got, ok, freshContext)
	}
}

func TestPromptSnapshot_DisableDoesNotApproveRevision(t *testing.T) {
	const key = "client:main:fatal-disable"
	dir := t.TempDir()
	topic := sampleTopic()
	p := approvedPromptSnapshotPersister(dir, 8)
	p.record(key, "before-fatal", sampleCtxFiles(), topic)

	p.disableFactDerived()
	currentGeneration := p.currentGeneration()
	p.recordAtGeneration(key, "after-fatal", sampleCtxFiles(), nil, currentGeneration)

	p.mu.Lock()
	snap := p.store[key]
	approved := p.factDerivedApproved
	revision := p.factRevision
	p.mu.Unlock()
	if approved {
		t.Fatal("fatal disable unexpectedly approved fact-derived persistence")
	}
	if revision != 8 {
		t.Fatalf("fatal disable changed revision to %d, want existing 8", revision)
	}
	if snap.Tier1Wiki != "" || len(snap.ContextFiles) != 0 || snap.FactRevision != nil {
		t.Fatalf("fact-derived fields survived fatal disable: %+v", snap)
	}
	if snap.TopicKnowledge == nil || snap.TopicKnowledge.Key != topic.Key {
		t.Fatalf("fatal disable removed independent topic: %+v", snap.TopicKnowledge)
	}
}

// TestPromptSnapshot_IgnoresRecordWhenDisabled confirms an empty state dir keeps the
// feature dormant (in-memory only), matching autonomous's SetStateDir contract.
func TestPromptSnapshot_IgnoresRecordWhenDisabled(t *testing.T) {
	const key = "client:main:persist-disabled"
	t.Cleanup(func() { clearSessionStores(key) })

	p := approvedPromptSnapshotPersister("", 0)
	p.record(key, "wiki", sampleCtxFiles(), nil) // must not panic, must not write
	if n := p.load(func(string) bool { return true }); n != 0 {
		t.Fatalf("disabled load restored %d, want 0", n)
	}
}
