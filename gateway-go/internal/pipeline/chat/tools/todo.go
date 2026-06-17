package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolTodo manages the user's to-do list — the SAME localtodo store the native
// client's 할일 screen reads via miniapp.todo.*. Use THIS (not heartbeat_update)
// when the user asks to add / complete / remove / list a 할일: heartbeat_update is
// the agent's own free-form work memo (HEARTBEAT.md), whereas this is the user's
// structured, checkable task list — so a to-do added here shows up on the user's
// device and any client that reads miniapp.todo.list.
func ToolTodo() ToolFunc { return toolTodoWithStore(nil) }

// toolTodoWithStore is the testable variant: a nil store falls back to the
// process-wide localtodo.Default() for production; tests pass an isolated store.
func toolTodoWithStore(store *localtodo.Store) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Title  string `json:"title"`
			ID     string `json:"id"`
			Done   *bool  `json:"done"`
			Due    string `json:"due"`
		}
		if err := jsonutil.UnmarshalInto("todo params", input, &p); err != nil {
			return "", err
		}

		s := store
		if s == nil {
			var derr error
			if s, derr = localtodo.Default(); derr != nil {
				return "", fmt.Errorf("todo: store unavailable: %w", derr)
			}
		}

		switch strings.ToLower(strings.TrimSpace(p.Action)) {
		case "", "list":
			return formatTodoList(s.List()), nil

		case "add", "create":
			title := strings.TrimSpace(p.Title)
			if title == "" {
				return "", fmt.Errorf("todo add: title required")
			}
			in := localtodo.CreateInput{Title: title}
			if d := parseTodoDue(p.Due); !d.IsZero() {
				in.Due = d
				in.DueAllDay = true
			}
			td, cerr := s.Create(in)
			if cerr != nil {
				return "", cerr
			}
			return fmt.Sprintf("할일 추가됨: %q (id=%s)", td.Title, td.ID), nil

		case "done", "complete", "set_done":
			done := true
			if p.Done != nil {
				done = *p.Done
			}
			td, serr := s.SetDone(strings.TrimSpace(p.ID), done)
			if serr != nil {
				return "", serr
			}
			state := "완료"
			if !done {
				state = "미완료"
			}
			return fmt.Sprintf("할일 %s 처리: %q", state, td.Title), nil

		case "delete", "remove":
			if derr := s.Delete(strings.TrimSpace(p.ID)); derr != nil {
				return "", derr
			}
			return fmt.Sprintf("할일 삭제됨 (id=%s)", p.ID), nil

		default:
			return "", fmt.Errorf("todo: unknown action %q (list|add|done|delete)", p.Action)
		}
	}
}

func formatTodoList(todos []localtodo.Todo) string {
	if len(todos) == 0 {
		return "할일 없음."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "할일 %d건:\n", len(todos))
	for _, t := range todos {
		mark := " "
		if t.Done {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s (id=%s)", mark, t.Title, t.ID)
		if !t.Due.IsZero() {
			fmt.Fprintf(&b, " · 마감 %s", t.Due.Format("2006-01-02"))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// parseTodoDue accepts an RFC3339 timestamp or a bare YYYY-MM-DD date; anything
// else (or empty) yields the zero time, i.e. no due date.
func parseTodoDue(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, perr := time.Parse(layout, s); perr == nil {
			return t
		}
	}
	return time.Time{}
}
