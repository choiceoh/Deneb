package schedule

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
)

// The list renders deadline-first (undone by due, dated before undated, done
// last) and flags overdue undone items with D+N.
func TestFormatTodoList_SortAndOverdue(t *testing.T) {
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.FixedZone("KST", 9*3600))
	day := func(d string) time.Time {
		tm, _ := time.Parse("2006-01-02", d)
		return tm
	}
	todos := []localtodo.Todo{
		{ID: "1", Title: "완료된 것", Done: true, Due: day("2026-07-01")},
		{ID: "2", Title: "마감 없음"},
		{ID: "3", Title: "다음주 마감", Due: day("2026-07-10")},
		{ID: "4", Title: "지난 마감", Due: day("2026-07-02")},
	}
	out := formatTodoListAt(todos, now)

	iOverdue := strings.Index(out, "지난 마감")
	iNext := strings.Index(out, "다음주 마감")
	iNoDue := strings.Index(out, "마감 없음")
	iDone := strings.Index(out, "완료된 것")
	if !(iOverdue < iNext && iNext < iNoDue && iNoDue < iDone) {
		t.Errorf("order wrong (want overdue < upcoming < undated < done):\n%s", out)
	}
	if !strings.Contains(out, "지남 D+3") {
		t.Errorf("expected overdue flag D+3 for 07-02 at 07-05:\n%s", out)
	}
	if strings.Contains(out, "완료된 것 (id=1) · 마감 2026-07-01 ⚠️") {
		t.Errorf("done items must not be flagged overdue:\n%s", out)
	}
}

func TestToolTodo_CreatesUpdatesAndDeletesTodo(t *testing.T) {
	store, err := localtodo.New(filepath.Join(t.TempDir(), "todos.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	fn := toolTodoWithStore(store)

	call := func(args map[string]any) string {
		t.Helper()
		b, _ := json.Marshal(args)
		out, cerr := fn(context.Background(), b)
		if cerr != nil {
			t.Fatalf("todo %v: %v", args, cerr)
		}
		return out
	}

	// add — lands in the localtodo store (the same store miniapp.todo.list reads)
	if out := call(map[string]any{"action": "add", "title": "테스트 할일"}); !strings.Contains(out, "추가됨") {
		t.Fatalf("add output: %q", out)
	}
	if got := store.List(); len(got) != 1 || got[0].Title != "테스트 할일" {
		t.Fatalf("store after add: %+v", got)
	}

	// list — surfaces the item as text for the agent
	if out := call(map[string]any{"action": "list"}); !strings.Contains(out, "테스트 할일") {
		t.Fatalf("list output: %q", out)
	}

	id := store.List()[0].ID

	// done — flips completion in the store
	if out := call(map[string]any{"action": "done", "id": id}); !strings.Contains(out, "완료") {
		t.Fatalf("done output: %q", out)
	}
	if !store.List()[0].Done {
		t.Fatal("todo not marked done in store")
	}

	// delete — removes from the store
	call(map[string]any{"action": "delete", "id": id})
	if got := store.List(); len(got) != 0 {
		t.Fatalf("store after delete: %+v", got)
	}
}

func TestToolTodo_AddReturnsErrorWithoutTitle(t *testing.T) {
	store, err := localtodo.New(filepath.Join(t.TempDir(), "todos.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	fn := toolTodoWithStore(store)
	b, _ := json.Marshal(map[string]any{"action": "add"})
	if _, err := fn(context.Background(), b); err == nil {
		t.Fatal("expected error for add without title")
	}
}
