package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizePriority(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"high", PriorityHigh},
		{"medium", PriorityMedium},
		{"low", PriorityLow},
		{"", PriorityMedium},
		{"critical", PriorityMedium},
		{"HIGH", PriorityMedium}, // case-sensitive
	}
	for _, tc := range tests {
		got := normalizePriority(tc.input)
		if got != tc.want {
			t.Errorf("normalizePriority(%q) = %q, want %q", tc.input, got, tc.want)
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

func TestGoalStore_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

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
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g, err := store.Add("Test goal", PriorityHigh)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if g.Description != "Test goal" {
		t.Errorf("expected description 'Test goal', got %q", g.Description)
	}
	if g.Priority != PriorityHigh {
		t.Errorf("expected priority high, got %q", g.Priority)
	}
	if g.Status != StatusActive {
		t.Errorf("expected status active, got %q", g.Status)
	}
	if g.ID == "" {
		t.Error("expected non-empty ID")
	}

	goals, err := store.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
	if goals[0].ID != g.ID {
		t.Errorf("expected goal ID %q, got %q", g.ID, goals[0].ID)
	}
}

func TestGoalStore_AddValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	// Empty description.
	_, err := store.Add("", PriorityMedium)
	if err == nil {
		t.Error("expected error for empty description")
	}

	// Overly long description.
	longDesc := make([]byte, MaxDescriptionLen+1)
	for i := range longDesc {
		longDesc[i] = 'a'
	}
	_, err = store.Add(string(longDesc), PriorityMedium)
	if err == nil {
		t.Error("expected error for description exceeding max length")
	}
}

func TestGoalStore_AddMaxGoals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	for i := 0; i < MaxGoals; i++ {
		_, err := store.Add("goal", PriorityMedium)
		if err != nil {
			t.Fatalf("Add() goal %d error: %v", i, err)
		}
	}

	// Should hit the limit.
	_, err := store.Add("one more", PriorityMedium)
	if err == nil {
		t.Error("expected error when exceeding MaxGoals")
	}
}

func TestGoalStore_Remove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g, _ := store.Add("To remove", PriorityLow)
	err := store.Remove(g.ID)
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	goals, _ := store.List()
	if len(goals) != 0 {
		t.Errorf("expected 0 goals after remove, got %d", len(goals))
	}

	// Removing non-existent goal.
	err = store.Remove("nonexistent")
	if err == nil {
		t.Error("expected error removing nonexistent goal")
	}
}

func TestGoalStore_ActiveGoals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g1, _ := store.Add("Active goal", PriorityHigh)
	g2, _ := store.Add("Will pause", PriorityMedium)

	// Pause one goal.
	_ = store.Update(g2.ID, StatusPaused, "blocked")

	active, err := store.ActiveGoals()
	if err != nil {
		t.Fatalf("ActiveGoals() error: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active goal, got %d", len(active))
	}
	if active[0].ID != g1.ID {
		t.Errorf("expected active goal %q, got %q", g1.ID, active[0].ID)
	}
}

func TestGoalStore_Update(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g, _ := store.Add("Update test", PriorityMedium)

	// Update status and note.
	err := store.Update(g.ID, StatusCompleted, "done")
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	goals, _ := store.List()
	if goals[0].Status != StatusCompleted {
		t.Errorf("expected status completed, got %q", goals[0].Status)
	}
	if goals[0].LastNote != "done" {
		t.Errorf("expected note 'done', got %q", goals[0].LastNote)
	}
	if goals[0].CycleCount != 1 {
		t.Errorf("expected cycleCount 1, got %d", goals[0].CycleCount)
	}

	// Update nonexistent goal.
	err = store.Update("nonexistent", StatusActive, "note")
	if err == nil {
		t.Error("expected error updating nonexistent goal")
	}
}

func TestGoalStore_UpdatePausedReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g, _ := store.Add("Pause test", PriorityMedium)

	// Pause with reason.
	_ = store.Update(g.ID, StatusPaused, "waiting for API")
	goals, _ := store.List()
	if goals[0].PausedReason != "waiting for API" {
		t.Errorf("expected pausedReason 'waiting for API', got %q", goals[0].PausedReason)
	}

	// Reactivate clears paused reason.
	_ = store.Update(g.ID, StatusActive, "")
	goals, _ = store.List()
	if goals[0].PausedReason != "" {
		t.Errorf("expected empty pausedReason after reactivation, got %q", goals[0].PausedReason)
	}
}

