package chat

import (
	"testing"
)

// resetRecallSnapshotStore wipes the package-level cache so tests do not
// observe state from each other.
func resetRecallSnapshotStore(t *testing.T) {
	t.Helper()
	recallSnapshotStore.mu.Lock()
	recallSnapshotStore.store = make(map[string]string)
	recallSnapshotStore.mu.Unlock()
}

func TestRecallSnapshot_RoundTrip(t *testing.T) {
	resetRecallSnapshotStore(t)

	if _, ok := cachedRecallMemory("s1"); ok {
		t.Fatalf("expected cache miss before store")
	}
	storeRecallMemory("s1", "<recall-context> source=wiki ... </recall-context>")
	got, ok := cachedRecallMemory("s1")
	if !ok || got == "" {
		t.Fatalf("expected cache hit after store, got ok=%v value=%q", ok, got)
	}
}

func TestRecallSnapshot_StoreEmptyIgnored(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "")
	if _, ok := cachedRecallMemory("s1"); ok {
		t.Errorf("empty value should not be cached")
	}
}

func TestRecallSnapshot_EmptySessionKeyIgnored(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("", "anything")
	if _, ok := cachedRecallMemory(""); ok {
		t.Errorf("empty session key should not produce a hit")
	}
}

func TestRecallSnapshot_Clear(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "value")
	clearRecallMemory("s1")
	if _, ok := cachedRecallMemory("s1"); ok {
		t.Errorf("expected miss after clear")
	}
	// Idempotent.
	clearRecallMemory("s1")
	clearRecallMemory("")
}

func TestRecallSnapshot_PerSessionIsolation(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "value-1")
	storeRecallMemory("s2", "value-2")
	if v, _ := cachedRecallMemory("s1"); v != "value-1" {
		t.Errorf("s1: want value-1, got %q", v)
	}
	if v, _ := cachedRecallMemory("s2"); v != "value-2" {
		t.Errorf("s2: want value-2, got %q", v)
	}
	clearRecallMemory("s1")
	if _, ok := cachedRecallMemory("s1"); ok {
		t.Errorf("s1 should be gone")
	}
	if _, ok := cachedRecallMemory("s2"); !ok {
		t.Errorf("s2 should still be cached")
	}
}

func TestRecallSnapshot_FirstWriteWins(t *testing.T) {
	resetRecallSnapshotStore(t)

	storeRecallMemory("s1", "first")
	storeRecallMemory("s1", "second") // must be ignored — atomic first-write-wins
	if v, _ := cachedRecallMemory("s1"); v != "first" {
		t.Errorf("expected first write to survive, got %q", v)
	}
	// After explicit clear, the next store wins again.
	clearRecallMemory("s1")
	storeRecallMemory("s1", "third")
	if v, _ := cachedRecallMemory("s1"); v != "third" {
		t.Errorf("expected post-clear store to take effect, got %q", v)
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
