package server

import (
	"context"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
)

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