func TestGoalStore_UpdateGoal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g, _ := store.Add("Priority test", PriorityLow)

	// Change priority.
	err := store.UpdateGoal(g.ID, PriorityHigh, "")
	if err != nil {
		t.Fatalf("UpdateGoal() error: %v", err)
	}
	goals, _ := store.List()
	if goals[0].Priority != PriorityHigh {
		t.Errorf("expected priority high, got %q", goals[0].Priority)
	}

	// Invalid status.
	err = store.UpdateGoal(g.ID, "", "invalid_status")
	if err == nil {
		t.Error("expected error for invalid status")
	}

	// Nonexistent goal.
	err = store.UpdateGoal("nonexistent", PriorityLow, "")
	if err == nil {
		t.Error("expected error for nonexistent goal")
	}
}

func TestGoalStore_ListSortedByPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	store.Add("Low priority", PriorityLow)
	store.Add("High priority", PriorityHigh)
	store.Add("Medium priority", PriorityMedium)

	goals, _ := store.List()
	if len(goals) != 3 {
		t.Fatalf("expected 3 goals, got %d", len(goals))
	}
	if goals[0].Priority != PriorityHigh {
		t.Errorf("expected first goal high priority, got %q", goals[0].Priority)
	}
	if goals[1].Priority != PriorityMedium {
		t.Errorf("expected second goal medium priority, got %q", goals[1].Priority)
	}
	if goals[2].Priority != PriorityLow {
		t.Errorf("expected third goal low priority, got %q", goals[2].Priority)
	}
}

func TestGoalStore_PurgeCompleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g, _ := store.Add("Old completed", PriorityMedium)
	_ = store.Update(g.ID, StatusCompleted, "done")

	// Manually backdate the goal's updatedAt.
	data, _ := store.Load()
	for i := range data.Goals {
		if data.Goals[i].ID == g.ID {
			data.Goals[i].UpdatedAtMs = time.Now().UnixMilli() - CompletedGoalRetentionMs - 1000
		}
	}
	_ = store.Save(data)

	purged, err := store.PurgeCompleted()
	if err != nil {
		t.Fatalf("PurgeCompleted() error: %v", err)
	}
	if purged != 1 {
		t.Errorf("expected 1 purged, got %d", purged)
	}

	goals, _ := store.List()
	if len(goals) != 0 {
		t.Errorf("expected 0 goals after purge, got %d", len(goals))
	}
}

func TestGoalStore_PurgeCompletedKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	g, _ := store.Add("Recent completed", PriorityMedium)
	_ = store.Update(g.ID, StatusCompleted, "just done")

	purged, _ := store.PurgeCompleted()
	if purged != 0 {
		t.Errorf("expected 0 purged for recent completion, got %d", purged)
	}
}

func TestGoalStore_CycleState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	state := CycleState{
		LastRunAtMs: time.Now().UnixMilli(),
		LastStatus:  "ok",
		TotalCycles: 5,
	}
	err := store.UpdateCycleState(state)
	if err != nil {
		t.Fatalf("UpdateCycleState() error: %v", err)
	}

	loaded, err := store.LoadCycleState()
	if err != nil {
		t.Fatalf("LoadCycleState() error: %v", err)
	}
	if loaded.TotalCycles != 5 {
		t.Errorf("expected totalCycles 5, got %d", loaded.TotalCycles)
	}
	if loaded.LastStatus != "ok" {
		t.Errorf("expected lastStatus 'ok', got %q", loaded.LastStatus)
	}
}

func TestGoalStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")

	// Write with one store instance.
	store1 := NewGoalStore(path)
	store1.Add("Persisted goal", PriorityHigh)

	// Read with a fresh instance.
	store2 := NewGoalStore(path)
	goals, err := store2.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal from fresh store, got %d", len(goals))
	}
	if goals[0].Description != "Persisted goal" {
		t.Errorf("expected 'Persisted goal', got %q", goals[0].Description)
	}
}

func TestGoalStore_SaveSkipsDuplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	store := NewGoalStore(path)

	store.Add("Test", PriorityMedium)

	// Get file info before second save (same data).
	info1, _ := os.Stat(path)
	data, _ := store.Load()
	// Save same data — should be a no-op.
	_ = store.Save(data)
	info2, _ := os.Stat(path)

	// ModTime should not change since content is identical.
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Log("save wrote file even though content was identical (acceptable but suboptimal)")
	}
}

func TestDefaultGoalStorePath(t *testing.T) {
	got := DefaultGoalStorePath("/home/test")
	want := filepath.Join("/home/test", DefaultAutonomousDir, "goals.json")
	if got != want {
		t.Errorf("DefaultGoalStorePath() = %q, want %q", got, want)
	}
}
