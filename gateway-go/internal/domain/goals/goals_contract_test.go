package goals

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultStoreConcurrentAccess(t *testing.T) {
	previous := Default()
	t.Cleanup(func() { SetDefault(previous) })
	stores := []*Store{NewStore("", nil), NewStore("", nil), nil}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if i%3 == 0 {
					SetDefault(stores[(i+j)%len(stores)])
				} else {
					_ = Default()
				}
			}
		}(i)
	}
	wg.Wait()
	SetDefault(stores[0])
	if Default() != stores[0] {
		t.Fatal("default store did not retain final assignment")
	}
}

func TestStateCloneDeepCopiesMutableFields(t *testing.T) {
	original := &State{
		Goal:            "goal",
		Status:          StatusActive,
		Subgoals:        []string{"one", "two"},
		ExecutedActions: map[string]bool{"message:a": true},
	}
	cloned := original.clone()
	cloned.Goal = "changed"
	cloned.Subgoals[0] = "changed"
	cloned.ExecutedActions["message:a"] = false
	cloned.ExecutedActions["message:b"] = true
	if original.Goal != "goal" || original.Subgoals[0] != "one" ||
		!original.ExecutedActions["message:a"] || original.ExecutedActions["message:b"] {
		t.Fatalf("clone aliases original: original=%+v clone=%+v", original, cloned)
	}
	if (*State)(nil).clone() != nil {
		t.Fatal("nil clone is non-nil")
	}
}

func TestStateActiveRemainingAndSummaryStatuses(t *testing.T) {
	if (*State)(nil).Active() || (*State)(nil).Remaining() != 0 {
		t.Fatal("nil state reported active or remaining budget")
	}
	for _, tc := range []struct {
		status Status
		label  string
	}{
		{StatusActive, "진행중"},
		{StatusPaused, "일시중지"},
		{StatusDone, "완료"},
		{StatusCleared, "중단됨"},
		{Status("custom"), "custom"},
	} {
		st := &State{
			Goal:         "목표",
			Status:       tc.status,
			TurnsUsed:    3,
			MaxTurns:     5,
			LastVerdict:  "continue",
			LastReason:   "진행 중",
			PausedReason: "사유",
			Subgoals:     []string{"첫째", "둘째"},
		}
		if got := st.Active(); got != (tc.status == StatusActive) {
			t.Errorf("Active(%q) = %v", tc.status, got)
		}
		if got := st.Remaining(); got != 2 {
			t.Errorf("Remaining(%q) = %d", tc.status, got)
		}
		summary := st.Summary()
		for _, want := range []string{tc.label, "진행 3/5", "1. 첫째", "2. 둘째", "최근 판정", "일시중지 사유"} {
			if !strings.Contains(summary, want) {
				t.Errorf("Summary(%q) missing %q: %s", tc.status, want, summary)
			}
		}
	}
	if got := (&State{MaxTurns: 2, TurnsUsed: 3}).Remaining(); got != 0 {
		t.Fatalf("overused Remaining = %d", got)
	}
}

func TestListActiveHasStableCreatedThenSessionOrder(t *testing.T) {
	store := NewStore("", nil)
	clock := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	store.Set("z", "z goal", 1)
	store.Set("a", "a goal", 1)
	clock = clock.Add(time.Second)
	store.Set("m", "m goal", 1)
	store.Set("done", "done goal", 1)
	store.RecordRun("done", "done", "finished", false)

	for i := 0; i < 20; i++ {
		active := store.ListActive()
		keys := make([]string, len(active))
		for j := range active {
			keys[j] = active[j].SessionKey
			active[j].Goal = "mutated"
		}
		if want := []string{"a", "z", "m"}; !reflect.DeepEqual(keys, want) {
			t.Fatalf("active order = %#v, want %#v", keys, want)
		}
	}
	if store.Get("a").Goal != "a goal" {
		t.Fatal("ListActive returned live state")
	}
}

func TestPauseOnlyTransitionsActiveGoals(t *testing.T) {
	store := NewStore("", nil)
	if store.Pause("missing", "reason") != nil {
		t.Fatal("Pause missing returned state")
	}
	store.Set("active", "goal", 5)
	paused := store.Pause("active", "operator")
	if paused.Status != StatusPaused || paused.PausedReason != "operator" {
		t.Fatalf("Pause active = %+v", paused)
	}
	pausedAgain := store.Pause("active", "replacement")
	if pausedAgain.Status != StatusPaused || pausedAgain.PausedReason != "operator" {
		t.Fatalf("Pause paused changed state = %+v", pausedAgain)
	}

	store.Set("done", "goal", 5)
	store.RecordRun("done", "done", "complete", false)
	done := store.Pause("done", "should not regress")
	if done.Status != StatusDone || done.PausedReason != "" {
		t.Fatalf("Pause done regressed terminal state = %+v", done)
	}
	store.Set("cleared", "goal", 5)
	store.Clear("cleared")
	cleared := store.Pause("cleared", "should not regress")
	if cleared.Status != StatusCleared {
		t.Fatalf("Pause cleared regressed terminal state = %+v", cleared)
	}
}

