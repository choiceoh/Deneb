package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/goals"
)

func TestFormatGoalGlance(t *testing.T) {
	st := &goals.State{
		Goal:       "탑솔라 6월 견적 정리",
		Status:     goals.StatusActive,
		TurnsUsed:  3,
		MaxTurns:   10,
		LastReason: "데이터 더 필요",
		Subgoals:   []string{"견적서 PDF", "발송 확인"},
	}
	got := formatGoalGlance(st)
	for _, want := range []string{
		"- 목표: 탑솔라 6월 견적 정리",
		"- 진행: 3/10턴 사용",
		"최근 판정: 데이터 더 필요",
		"- 완료 기준: 견적서 PDF; 발송 확인",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatGoalGlance missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatGoalGlance_NoMaxTurnsNoExtras(t *testing.T) {
	st := &goals.State{Goal: "g", Status: goals.StatusActive, TurnsUsed: 2}
	got := formatGoalGlance(st)
	if !strings.Contains(got, "- 진행: 2턴 사용") {
		t.Errorf("want unbounded-turns line, got:\n%s", got)
	}
	if strings.Contains(got, "/") || strings.Contains(got, "최근 판정") || strings.Contains(got, "완료 기준") {
		t.Errorf("unexpected optional sections in:\n%s", got)
	}
}

func TestFormatGoalGlance_EmptyGoal(t *testing.T) {
	if got := formatGoalGlance(&goals.State{Goal: "  ", Status: goals.StatusActive}); got != "" {
		t.Errorf("empty goal should render \"\", got %q", got)
	}
}

func TestNewGoalGlanceFunc(t *testing.T) {
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
