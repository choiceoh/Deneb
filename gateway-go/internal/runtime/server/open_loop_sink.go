// open_loop_sink.go — heartbeat to-do deadline signals.
//
// The open-loop → to-do auto-creation was removed (2026-06-21): commitments are
// no longer silently recorded as to-dos (operator approval first). No to-do sink
// is wired into the dreamer anymore. What remains is the read-only projection of
// undone, dated to-dos onto the heartbeat signal engine's deadline inputs, plus
// the small collector-merge helper the heartbeat uses.
package server

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
)

// newTodoDeadlineCollector surfaces undone to-dos with due dates as deadline
// inputs for the heartbeat signal engine. Best-effort: store errors yield no
// signals.
func newTodoDeadlineCollector() func(ctx context.Context) autonomous.SignalInputs {
	return func(context.Context) autonomous.SignalInputs {
		in := autonomous.SignalInputs{Now: time.Now()}
		store, err := localtodo.Default()
		if err != nil {
			return in
		}
		in.Deadlines = todoDeadlines(store.List())
		return in
	}
}

// todoDeadlines projects undone, dated to-dos onto the engine's input shape.
func todoDeadlines(todos []localtodo.Todo) []autonomous.DeadlineSignalInput {
	var out []autonomous.DeadlineSignalInput
	for _, t := range todos {
		if t.Done || t.Due.IsZero() {
			continue
		}
		out = append(out, autonomous.DeadlineSignalInput{Label: t.Title, Due: t.Due})
	}
	return out
}

// combineSignalCollectors merges several collectors into one SignalInputs
// snapshot (slices appended; Now taken from the first collector that sets it).
// nil collectors are skipped; returns nil when none remain.
func combineSignalCollectors(collectors ...func(ctx context.Context) autonomous.SignalInputs) func(ctx context.Context) autonomous.SignalInputs {
	var active []func(ctx context.Context) autonomous.SignalInputs
	for _, c := range collectors {
		if c != nil {
			active = append(active, c)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return func(ctx context.Context) autonomous.SignalInputs {
		var merged autonomous.SignalInputs
		for _, c := range active {
			in := c(ctx)
			if merged.Now.IsZero() {
				merged.Now = in.Now
			}
			merged.Mail = append(merged.Mail, in.Mail...)
			merged.Events = append(merged.Events, in.Events...)
			merged.Deadlines = append(merged.Deadlines, in.Deadlines...)
		}
		return merged
	}
}
