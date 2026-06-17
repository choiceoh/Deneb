package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
)

func TestToolTodo_CRUD(t *testing.T) {
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

func TestToolTodo_AddRequiresTitle(t *testing.T) {
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