func TestResumeNoopAndResetSemantics(t *testing.T) {
	store := NewStore("", nil)
	if store.Resume("missing") != nil {
		t.Fatal("Resume missing returned state")
	}
	store.Set("active", "goal", 5)
	store.RecordRun("active", "continue", "step", true)
	active := store.Resume("active")
	if active.Status != StatusActive || active.TurnsUsed != 1 || active.ConsecParseFailures != 1 {
		t.Fatalf("Resume active changed state = %+v", active)
	}
	store.Pause("active", "wait")
	resumed := store.Resume("active")
	if resumed.Status != StatusActive || resumed.TurnsUsed != 0 || resumed.ConsecParseFailures != 0 || resumed.PausedReason != "" {
		t.Fatalf("Resume paused = %+v", resumed)
	}
	store.Set("done", "goal", 5)
	store.RecordRun("done", "done", "complete", false)
	if got := store.Resume("done"); got.Status != StatusDone {
		t.Fatalf("Resume done = %+v", got)
	}
}

func TestRecordRunMissingAndLastBudgetDonePrecedence(t *testing.T) {
	store := NewStore("", nil)
	if got := store.RecordRun("missing", "done", "", false); got != nil {
		t.Fatalf("RecordRun missing = %+v", got)
	}
	store.Set("last", "goal", 1)
	finished := store.RecordRun("last", "done", "completed on last run", false)
	if finished.Status != StatusDone || finished.TurnsUsed != 1 || finished.PausedReason != "" {
		t.Fatalf("last-budget done = %+v", finished)
	}
	store.Set("parse", "goal", MaxConsecutiveParseFailures+2)
	store.RecordRun("parse", "continue", "bad", true)
	reset := store.RecordRun("parse", "skipped", "judge unavailable", false)
	if reset.ConsecParseFailures != 0 || reset.Status != StatusActive {
		t.Fatalf("non-parse failure did not reset count = %+v", reset)
	}
}

func TestCommitActionsCopiesKeysAndHandlesMissing(t *testing.T) {
	store := NewStore("", nil)
	store.CommitActions("missing", []string{"message:a"})
	store.Set("k", "goal", 2)
	store.CommitActions("k", nil)
	if store.SeenAction("k", "message:a") {
		t.Fatal("nil commit changed ledger")
	}
	keys := []string{"message:a", "exec:b", "message:a"}
	store.CommitActions("k", keys)
	keys[0] = "mutated"
	if !store.SeenAction("k", "message:a") || !store.SeenAction("k", "exec:b") || store.SeenAction("k", "mutated") {
		t.Fatalf("ledger = %+v", store.Get("k").ExecutedActions)
	}
	if store.SeenAction("missing", "message:a") {
		t.Fatal("missing goal reported seen action")
	}
}

func TestLoadFiltersNilAndSurvivesCorruptState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	persisted := map[string]*State{
		"valid": {Goal: "restored", Status: StatusActive, SessionKey: "valid", ExecutedActions: map[string]bool{"message:a": true}},
		"nil":   nil,
	}
	data, _ := json.Marshal(persisted)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir, slog.Default())
	if got := store.Get("valid"); got == nil || got.Goal != "restored" || !got.ExecutedActions["message:a"] {
		t.Fatalf("restored = %+v", got)
	}
	if store.Get("nil") != nil {
		t.Fatal("nil persisted goal was loaded")
	}

	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := NewStore(dir, slog.Default())
	if got := corrupt.ListActive(); len(got) != 0 {
		t.Fatalf("corrupt state loaded goals = %+v", got)
	}
}

func TestDestructiveActionClassificationMatrix(t *testing.T) {
	for _, action := range []string{"send", "reply", "forward", "upload", "share", "backup", "create", "update", "delete", "add", "remove", "write", "edit"} {
		input := []byte(`{"action":" ` + strings.ToUpper(action) + ` "}`)
		key, ok := DestructiveActionKey("tool", input)
		if !ok || !strings.HasPrefix(key, "tool:") {
			t.Errorf("action %q = %q,%v", action, key, ok)
		}
	}
	for _, input := range []string{
		`{"action":"list"}`,
		`{"action":"search"}`,
		`{"action":"read"}`,
		`{"action":12}`,
		`not-json`,
		`{}`,
	} {
		if key, ok := DestructiveActionKey("tool", []byte(input)); ok || key != "" {
			t.Errorf("read/invalid input %q = %q,%v", input, key, ok)
		}
	}
	for _, name := range []string{"message", "exec"} {
		if key, ok := DestructiveActionKey(name, []byte("not-json")); !ok || !strings.HasPrefix(key, name+":") {
			t.Errorf("always destructive %q = %q,%v", name, key, ok)
		}
	}
}

func TestHashArgsCanonicalValuesAndRawFallback(t *testing.T) {
	if a, b := hashArgs([]byte(`{"b":2,"a":1}`)), hashArgs([]byte(" { \"a\" : 1, \"b\" : 2 } ")); a != b {
		t.Fatalf("canonical object hashes differ: %q != %q", a, b)
	}
	if a, b := hashArgs([]byte(`[1,2,3]`)), hashArgs([]byte("[1, 2, 3]")); a != b {
		t.Fatalf("canonical array hashes differ: %q != %q", a, b)
	}
	if a, b := hashArgs([]byte("not-json")), hashArgs([]byte("not-json")); a != b || len(a) != 16 {
		t.Fatalf("raw hash shape/stability = %q,%q", a, b)
	}
	if hashArgs([]byte("raw-a")) == hashArgs([]byte("raw-b")) {
		t.Fatal("different raw inputs collided")
	}
}

func TestItoaBoundaries(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{20, "20"},
		{1234567890, "1234567890"},
	} {
		if got := itoa(tc.n); got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
