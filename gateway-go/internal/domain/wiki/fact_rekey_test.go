package wiki

import (
	"testing"
	"time"
)

// A known legacy key from the live table — the migration must carry value,
// kind, and AUTHORITY verbatim (re-stamping authority through the agent tool
// was the rejected alternative) and retire the old identity without erasing
// its history.
func TestRekeyLegacyFactsMovesClaimVerbatimAndTombstonesOldIdentity(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	const oldKey = "미사용자원죽서비스구현과충돌하스텁은발견즉시정리"
	newKey, ok := legacyFactRekeys[oldKey]
	if !ok {
		t.Fatalf("legacy key missing from the rekey table")
	}

	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: oldKey,
		Value:     "미사용 자원(죽은 서비스, 구현과 충돌하는 스텁)은 발견 즉시 정리.",
		Kind:      FactKind("generic"),
		Authority: FactAuthority("legacy_import"),
		Actor:     "test-seed",
		At:        time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	moved, err := store.RekeyLegacyFacts()
	if err != nil {
		t.Fatalf("RekeyLegacyFacts: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved = %d, want 1", moved)
	}

	var got *FactClaim
	for _, c := range store.ActiveFacts("") {
		c := c
		switch c.Key {
		case newKey:
			got = &c
		case oldKey:
			t.Fatalf("old identity still active: %+v", c)
		}
	}
	if got == nil {
		t.Fatal("re-keyed claim not active")
	}
	if got.Value != "미사용 자원(죽은 서비스, 구현과 충돌하는 스텁)은 발견 즉시 정리." ||
		got.Kind != FactKind("generic") ||
		got.Authority != FactAuthority("legacy_import") {
		t.Fatalf("claim not carried verbatim: %+v", got)
	}
}

// Replaying on every startup must be free: a migrated key has no active claim.
func TestRekeyLegacyFactsIsIdempotent(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	const oldKey = "미사용자원죽서비스구현과충돌하스텁은발견즉시정리"
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: oldKey, Value: "v",
		Kind: FactKind("generic"), Authority: FactAuthority("legacy_import"),
		Actor: "test-seed", At: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if moved, err := store.RekeyLegacyFacts(); err != nil || moved != 1 {
		t.Fatalf("first run: moved=%d err=%v", moved, err)
	}
	before := store.LatestFactRevision()
	if moved, err := store.RekeyLegacyFacts(); err != nil || moved != 0 {
		t.Fatalf("second run: moved=%d err=%v", moved, err)
	}
	if store.LatestFactRevision() != before {
		t.Fatal("idempotent replay advanced the journal")
	}
}

// Facts outside the reviewed table — axis-keyed runtime captures — never move.
func TestRekeyLegacyFactsIgnoresAxisKeys(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.answer_first", Value: "즉답부터",
		Kind: FactKind("preference"), Authority: FactAuthority("direct_user"),
		Actor: "test-seed", At: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if moved, err := store.RekeyLegacyFacts(); err != nil || moved != 0 {
		t.Fatalf("moved=%d err=%v, want untouched", moved, err)
	}
}

// The table itself: every target key is a bounded axis identity and unique —
// the exact properties the sentence keys lacked.
func TestLegacyFactRekeyTableTargetsAreAxisShapedAndUnique(t *testing.T) {
	seen := map[string]string{}
	for _, m := range legacyFactRekeyTable {
		if !factAxisKeyShape.MatchString(m.new) {
			t.Errorf("target %q is not axis-shaped", m.new)
		}
		if prev, dup := seen[m.new]; dup {
			t.Errorf("target %q assigned to two legacy keys (%q, %q)", m.new, prev, m.old)
		}
		seen[m.new] = m.old
	}
	if len(legacyFactRekeyTable) != 40 {
		t.Fatalf("table entries = %d, want the reviewed 40", len(legacyFactRekeyTable))
	}
}
