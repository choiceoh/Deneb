package chat

import "testing"

func TestTier1Snapshot_FreezeAndClear(t *testing.T) {
	const session = "client:tier1-test"
	t.Cleanup(func() { clearTier1Wiki(session) })

	if _, ok := cachedTier1Wiki(session); ok {
		t.Fatal("fresh session must have no snapshot")
	}

	// Empty results are not frozen — a store still warming at boot retries.
	storeTier1Wiki(session, "")
	if _, ok := cachedTier1Wiki(session); ok {
		t.Fatal("empty value must not freeze")
	}

	storeTier1Wiki(session, "## 핵심 지식\n첫 스냅샷")
	if v, ok := cachedTier1Wiki(session); !ok || v != "## 핵심 지식\n첫 스냅샷" {
		t.Fatalf("expected frozen snapshot, got (%q, %v)", v, ok)
	}

	// First-write-wins: a later (different) computation must not shift the
	// session's system-prompt bytes mid-session.
	storeTier1Wiki(session, "변경된 위키")
	if v, _ := cachedTier1Wiki(session); v != "## 핵심 지식\n첫 스냅샷" {
		t.Fatalf("snapshot must be first-write-wins, got %q", v)
	}

	clearTier1Wiki(session)
	if _, ok := cachedTier1Wiki(session); ok {
		t.Fatal("/reset must clear the snapshot")
	}
}

func TestTier1Snapshot_ClearAllSessions(t *testing.T) {
	clearAllTier1Wiki()
	t.Cleanup(clearAllTier1Wiki)
	storeTier1Wiki("client:main", "old-main")
	storeTier1Wiki("cron:daily", "old-cron")

	clearAllTier1Wiki()
	for _, session := range []string{"client:main", "cron:daily"} {
		if _, ok := cachedTier1Wiki(session); ok {
			t.Fatalf("tier1 snapshot for %q survived global fact invalidation", session)
		}
	}
}

func TestTier1Snapshot_GlobalClearRejectsInflightStaleRefill(t *testing.T) {
	clearAllTier1Wiki()
	t.Cleanup(clearAllTier1Wiki)
	_, _, generation := cachedTier1WikiWithGeneration("client:main")

	clearAllTier1Wiki()
	if storeTier1WikiIfGeneration("client:main", "stale-before-correction", generation) {
		t.Fatal("pre-invalidation computation refilled the cleared tier1 cache")
	}
	if _, ok := cachedTier1Wiki("client:main"); ok {
		t.Fatal("stale in-flight tier1 snapshot survived generation fence")
	}

	_, _, generation = cachedTier1WikiWithGeneration("client:main")
	if !storeTier1WikiIfGeneration("client:main", "fresh-after-correction", generation) {
		t.Fatal("current-generation tier1 snapshot was rejected")
	}
}
