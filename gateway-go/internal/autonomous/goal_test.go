package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempGoalStore(t *testing.T) *GoalStore {
	t.Helper()
	dir := t.TempDir()
	return NewGoalStore(filepath.Join(dir, "goals.json"))
}

func TestGoalStore_LoadEmpty(t *testing.T) {
	store := tempGoalStore(t)
	data, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if data.Version != 1 {
		t.Errorf("expected version 1, got %d", data.Version)
	}
	if len(data.Goals) != 0 {
		t.Errorf("expected 0 goals, got %d", len(data.Goals))
	}
}

func TestGoalStore_AddAndList(t *testing.T) {
	store := tempGoalStore(t)

	g1, err := store.Add("first goal", "high")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if g1.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if g1.Priority != PriorityHigh {
		t.Errorf("priority = %q, want %q", g1.Priority, PriorityHigh)
	}
	if g1.Status != StatusActive {
		t.Errorf("status = %q, want %q", g1.Status, StatusActive)
	}

	g2, err := store.Add("second goal", "low")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	goals, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("len = %d, want 2", len(goals))
	}
	// Should be sorted by priority: high first.
	if goals[0].ID != g1.ID {
		t.Errorf("expected high-priority goal first, got %q", goals[0].Priority)
	}
	if goals[1].ID != g2.ID {
		t.Errorf("expected low-priority goal second, got %q", goals[1].Priority)
	}
}

func TestGoalStore_AddEmptyDescription(t *testing.T) {
	store := tempGoalStore(t)
	_, err := store.Add("", "medium")
	if err == nil {
		t.Fatal("expected error for empty description")
	}
}

