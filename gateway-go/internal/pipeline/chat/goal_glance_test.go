package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/goals"
)

// Unit tests for the pure formatGoalGlance renderer moved with the symbol to
// internal/pipeline/chat/tools/goal_ambient_test.go. Only the NewGoalGlanceFunc
// wiring (still exported from this package) is exercised here.

func TestNewGoalGlanceFuncReturnsGoalForActiveSession(t *testing.T) {
	// Save/restore the process default so this never leaks into sibling tests.
	prev := goals.Default()
	t.Cleanup(func() { goals.SetDefault(prev) })

	store := goals.NewStore("", nil)
	store.Set("client:main", "지속 목표 X", 5)
	goals.SetDefault(store)

	fn := NewGoalGlanceFunc()
	ctx := context.Background()

	if got := fn(ctx, "client:main"); !strings.Contains(got, "지속 목표 X") {
		t.Errorf("active-goal session should surface the goal, got %q", got)
	}
	if got := fn(ctx, "client:main:other"); got != "" {
		t.Errorf("session without a goal should render \"\", got %q", got)
	}
	if got := fn(ctx, ""); got != "" {
		t.Errorf("empty session key should render \"\", got %q", got)
	}
}

func TestNewGoalGlanceFunc_NilStore(t *testing.T) {
	prev := goals.Default()
	t.Cleanup(func() { goals.SetDefault(prev) })
	goals.SetDefault(nil)
	if got := NewGoalGlanceFunc()(context.Background(), "client:main"); got != "" {
		t.Errorf("nil store should render \"\", got %q", got)
	}
}
