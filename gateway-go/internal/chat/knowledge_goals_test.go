package chat

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
)

func tempGoalStore(t *testing.T) *autonomous.GoalStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "goals.json")
	return autonomous.NewGoalStore(path)
}

func TestAutoSetGoalsFromFacts_NilStore(t *testing.T) {
	// Should not panic with nil store.
	autoSetGoalsFromFacts(nil, []memory.SearchResult{
		{Fact: memory.Fact{Content: "test", Category: "decision", Importance: 0.9}},
	})
}

func TestAutoSetGoalsFromFacts_EmptyFacts(t *testing.T) {
	store := tempGoalStore(t)
	autoSetGoalsFromFacts(store, nil)
	goals, _ := store.List()
	if len(goals) != 0 {
		t.Errorf("expected 0 goals, got %d", len(goals))
	}
}

func TestAutoSetGoalsFromFacts_CreatesGoalFromDecision(t *testing.T) {
	store := tempGoalStore(t)
	now := time.Now()
	facts := []memory.SearchResult{
		{
			Fact: memory.Fact{
				ID:         1,
				Content:    "Telegram 봇 에러 핸들링 개선 필요",
				Category:   "decision",
				Importance: 0.9,
				CreatedAt:  now.Add(-2 * 24 * time.Hour),
				UpdatedAt:  now.Add(-1 * 24 * time.Hour),
			},
			Score: 0.85,
		},
	}

	autoSetGoalsFromFacts(store, facts)

	goals, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
	if goals[0].Description != "Telegram 봇 에러 핸들링 개선 필요" {
		t.Errorf("unexpected description: %q", goals[0].Description)
	}
	if goals[0].Priority != autonomous.PriorityHigh {
		t.Errorf("expected high priority for importance 0.9, got %q", goals[0].Priority)
	}
}

func TestAutoSetGoalsFromFacts_SkipsLowImportance(t *testing.T) {
	store := tempGoalStore(t)
	now := time.Now()
	facts := []memory.SearchResult{
		{
			Fact: memory.Fact{
				Content:    "Some low-importance context",
				Category:   "context",
				Importance: 0.5,
				CreatedAt:  now,
			},
		},
	}

	autoSetGoalsFromFacts(store, facts)

	goals, _ := store.List()
	if len(goals) != 0 {
		t.Errorf("expected 0 goals for low importance, got %d", len(goals))
	}
}

func TestAutoSetGoalsFromFacts_SkipsPreference(t *testing.T) {
	store := tempGoalStore(t)
	now := time.Now()
	facts := []memory.SearchResult{
		{
			Fact: memory.Fact{
				Content:    "User prefers Korean responses",
				Category:   "preference",
				Importance: 0.95,
				CreatedAt:  now,
			},
		},
	}

	autoSetGoalsFromFacts(store, facts)

	goals, _ := store.List()
	if len(goals) != 0 {
		t.Errorf("expected 0 goals for preference category, got %d", len(goals))
	}
}

func TestAutoSetGoalsFromFacts_SkipsStaleFacts(t *testing.T) {
	store := tempGoalStore(t)
	facts := []memory.SearchResult{
		{
			Fact: memory.Fact{
				Content:    "Old decision that is no longer relevant",
				Category:   "decision",
				Importance: 0.9,
				CreatedAt:  time.Now().Add(-30 * 24 * time.Hour), // 30 days old
				UpdatedAt:  time.Now().Add(-30 * 24 * time.Hour),
			},
		},
	}

	autoSetGoalsFromFacts(store, facts)

	goals, _ := store.List()
	if len(goals) != 0 {
		t.Errorf("expected 0 goals for stale fact, got %d", len(goals))
	}
}

func TestAutoSetGoalsFromFacts_DeduplicatesExisting(t *testing.T) {
	store := tempGoalStore(t)
	now := time.Now()

	// Pre-create a goal with similar content.
	_, err := store.Add("Telegram 봇 에러 핸들링 개선 필요", "medium")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	facts := []memory.SearchResult{
		{
			Fact: memory.Fact{
				Content:    "Telegram 봇 에러 핸들링 개선 필요",
				Category:   "decision",
				Importance: 0.9,
				CreatedAt:  now,
			},
		},
	}

	autoSetGoalsFromFacts(store, facts)

	goals, _ := store.List()
	if len(goals) != 1 {
		t.Errorf("expected 1 goal (no duplicate), got %d", len(goals))
	}
}

func TestAutoSetGoalsFromFacts_MaxPerCycle(t *testing.T) {
	store := tempGoalStore(t)
	now := time.Now()

	// Create 5 qualifying facts, but only autoGoalMaxPerCycle should be created.
	var facts []memory.SearchResult
	for i := 0; i < 5; i++ {
		facts = append(facts, memory.SearchResult{
			Fact: memory.Fact{
				ID:         int64(i + 1),
				Content:    time.Now().Format("15:04:05.000") + " unique goal content number " + string(rune('A'+i)),
				Category:   "decision",
				Importance: 0.9,
				CreatedAt:  now,
			},
		})
	}

	autoSetGoalsFromFacts(store, facts)

	goals, _ := store.List()
	if len(goals) != autoGoalMaxPerCycle {
		t.Errorf("expected %d goals (max per cycle), got %d", autoGoalMaxPerCycle, len(goals))
	}
}

func TestAutoSetGoalsFromFacts_ContextCategory(t *testing.T) {
	store := tempGoalStore(t)
	now := time.Now()
	facts := []memory.SearchResult{
		{
			Fact: memory.Fact{
				Content:    "DGX Spark에 새 모델 배포 작업 진행 중",
				Category:   "context",
				Importance: 0.85,
				CreatedAt:  now,
			},
		},
	}

	autoSetGoalsFromFacts(store, facts)

	goals, _ := store.List()
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal for context category, got %d", len(goals))
	}
	if goals[0].Priority != autonomous.PriorityMedium {
		t.Errorf("expected medium priority for importance 0.85, got %q", goals[0].Priority)
	}
}

func TestIsGoalWorthy(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-autoGoalMaxAgeDays * 24 * time.Hour)

	tests := []struct {
		name string
		fact memory.Fact
		want bool
	}{
		{
			name: "qualifying decision",
			fact: memory.Fact{Content: "Deploy new version to production", Category: "decision", Importance: 0.9, CreatedAt: now},
			want: true,
		},
		{
			name: "wrong category",
			fact: memory.Fact{Content: "User likes dark mode", Category: "preference", Importance: 0.95, CreatedAt: now},
			want: false,
		},
		{
			name: "low importance",
			fact: memory.Fact{Content: "Minor context note", Category: "decision", Importance: 0.5, CreatedAt: now},
			want: false,
		},
		{
			name: "too short",
			fact: memory.Fact{Content: "ok", Category: "decision", Importance: 0.9, CreatedAt: now},
			want: false,
		},
		{
			name: "stale",
			fact: memory.Fact{Content: "Old decision to implement", Category: "decision", Importance: 0.9, CreatedAt: now.Add(-30 * 24 * time.Hour)},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGoalWorthy(tt.fact, cutoff)
			if got != tt.want {
				t.Errorf("isGoalWorthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFactToPriority(t *testing.T) {
	if factToPriority(0.95) != autonomous.PriorityHigh {
		t.Error("expected high for 0.95")
	}
	if factToPriority(0.85) != autonomous.PriorityMedium {
		t.Error("expected medium for 0.85")
	}
	if factToPriority(0.7) != autonomous.PriorityLow {
		t.Error("expected low for 0.7")
	}
}
