package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
)

func TestOpenLoopsToTodos_DedupAndDue(t *testing.T) {
	store, err := localtodo.New(filepath.Join(t.TempDir(), "todos.json"))
	if err != nil {
		t.Fatalf("localtodo.New: %v", err)
	}
	loops := []wiki.OpenLoop{
		{What: "탑솔라에 견적서 발송", Who: "우리", Due: "2026-06-20", Context: "6/9 미팅에서 약속"},
		{What: "탑솔라에 견적서 발송!"}, // punctuation variant — must dedup to the same key
		{What: ""},             // empty — dropped
	}
	if added := openLoopsToTodos(store, loops, nil); added != 1 {
		t.Fatalf("want 1 new todo, got %d", added)
	}
	todos := store.List()
	if len(todos) != 1 {
		t.Fatalf("want 1 stored todo, got %d", len(todos))
	}
	td := todos[0]
	if td.Title != "탑솔라에 견적서 발송" {
		t.Errorf("title = %q", td.Title)
	}
	if td.Due.IsZero() || !td.DueAllDay {
		t.Errorf("due not parsed: %+v", td)
	}
	if td.Note == "" {
		t.Error("who/context should land in the note")
	}

	// A later cycle re-extracting the same promise adds nothing — even after
	// the user completes it (done items must not resurrect).
	if _, err := store.SetDone(td.ID, true); err != nil {
		t.Fatal(err)
	}
	if added := openLoopsToTodos(store, loops[:1], nil); added != 0 {
		t.Errorf("completed promise resurrected: added=%d", added)
	}
}

func TestTodoDeadlinesAndCombine(t *testing.T) {
	now := time.Now()
	todos := []localtodo.Todo{
		{Title: "견적 회신", Due: now.Add(3 * time.Hour)},
		{Title: "완료된 일", Due: now.Add(2 * time.Hour), Done: true},
		{Title: "기한 없음"},
	}
	dl := todoDeadlines(todos)
	if len(dl) != 1 || dl[0].Label != "견적 회신" {
		t.Fatalf("unexpected deadlines: %+v", dl)
	}

	cal := func(context.Context) autonomous.SignalInputs {
		return autonomous.SignalInputs{Now: now, Events: []autonomous.EventSignalInput{{ID: "e1", Summary: "회의", Start: now.Add(time.Hour)}}}
	}
	todo := func(context.Context) autonomous.SignalInputs {
		return autonomous.SignalInputs{Now: now, Deadlines: dl}
	}
	combined := combineSignalCollectors(cal, nil, todo)
	in := combined(context.Background())
	if len(in.Events) != 1 || len(in.Deadlines) != 1 || in.Now.IsZero() {
		t.Errorf("combine lost inputs: %+v", in)
	}
	if combineSignalCollectors(nil) != nil {
		t.Error("all-nil collectors must combine to nil")
	}

	// The engine must actually escalate on the due-soon to-do.
	rep := autonomous.DetectSignals(in, autonomous.DefaultSignalConfig())
	found := false
	for _, sg := range rep.Signals {
		if sg.Kind == autonomous.SignalDeadlineApproaching {
			found = true
		}
	}
	if !found {
		t.Errorf("deadline signal not raised: %+v", rep.Signals)
	}
}
