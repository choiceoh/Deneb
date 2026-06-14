package goals

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_SetGetListActive(t *testing.T) {
	s := NewStore("", nil)
	st := s.Set("client:main", "탑솔라 6월 견적 정리", 0)
	if st.Status != StatusActive || st.MaxTurns != DefaultMaxTurns || st.TurnsUsed != 0 {
		t.Fatalf("unexpected new goal: %+v", st)
	}
	if got := s.Get("client:main"); got == nil || got.Goal != "탑솔라 6월 견적 정리" {
		t.Fatalf("Get mismatch: %+v", got)
	}
	if got := s.Get("client:other"); got != nil {
		t.Fatalf("expected nil for unset session, got %+v", got)
	}
	if active := s.ListActive(); len(active) != 1 {
		t.Fatalf("ListActive = %d, want 1", len(active))
	}
	// Returned states are copies — mutating one must not touch the store.
	st.Goal = "mutated"
	if s.Get("client:main").Goal == "mutated" {
		t.Fatal("Get returned a live reference, not a copy")
	}
}

func TestStore_RecordRun_DoneVerdict(t *testing.T) {
	s := NewStore("", nil)
	s.Set("k", "goal", 20)
	st := s.RecordRun("k", "done", "deliverable produced", false)
	if st.Status != StatusDone {
		t.Fatalf("status = %q, want done", st.Status)
	}
	if st.TurnsUsed != 1 || st.LastVerdict != "done" {
		t.Fatalf("bookkeeping wrong: %+v", st)
	}
	if len(s.ListActive()) != 0 {
		t.Fatal("done goal should not be active")
	}
}

func TestStore_RecordRun_BudgetExhaustionPauses(t *testing.T) {
	s := NewStore("", nil)
	s.Set("k", "goal", 3)
	var st *State
	for i := 0; i < 3; i++ {
		st = s.RecordRun("k", "continue", "", false)
	}
	if st.Status != StatusPaused {
		t.Fatalf("status after budget = %q, want paused", st.Status)
	}
	if st.PausedReason == "" {
		t.Fatal("expected a paused reason on budget exhaustion")
	}
	if st.Remaining() != 0 {
		t.Fatalf("Remaining = %d, want 0", st.Remaining())
	}
}

func TestStore_RecordRun_ParseFailureCapPauses(t *testing.T) {
	s := NewStore("", nil)
	s.Set("k", "goal", 20)
	var st *State
	for i := 0; i < MaxConsecutiveParseFailures; i++ {
		st = s.RecordRun("k", "continue", "", true) // parseFailed
	}
	if st.Status != StatusPaused {
		t.Fatalf("status after %d parse failures = %q, want paused", MaxConsecutiveParseFailures, st.Status)
	}
	// A successful parse resets the counter.
	s.Set("k", "goal", 20)
	s.RecordRun("k", "continue", "", true)
	st = s.RecordRun("k", "continue", "", false) // resets
	if st.ConsecParseFailures != 0 || st.Status != StatusActive {
		t.Fatalf("parse-failure counter not reset: %+v", st)
	}
}

func TestStore_PauseResumeClear(t *testing.T) {
	s := NewStore("", nil)
	s.Set("k", "goal", 20)
	s.RecordRun("k", "continue", "", false) // turnsUsed=1
	s.Pause("k", "user-paused")
	if got := s.Get("k"); got.Status != StatusPaused {
		t.Fatalf("status = %q, want paused", got.Status)
	}
	// Resume reactivates AND resets the budget.
	st := s.Resume("k")
	if st.Status != StatusActive || st.TurnsUsed != 0 || st.PausedReason != "" {
		t.Fatalf("resume did not reset: %+v", st)
	}
	s.Clear("k")
	if got := s.Get("k"); got.Status != StatusCleared {
		t.Fatalf("status = %q, want cleared", got.Status)
	}
	if len(s.ListActive()) != 0 {
		t.Fatal("cleared goal should not be active")
	}
}

