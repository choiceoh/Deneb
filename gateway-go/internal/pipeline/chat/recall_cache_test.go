package chat

import (
	"testing"
)

// resetRecallSnapshotStore wipes the package-level cache so tests do not
// observe state from each other.
func resetRecallSnapshotStore(t *testing.T) {
	t.Helper()
	recallSnapshotStore.mu.Lock()
	recallSnapshotStore.store = make(map[recallSnapshotKey]string)
	recallSnapshotStore.mu.Unlock()
}

func TestRecallSnapshot_RoundTrip(t *testing.T) {
	resetRecallSnapshotStore(t)

	if _, ok := cachedRecallMemory("s1", "fp"); ok {
		t.Fatalf("expected cache miss before store")
	}
	storeRecallMemory("s1", "fp", "<recall-context> source=wiki ... </recall-context>")
	got, ok := cachedRecallMemory("s1", "fp")
	if !ok || got == "" {
		t.Fatalf("expected cache hit after store, got ok=%v value=%q", ok, got)
	}
}

func TestRecallSnapshot_StoreEmptyIgnored(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "fp", "")
	if _, ok := cachedRecallMemory("s1", "fp"); ok {
		t.Errorf("empty value should not be cached")
	}
}

func TestRecallSnapshot_EmptySessionKeyIgnored(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("", "fp", "anything")
	if _, ok := cachedRecallMemory("", "fp"); ok {
		t.Errorf("empty session key should not produce a hit")
	}
}

func TestRecallSnapshot_Clear(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "fpA", "value-A")
	storeRecallMemory("s1", "fpB", "value-B")
	clearRecallMemory("s1")
	if _, ok := cachedRecallMemory("s1", "fpA"); ok {
		t.Errorf("expected miss after clear (fpA)")
	}
	if _, ok := cachedRecallMemory("s1", "fpB"); ok {
		t.Errorf("expected miss after clear (fpB) — clear must wipe all fingerprints for session")
	}
	// Idempotent.
	clearRecallMemory("s1")
	clearRecallMemory("")
}

func TestRecallSnapshot_PerSessionIsolation(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "fp", "value-1")
	storeRecallMemory("s2", "fp", "value-2")
	if v, _ := cachedRecallMemory("s1", "fp"); v != "value-1" {
		t.Errorf("s1: want value-1, got %q", v)
	}
	if v, _ := cachedRecallMemory("s2", "fp"); v != "value-2" {
		t.Errorf("s2: want value-2, got %q", v)
	}
	clearRecallMemory("s1")
	if _, ok := cachedRecallMemory("s1", "fp"); ok {
		t.Errorf("s1 should be gone")
	}
	if _, ok := cachedRecallMemory("s2", "fp"); !ok {
		t.Errorf("s2 should still be cached")
	}
}

func TestRecallSnapshot_PerFingerprintIsolation(t *testing.T) {
	resetRecallSnapshotStore(t)

	// Same session, different cue fingerprints → independent slots so a
	// turn about topic A does not leak its recall into a turn about topic B.
	storeRecallMemory("s1", "topic-a", "value-A")
	storeRecallMemory("s1", "topic-b", "value-B")
	if v, _ := cachedRecallMemory("s1", "topic-a"); v != "value-A" {
		t.Errorf("topic-a: want value-A, got %q", v)
	}
	if v, _ := cachedRecallMemory("s1", "topic-b"); v != "value-B" {
		t.Errorf("topic-b: want value-B, got %q", v)
	}
	// A third fingerprint with no entry must miss.
	if _, ok := cachedRecallMemory("s1", "topic-c"); ok {
		t.Errorf("topic-c should miss")
	}
}

func TestRecallSnapshot_FirstWriteWins(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "fp", "first")
	storeRecallMemory("s1", "fp", "second") // must be ignored — first-write-wins per slot
	if v, _ := cachedRecallMemory("s1", "fp"); v != "first" {
		t.Errorf("expected first write to survive, got %q", v)
	}
	// After explicit clear, the next store wins again.
	clearRecallMemory("s1")
	storeRecallMemory("s1", "fp", "third")
	if v, _ := cachedRecallMemory("s1", "fp"); v != "third" {
		t.Errorf("expected post-clear store to take effect, got %q", v)
	}
}

func TestRecallCueFingerprint_NoCueReturnsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{"unrelated question", "오늘 날씨 어때?"},
		{"empty", ""},
		{"only whitespace", "   \n  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recallCueFingerprint(tc.message); got != "" {
				t.Errorf("recallCueFingerprint(%q) = %q, want empty", tc.message, got)
			}
		})
	}
}

func TestRecallCueFingerprint_CueOnlyVagueReference(t *testing.T) {
	got := recallCueFingerprint("그거 뭐였지?")
	if got != "cue-only" {
		t.Errorf("vague-cue message should map to cue-only slot, got %q", got)
	}
}

func TestRecallCueFingerprint_DifferentTopicsDifferentFingerprints(t *testing.T) {
	// Different topical messages must produce different fingerprints so a
	// turn about topic A does not hit the cache slot of an earlier turn
	// about topic B. This is the core anti-poisoning property.
	a := recallCueFingerprint("전에 chat pipeline 어떻게 됐지?")
	b := recallCueFingerprint("전에 telegram 봇 설정 어떻게 했지?")
	if a == "" || b == "" {
		t.Fatalf("expected both messages to produce non-empty fingerprints, got a=%q b=%q", a, b)
	}
	if a == b {
		t.Errorf("different topics produced same fingerprint: %q vs %q", a, b)
	}
}

func TestRecallCueFingerprint_Stable(t *testing.T) {
	// Same message produces the same fingerprint every time (sorted terms
	// make the output deterministic regardless of token order).
	msg := "전에 chat pipeline 정리한 거"
	first := recallCueFingerprint(msg)
	second := recallCueFingerprint(msg)
	if first == "" {
		t.Fatalf("expected non-empty fingerprint, got empty")
	}
	if first != second {
		t.Errorf("fingerprint not stable: %q vs %q", first, second)
	}
}

func TestRecallMemoryHasEvidence(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"whitespace", "   \n  ", false},
		{"no-evidence stub", `<recall-context>
## 회상 근거 (자동 검색)
source=none confidence=none age=unknown
</recall-context>`, false},
		{"wiki evidence", `<recall-context>
## 회상 근거 (자동 검색)
- source=wiki ref="proj/x.md" confidence=high age=3h score=1.10
  match: foo bar
</recall-context>`, true},
		{"diary evidence", `<recall-context>
- source=diary ref="diary-2026-04-30#10:00" confidence=high age=12d
</recall-context>`, true},
		{"transcript evidence", `<recall-context>
- source=transcript ref="abc123#5/user" confidence=medium age=1h
</recall-context>`, true},
		{"session evidence", `<recall-context>
- source=session ref="msg#7/assistant" confidence=medium age=20m
</recall-context>`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recallMemoryHasEvidence(tc.in); got != tc.want {
				t.Errorf("recallMemoryHasEvidence(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
