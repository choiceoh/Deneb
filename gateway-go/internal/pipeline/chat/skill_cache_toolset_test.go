package chat

import "testing"

// The cached ambient catalog is built from the registered tool set
// (requires_tools / fallback_for_tools eligibility), so that set has to be part
// of the cache key. External MCP tools register from a background goroutine
// after boot, so the set provably changes at runtime — keyed on the curator
// version alone, a first turn that won the race pinned an MCP-less catalog
// until a skills file change.
func TestSkillToolSetKeyMovesWhenTheToolSetChanges(t *testing.T) {
	base := []string{"wiki", "calendar", "read"}
	baseKey := skillToolSetKey(base)

	if got := skillToolSetKey([]string{"read", "wiki", "calendar"}); got != baseKey {
		t.Errorf("reordering the same set changed the key (%d != %d); it would force needless rebuilds", got, baseKey)
	}
	if got := skillToolSetKey(append(append([]string{}, base...), "plaud:transcribe")); got == baseKey {
		t.Error("a newly registered MCP tool did not move the key")
	}
	if got := skillToolSetKey(base[:2]); got == baseKey {
		t.Error("removing a tool did not move the key")
	}
	if got := skillToolSetKey(nil); got == baseKey {
		t.Error("an empty registry shares the key of a populated one")
	}
}

// A rename that keeps the count must still move the key — summing hashes alone
// would not catch a swap if the two names collided, so the length mix is not
// the only defense being relied on here.
func TestSkillToolSetKeyMovesOnRename(t *testing.T) {
	before := skillToolSetKey([]string{"wiki", "calendar"})
	after := skillToolSetKey([]string{"wiki", "calendars"})
	if before == after {
		t.Error("renaming a tool did not move the key")
	}
}

// The invalidation hook must clear the tool fingerprint too; leaving a stale
// non-zero key there would let a rebuild-then-same-tools sequence match a
// catalog that was never stored.
func TestInvalidateSkillsCacheClearsTheToolFingerprint(t *testing.T) {
	skillsCache.mu.Lock()
	skillsCache.built = true
	skillsCache.version = 7
	skillsCache.toolsKey = 12345
	skillsCache.mu.Unlock()

	InvalidateSkillsCache()

	skillsCache.mu.RLock()
	defer skillsCache.mu.RUnlock()
	if skillsCache.built || skillsCache.version != 0 || skillsCache.toolsKey != 0 {
		t.Errorf("cache not fully cleared: built=%v version=%d toolsKey=%d",
			skillsCache.built, skillsCache.version, skillsCache.toolsKey)
	}
}
