package handlerminiapp

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func todoDepsWithStore(t *testing.T) (TodoDeps, *localtodo.Store) {
	t.Helper()
	store, err := localtodo.New(filepath.Join(t.TempDir(), "todos.json"))
	if err != nil {
		t.Fatalf("localtodo.New: %v", err)
	}
	return TodoDeps{Store: store}, store
}

func TestTodoMethods_NilStoreSkips(t *testing.T) {
	if TodoMethods(TodoDeps{}) != nil {
		t.Error("expected nil method map when no store wired")
	}
}

func TestTodoCreateListShape(t *testing.T) {
	deps, _ := todoDepsWithStore(t)

	createResp := todoCreate(deps)(authedCtx(), reqWith(t, "miniapp.todo.create", map[string]any{
		"title": "보고서 작성",
		"note":  "Q2 매출",
		"due":   "2026-06-10T09:00:00Z",
	}))
	var created todoOut
	decode(t, createResp, &created)
	if created.ID == "" || created.Title != "보고서 작성" || created.Done {
		t.Fatalf("created = %+v", created)
	}
	if created.Due != "2026-06-10T09:00:00Z" {
		t.Errorf("due = %q, want RFC3339 UTC", created.Due)
	}

	listResp := todoList(deps)(authedCtx(), reqWith(t, "miniapp.todo.list", nil))
	var list struct {
		Todos []todoOut `json:"todos"`
	}
	decode(t, listResp, &list)
	if len(list.Todos) != 1 || list.Todos[0].Title != "보고서 작성" {
		t.Fatalf("list = %+v", list.Todos)
	}
}

func TestTodoCreateRequiresTitle(t *testing.T) {
	deps, _ := todoDepsWithStore(t)
	resp := todoCreate(deps)(authedCtx(), reqWith(t, "miniapp.todo.create", map[string]any{"note": "no title"}))
	if resp.OK {
		t.Fatal("expected error for missing title")
	}
}

func TestTodoSetDoneAndListFilter(t *testing.T) {
	deps, store := todoDepsWithStore(t)
	td, err := store.Create(localtodo.CreateInput{Title: "전화하기"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	doneResp := todoSetDone(deps)(authedCtx(), reqWith(t, "miniapp.todo.set_done", map[string]any{
		"id": td.ID, "done": true,
	}))
	var done todoOut
	decode(t, doneResp, &done)
	if !done.Done || done.DoneAt == "" {
		t.Fatalf("set_done = %+v", done)
	}

	// includeDone=false hides the completed item.
	listResp := todoList(deps)(authedCtx(), reqWith(t, "miniapp.todo.list", map[string]any{"includeDone": false}))
	var list struct {
		Todos []todoOut `json:"todos"`
	}
	decode(t, listResp, &list)
	if len(list.Todos) != 0 {
		t.Fatalf("includeDone=false returned %d, want 0", len(list.Todos))
	}
}

func TestTodoUpdatePreservesDone(t *testing.T) {
	deps, store := todoDepsWithStore(t)
	td, _ := store.Create(localtodo.CreateInput{Title: "원래"})
	if _, err := store.SetDone(td.ID, true); err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	resp := todoUpdate(deps)(authedCtx(), reqWith(t, "miniapp.todo.update", map[string]any{
		"id": td.ID, "title": "수정",
	}))
	var updated todoOut
	decode(t, resp, &updated)
	if updated.Title != "수정" || !updated.Done {
		t.Fatalf("update = %+v, want renamed + still done", updated)
	}
}

func TestTodoDelete(t *testing.T) {
	deps, store := todoDepsWithStore(t)
	td, _ := store.Create(localtodo.CreateInput{Title: "삭제 대상"})
	resp := todoDelete(deps)(authedCtx(), reqWith(t, "miniapp.todo.delete", map[string]any{"id": td.ID}))
	if !resp.OK {
		t.Fatalf("delete not OK: %s", resp.Error.Message)
	}
	if store.Get(td.ID) != nil {
		t.Error("todo present after delete")
	}
}

func TestTodoMissingIDNotFound(t *testing.T) {
	deps, _ := todoDepsWithStore(t)
	resp := todoSetDone(deps)(authedCtx(), reqWith(t, "miniapp.todo.set_done", map[string]any{"id": "todo:nope", "done": true}))
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("expected NOT_FOUND, got OK=%v err=%+v", resp.OK, resp.Error)
	}
}
