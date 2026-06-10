// open_loop_sink.go — lands dream-extracted commitments in the to-do store
// and surfaces their deadlines to the heartbeat signal engine.
//
// The dreamer (domain/wiki) extracts open loops but stays decoupled from
// platform stores; this file is the thin adapter. To-dos give the loops a
// native-client surface (checkable list) for free, and the signal engine's
// existing deadline rule escalates due-soon items into the heartbeat turn.
package server

import (
	"context"
	"crypto/sha1" //nolint:gosec // G505 — non-cryptographic dedup key
	"encoding/hex"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
)

// openLoopTodoSink returns the dreamer sink: open loops become to-dos with a
// stable provenance key so repeated extraction across cycles is idempotent
// (CreateIfAbsent matches done items too, so a completed promise mentioned in
// an old diary never resurrects).
func openLoopTodoSink(logger *slog.Logger) func(ctx context.Context, loops []wiki.OpenLoop) (int, error) {
	return func(_ context.Context, loops []wiki.OpenLoop) (int, error) {
		store, err := localtodo.Default()
		if err != nil {
			return 0, err
		}
		return openLoopsToTodos(store, loops, logger), nil
	}
}

// openLoopsToTodos records loops into the given store; returns how many were new.
func openLoopsToTodos(store *localtodo.Store, loops []wiki.OpenLoop, logger *slog.Logger) int {
	added := 0
	for _, l := range loops {
		title := strings.TrimSpace(l.What)
		if title == "" {
			continue
		}
		note := strings.TrimSpace(l.Context)
		if who := strings.TrimSpace(l.Who); who != "" {
			if note != "" {
				note = who + " — " + note
			} else {
				note = who
			}
		}
		in := localtodo.CreateInput{
			Title:  title,
			Note:   note,
			Source: "dream:" + openLoopKey(l.What),
		}
		if due, err := time.ParseInLocation("2006-01-02", l.Due, time.Local); err == nil {
			in.Due = due
			in.DueAllDay = true
		}
		_, created, err := store.CreateIfAbsent(in)
		if err != nil {
			if logger != nil {
				logger.Warn("open-loop todo create failed", "title", title, "error", err)
			}
			continue
		}
		if created {
			added++
		}
	}
	return added
}

// openLoopKey normalizes a commitment phrase into a stable dedup key: case,
// whitespace, and punctuation variations of the same promise collapse to the
// same key across dream cycles.
func openLoopKey(what string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(what) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	sum := sha1.Sum([]byte(b.String())) //nolint:gosec // G401 — dedup key, not security
	return hex.EncodeToString(sum[:8])
}

// newTodoDeadlineCollector surfaces undone to-dos with due dates (dream open
// loops and user to-dos alike) as deadline inputs for the heartbeat signal
// engine. Best-effort: store errors yield no signals.
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
