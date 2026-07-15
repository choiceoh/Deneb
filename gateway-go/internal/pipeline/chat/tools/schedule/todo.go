package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolTodo manages the user's to-do list — the SAME localtodo store the native
// client's 할일 screen reads via miniapp.todo.*. Use THIS (not heartbeat_update)
// when the user asks to add / complete / remove / list a 할일: heartbeat_update is
// the agent's own free-form work memo (HEARTBEAT.md), whereas this is the user's
// structured, checkable task list — so a to-do added here shows up on the user's
// device and any client that reads miniapp.todo.list.
func ToolTodo() toolport.ToolFunc { return toolTodoWithStore(nil) }

// toolTodoWithStore is the testable variant: a nil store falls back to the
// process-wide localtodo.Default() for production; tests pass an isolated store.
func toolTodoWithStore(store *localtodo.Store) toolport.ToolFunc {
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
				return fmt.Sprintf("할일을 찾지 못했습니다 (id=%s): %s. `todo(action=\"list\")`로 현재 id를 확인한 뒤 다시 시도하세요.", p.ID, serr), nil
			}
			state := "완료"
			if !done {
				state = "미완료"
			}
			return fmt.Sprintf("할일 %s 처리: %q", state, td.Title), nil

		case "delete", "remove":
			if derr := s.Delete(strings.TrimSpace(p.ID)); derr != nil {
				return fmt.Sprintf("할일을 찾지 못했습니다 (id=%s): %s. `todo(action=\"list\")`로 현재 id를 확인한 뒤 다시 시도하세요.", p.ID, derr), nil
			}
			return fmt.Sprintf("할일 삭제됨 (id=%s)", p.ID), nil

		default:
			return "", fmt.Errorf("todo: unknown action %q (list|add|done|delete)", p.Action)
		}
	}
}

// formatTodoList renders the list deadline-first: undone items sorted by due
// (dated before undated), completed items last — and flags overdue items with
// D+N. The whole value of a task list is surfacing deadlines; store order
// buried them and "마감 2026-06-01" read the same whether past or future.
func formatTodoList(todos []localtodo.Todo) string {
	// dentime honors the configured timezone (DENEB_TIMEZONE) — a bare
	// time.Now() on a UTC host would shift the D+N overdue math by a day.
	return formatTodoListAt(todos, dentime.Now())
}

func formatTodoListAt(todos []localtodo.Todo, now time.Time) string {
	if len(todos) == 0 {
		return "할일 없음."
	}
	sorted := append([]localtodo.Todo(nil), todos...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Done != b.Done {
			return !a.Done // undone first
		}
		if a.Due.IsZero() != b.Due.IsZero() {
			return !a.Due.IsZero() // dated before undated
		}
		if !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		return false
	})

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var b strings.Builder
	fmt.Fprintf(&b, "할일 %d건:\n", len(sorted))
	for _, t := range sorted {
		mark := " "
		if t.Done {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s (id=%s)", mark, t.Title, t.ID)
		if !t.Due.IsZero() {
			fmt.Fprintf(&b, " · 마감 %s", t.Due.Format("2006-01-02"))
			if !t.Done {
				due := time.Date(t.Due.Year(), t.Due.Month(), t.Due.Day(), 0, 0, 0, 0, now.Location())
				if days := int(today.Sub(due).Hours() / 24); days > 0 {
					fmt.Fprintf(&b, " ⚠️ 지남 D+%d", days)
				}
			}
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