func TestStore_Ledger(t *testing.T) {
	s := NewStore("", nil)
	s.Set("k", "goal", 20)
	if s.SeenAction("k", "message:abc") {
		t.Fatal("fresh goal should have empty ledger")
	}
	s.CommitActions("k", []string{"message:abc", "exec:def"})
	if !s.SeenAction("k", "message:abc") || !s.SeenAction("k", "exec:def") {
		t.Fatal("committed actions not seen")
	}
	if s.SeenAction("k", "message:zzz") {
		t.Fatal("uncommitted action reported seen")
	}
	// Set (new goal) resets the ledger.
	s.Set("k", "goal2", 20)
	if s.SeenAction("k", "message:abc") {
		t.Fatal("new goal should start with a clean ledger")
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir, nil)
	s1.Set("client:main", "지속 목표", 10)
	s1.RecordRun("client:main", "continue", "step 1", false)
	s1.CommitActions("client:main", []string{"message:hash1"})

	// A fresh store over the same dir restores the goal + ledger.
	s2 := NewStore(dir, nil)
	got := s2.Get("client:main")
	if got == nil {
		t.Fatalf("goal not restored from %s", filepath.Join(dir, "goals.json"))
	}
	if got.Goal != "지속 목표" || got.TurnsUsed != 1 || got.MaxTurns != 10 {
		t.Fatalf("restored state wrong: %+v", got)
	}
	if !s2.SeenAction("client:main", "message:hash1") {
		t.Fatal("ledger not restored")
	}
}

func TestDestructiveActionKey(t *testing.T) {
	tests := []struct {
		name         string
		tool         string
		input        string
		wantDestruct bool
	}{
		{"message send always destructive", "message", `{"text":"hi"}`, true},
		{"exec always destructive", "exec", `{"command":"ls"}`, true},
		{"gmail send is destructive", "gmail", `{"action":"send","to":"a@b.c"}`, true},
		{"gmail read is not", "gmail", `{"action":"read","id":"1"}`, false},
		{"gmail inbox is not", "gmail", `{"action":"inbox"}`, false},
		{"dropbox upload is destructive", "dropbox", `{"action":"upload","path":"/x"}`, true},
		{"calendar create is destructive", "calendar", `{"action":"create"}`, true},
		{"wiki search is not", "wiki", `{"action":"search","query":"x"}`, false},
		{"plain read tool is not", "read", `{"path":"/x"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, destruct := DestructiveActionKey(tt.tool, []byte(tt.input))
			if destruct != tt.wantDestruct {
				t.Fatalf("destructive = %v, want %v", destruct, tt.wantDestruct)
			}
			if destruct && key == "" {
				t.Fatal("destructive call produced empty key")
			}
		})
	}
}

func TestDestructiveActionKey_StableAcrossKeyOrder(t *testing.T) {
	// Same call, different JSON key order → same ledger key (canonicalized).
	k1, _ := DestructiveActionKey("message", []byte(`{"to":"a@b.c","text":"hi"}`))
	k2, _ := DestructiveActionKey("message", []byte(`{"text":"hi","to":"a@b.c"}`))
	if k1 != k2 {
		t.Fatalf("key not stable across arg order: %q vs %q", k1, k2)
	}
	// Different args → different key.
	k3, _ := DestructiveActionKey("message", []byte(`{"to":"a@b.c","text":"bye"}`))
	if k1 == k3 {
		t.Fatal("different args produced the same key")
	}
}

func TestStore_AddSubgoal(t *testing.T) {
	s := NewStore("", nil)
	if s.AddSubgoal("k", "criterion") != nil {
		t.Fatal("AddSubgoal on a missing goal should return nil")
	}
	s.Set("k", "goal", 20)
	st := s.AddSubgoal("k", "  PDF 생성  ")
	if st == nil || len(st.Subgoals) != 1 || st.Subgoals[0] != "PDF 생성" {
		t.Fatalf("subgoal not added/trimmed: %+v", st)
	}
	if st = s.AddSubgoal("k", "메일 전송"); len(st.Subgoals) != 2 {
		t.Fatalf("second subgoal not appended: %+v", st.Subgoals)
	}
	if st = s.AddSubgoal("k", "   "); len(st.Subgoals) != 2 {
		t.Fatalf("empty subgoal should be ignored: %+v", st.Subgoals)
	}
	// A fresh goal resets the subgoals.
	s.Set("k", "goal2", 20)
	if got := s.Get("k"); len(got.Subgoals) != 0 {
		t.Fatalf("new goal should reset subgoals: %+v", got.Subgoals)
	}
}

func TestState_Summary(t *testing.T) {
	if (*State)(nil).Summary() == "" {
		t.Fatal("nil Summary should be non-empty")
	}
	s := NewStore("", nil)
	s.Set("k", "탑솔라 견적", 20)
	s.AddSubgoal("k", "PDF 생성")
	out := s.Get("k").Summary()
	if !strings.Contains(out, "탑솔라 견적") || !strings.Contains(out, "PDF 생성") || !strings.Contains(out, "진행중") {
		t.Fatalf("summary missing fields: %q", out)
	}
}