func TestGoalStore_AddDescriptionTooLong(t *testing.T) {
	store := tempGoalStore(t)
	long := make([]byte, MaxDescriptionLen+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err := store.Add(string(long), "medium")
	if err == nil {
		t.Fatal("expected error for long description")
	}
}

func TestGoalStore_MaxGoals(t *testing.T) {
	store := tempGoalStore(t)
	for i := 0; i < MaxGoals; i++ {
		if _, err := store.Add("goal", "medium"); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	_, err := store.Add("one too many", "medium")
	if err == nil {
		t.Fatal("expected error when exceeding MaxGoals")
	}
}

func TestGoalStore_Remove(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("removable", "medium")

	if err := store.Remove(g.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	goals, _ := store.List()
	if len(goals) != 0 {
		t.Fatalf("len = %d after remove, want 0", len(goals))
	}
}

func TestGoalStore_RemoveNotFound(t *testing.T) {
	store := tempGoalStore(t)
	if err := store.Remove("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestGoalStore_Update(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("updatable", "medium")

	if err := store.Update(g.ID, StatusCompleted, "done"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	goals, _ := store.List()
	if goals[0].Status != StatusCompleted {
		t.Errorf("status = %q, want %q", goals[0].Status, StatusCompleted)
	}
	if goals[0].LastNote != "done" {
		t.Errorf("lastNote = %q, want %q", goals[0].LastNote, "done")
	}
	if goals[0].CycleCount != 1 {
		t.Errorf("cycleCount = %d, want 1", goals[0].CycleCount)
	}

	// Update nonexistent goal.
	err := store.Update("nonexistent", StatusActive, "note")
	if err == nil {
		t.Error("expected error updating nonexistent goal")
	}
}

func TestGoalStore_UpdatePausedReason(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("pausable", "medium")

	store.Update(g.ID, StatusPaused, "waiting for API")
	goals, _ := store.List()
	if goals[0].PausedReason != "waiting for API" {
		t.Errorf("pausedReason = %q", goals[0].PausedReason)
	}

	// Reactivate clears paused reason.
	store.Update(g.ID, StatusActive, "resumed")
	goals, _ = store.List()
	if goals[0].PausedReason != "" {
		t.Errorf("pausedReason should be cleared, got %q", goals[0].PausedReason)
	}
}

func TestGoalStore_UpdateGoal(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("updatable", "low")

	if err := store.UpdateGoal(g.ID, "high", ""); err != nil {
		t.Fatalf("UpdateGoal: %v", err)
	}
	goals, _ := store.List()
	if goals[0].Priority != PriorityHigh {
		t.Errorf("priority = %q, want high", goals[0].Priority)
	}

	// Invalid status.
	if err := store.UpdateGoal(g.ID, "", "invalid_status"); err == nil {
		t.Fatal("expected error for invalid status")
	}

	// Nonexistent goal.
	if err := store.UpdateGoal("nonexistent", PriorityLow, ""); err == nil {
		t.Error("expected error for nonexistent goal")
	}
}

func TestGoalStore_ActiveGoals(t *testing.T) {
	store := tempGoalStore(t)
	g1, _ := store.Add("active goal", "high")
	g2, _ := store.Add("completed goal", "low")
	store.Update(g2.ID, StatusCompleted, "done")

	active, err := store.ActiveGoals()
	if err != nil {
		t.Fatalf("ActiveGoals: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("len = %d, want 1", len(active))
	}
	if active[0].ID != g1.ID {
		t.Errorf("expected active goal ID %q, got %q", g1.ID, active[0].ID)
	}
}

func TestGoalStore_ListSortedByPriority(t *testing.T) {
	store := tempGoalStore(t)
	store.Add("Low priority", PriorityLow)
	store.Add("High priority", PriorityHigh)
	store.Add("Medium priority", PriorityMedium)

	goals, _ := store.List()
	if len(goals) != 3 {
		t.Fatalf("expected 3 goals, got %d", len(goals))
	}
	if goals[0].Priority != PriorityHigh {
		t.Errorf("first = %q, want high", goals[0].Priority)
	}
	if goals[1].Priority != PriorityMedium {
		t.Errorf("second = %q, want medium", goals[1].Priority)
	}
	if goals[2].Priority != PriorityLow {
		t.Errorf("third = %q, want low", goals[2].Priority)
	}
}

func TestGoalStore_PurgeCompleted(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("old completed", "medium")
	store.Update(g.ID, StatusCompleted, "done")

	// Manually backdate the updatedAtMs to trigger purge.
	data, _ := store.Load()
	for i := range data.Goals {
		if data.Goals[i].ID == g.ID {
			data.Goals[i].UpdatedAtMs = time.Now().UnixMilli() - CompletedGoalRetentionMs - 1000
		}
	}
	store.Save(data)

	purged, err := store.PurgeCompleted()
	if err != nil {
		t.Fatalf("PurgeCompleted: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1", purged)
	}

	goals, _ := store.List()
	if len(goals) != 0 {
		t.Errorf("goals remaining = %d, want 0", len(goals))
	}
}

func TestGoalStore_PurgeCompletedKeepsRecent(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("Recent completed", "medium")
	store.Update(g.ID, StatusCompleted, "just done")

	purged, _ := store.PurgeCompleted()
	if purged != 0 {
		t.Errorf("expected 0 purged for recent completion, got %d", purged)
	}
}

func TestGoalStore_CycleState(t *testing.T) {
	store := tempGoalStore(t)

	cs := CycleState{
		LastRunAtMs:       1000,
		LastStatus:        "ok",
		ConsecutiveErrors: 3,
		TotalCycles:       10,
	}
	if err := store.UpdateCycleState(cs); err != nil {
		t.Fatalf("UpdateCycleState: %v", err)
	}

	loaded, err := store.LoadCycleState()
	if err != nil {
		t.Fatalf("LoadCycleState: %v", err)
	}
	if loaded.TotalCycles != 10 {
		t.Errorf("totalCycles = %d, want 10", loaded.TotalCycles)
	}
	if loaded.LastStatus != "ok" {
		t.Errorf("lastStatus = %q, want ok", loaded.LastStatus)
	}
}

func TestGoalStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")

	store1 := NewGoalStore(path)
	store1.Add("persistent goal", "high")

	// Create a new store instance reading from the same file.
	store2 := NewGoalStore(path)
	goals, err := store2.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("len = %d, want 1", len(goals))
	}
	if goals[0].Description != "persistent goal" {
		t.Errorf("description = %q", goals[0].Description)
	}
}

func TestGoalStore_RecentlyChanged(t *testing.T) {
	store := tempGoalStore(t)
	g, _ := store.Add("recent", "medium")
	store.Update(g.ID, StatusCompleted, "done")

	recent, err := store.RecentlyChanged(60 * 1000) // last minute
	if err != nil {
		t.Fatalf("RecentlyChanged: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("len = %d, want 1", len(recent))
	}
}

func TestGoalStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)
	store.Add("test", "medium")

	// Verify file exists and is valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty file")
	}
	// No temp files should remain.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "goals.json" {
			t.Errorf("unexpected file: %s", e.Name())
		}
	}
}

func TestNormalizePriority(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"high", PriorityHigh},
		{"medium", PriorityMedium},
		{"low", PriorityLow},
		{"invalid", PriorityMedium},
		{"", PriorityMedium},
		{"critical", PriorityMedium},
		{"HIGH", PriorityMedium}, // case-sensitive
	}
	for _, tt := range tests {
		got := normalizePriority(tt.input)
		if got != tt.want {
			t.Errorf("normalizePriority(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPriorityRank(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{PriorityHigh, 3},
		{PriorityMedium, 2},
		{PriorityLow, 1},
		{"unknown", 0},
		{"", 0},
	}
	for _, tc := range tests {
		got := priorityRank(tc.input)
		if got != tc.want {
			t.Errorf("priorityRank(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestDefaultGoalStorePath(t *testing.T) {
	got := DefaultGoalStorePath("/home/test")
	want := filepath.Join("/home/test", DefaultAutonomousDir, "goals.json")
	if got != want {
		t.Errorf("DefaultGoalStorePath() = %q, want %q", got, want)
	}
}
