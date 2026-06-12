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
