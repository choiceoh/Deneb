// todo.go — miniapp.todo.* RPC handlers.
//
//   miniapp.todo.list      — all to-dos (optionally hiding completed ones)
//   miniapp.todo.create    — add a to-do
//   miniapp.todo.update    — edit a to-do's title/note/due (done state preserved)
//   miniapp.todo.set_done  — flip a to-do's completion
//   miniapp.todo.delete    — remove a to-do
//
// To-dos are the task-list companion to the local calendar: a checkable item
// with an optional due date, backed entirely by the local store (localtodo) —
// there is no external provider, so every method writes locally. A to-do whose
// due date falls in a calendar day is surfaced on that day in the native
// calendar; undated to-dos live only in the to-do list.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// LocalTodos is the subset of *localtodo.Store the handlers use. Interface-based
// for test fakes.
type LocalTodos interface {
	List() []localtodo.Todo
	Get(id string) *localtodo.Todo
	Create(in localtodo.CreateInput) (localtodo.Todo, error)
	Update(id string, in localtodo.CreateInput) (*localtodo.Todo, error)
	SetDone(id string, done bool) (*localtodo.Todo, error)
	Delete(id string) error
}

// TodoDeps wraps the local to-do store. When nil, TodoMethods returns nil and
// method_registry.go skips the domain.
type TodoDeps struct {
	Store LocalTodos
}

// todoOut is the wire shape for a to-do. Marked for Kotlin codegen so the native
// client shares this exact shape. Due/DoneAt are RFC3339 (UTC) or "" when unset.
//
//deneb:wire
type todoOut struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Note      string `json:"note,omitempty"`
	Due       string `json:"due,omitempty"`
	DueAllDay bool   `json:"dueAllDay,omitempty"`
	Done      bool   `json:"done,omitempty"`
	DoneAt    string `json:"doneAt,omitempty"`
}

// TodoMethods returns the miniapp.todo.* handler map, or nil when no store is
// wired (so method_registry.go can skip registration).
func TodoMethods(deps TodoDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.todo.list":     todoList(deps),
		"miniapp.todo.create":   todoCreate(deps),
		"miniapp.todo.update":   todoUpdate(deps),
		"miniapp.todo.set_done": todoSetDone(deps),
		"miniapp.todo.delete":   todoDelete(deps),
	}
}

// --- list ----------------------------------------------------------------

func todoList(deps TodoDeps) rpcutil.HandlerFunc {
	type params struct {
		// IncludeDone defaults to true (omitted -> show completed too). Set false
		// to return only open items.
		IncludeDone *bool `json:"includeDone,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		// Params are optional — an empty body lists everything.
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		includeDone := p.IncludeDone == nil || *p.IncludeDone
		todos := deps.Store.List()
		out := make([]todoOut, 0, len(todos))
		for _, t := range todos {
			if !includeDone && t.Done {
				continue
			}
			out = append(out, projectTodoOut(t))
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"todos": out})
	}
}

// --- create / update -----------------------------------------------------

// todoInput is the shared params shape for create/update. due is optional
// RFC3339; an empty/absent due means the to-do has no due date.
type todoInput struct {
	Title     string `json:"title"`
	Note      string `json:"note,omitempty"`
	Due       string `json:"due,omitempty"`
	DueAllDay bool   `json:"dueAllDay,omitempty"`
}

func parseTodoInput(reqID string, p todoInput) (localtodo.CreateInput, *protocol.ResponseFrame) {
	if strings.TrimSpace(p.Title) == "" {
		return localtodo.CreateInput{}, rpcerr.MissingParam("title").Response(reqID)
	}
	var due time.Time
	if s := strings.TrimSpace(p.Due); s != "" {
		var err error
		due, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return localtodo.CreateInput{}, rpcerr.InvalidParams(fmt.Errorf("due must be RFC3339: %w", err)).Response(reqID)
		}
	}
	return localtodo.CreateInput{
		Title:     p.Title,
		Note:      p.Note,
		Due:       due,
		DueAllDay: p.DueAllDay,
	}, nil
}

func todoCreate(deps TodoDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[todoInput](req)
		if errResp != nil {
			return errResp
		}
		in, errResp := parseTodoInput(req.ID, p)
		if errResp != nil {
			return errResp
		}
		td, err := deps.Store.Create(in)
		if err != nil {
			return rpcerr.WrapUnavailable("todo create failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, projectTodoOut(td))
	}
}

func todoUpdate(deps TodoDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
		todoInput
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		in, errResp := parseTodoInput(req.ID, p.todoInput)
		if errResp != nil {
			return errResp
		}
		td, err := deps.Store.Update(p.ID, in)
		if err != nil {
			if errors.Is(err, localtodo.ErrNotFound) {
				return rpcerr.NotFound("todo " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("todo update failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, projectTodoOut(*td))
	}
}

// --- set_done ------------------------------------------------------------

func todoSetDone(deps TodoDeps) rpcutil.HandlerFunc {
	type params struct {
		ID   string `json:"id"`
		Done bool   `json:"done"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		td, err := deps.Store.SetDone(p.ID, p.Done)
		if err != nil {
			if errors.Is(err, localtodo.ErrNotFound) {
				return rpcerr.NotFound("todo " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("todo set_done failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, projectTodoOut(*td))
	}
}

// --- delete --------------------------------------------------------------

func todoDelete(deps TodoDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if err := deps.Store.Delete(p.ID); err != nil {
			if errors.Is(err, localtodo.ErrNotFound) {
				return rpcerr.NotFound("todo " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("todo delete failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true})
	}
}

// --- helpers -------------------------------------------------------------

// projectTodoOut maps a domain Todo to the wire shape. formatTime (calendar.go)
// renders zero times as "" so the JSON shape stays predictable.
func projectTodoOut(t localtodo.Todo) todoOut {
	return todoOut{
		ID:        t.ID,
		Title:     t.Title,
		Note:      t.Note,
		Due:       formatTime(t.Due),
		DueAllDay: t.DueAllDay,
		Done:      t.Done,
		DoneAt:    formatTime(t.DoneAt),
	}
}
