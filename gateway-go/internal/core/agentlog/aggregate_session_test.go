package agentlog

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAggregateBySessionReturnsSortedRollupsWithinWindow(t *testing.T) {
	w := NewWriter(t.TempDir())
	now := time.Now().UnixMilli()

	end := func(session string, in, out, tools int) {
		data, _ := json.Marshal(RunEndData{InputTokens: in, OutputTokens: out, ToolCalls: tools})
		if err := w.Append(LogEntry{Ts: now, Type: TypeRunEnd, RunID: "r", Session: session, Data: data}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	end("client:main", 1000, 200, 3)
	end("client:main", 500, 100, 1)
	end("cron:daily", 9000, 300, 7)
	if err := w.Append(LogEntry{Ts: now, Type: TypeRunError, RunID: "r", Session: "client:main", Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	// An old entry outside the window must be excluded.
	oldData, _ := json.Marshal(RunEndData{InputTokens: 77777, OutputTokens: 1})
	if err := w.Append(LogEntry{Ts: now - 10*24*3600*1000, Type: TypeRunEnd, RunID: "r", Session: "cron:daily", Data: oldData}); err != nil {
		t.Fatal(err)
	}

	stats := w.AggregateBySession(now - 24*3600*1000)
	if len(stats) != 2 {
		t.Fatalf("want 2 sessions, got %d: %+v", len(stats), stats)
	}
	// Sorted by total tokens desc → cron:daily (9300) first.
	if stats[0].Session != "cron:daily" || stats[0].Runs != 1 || stats[0].InputTokens != 9000 {
		t.Errorf("top session wrong: %+v", stats[0])
	}
	main := stats[1]
	if main.Session != "client:main" || main.Runs != 2 || main.Errors != 1 ||
		main.InputTokens != 1500 || main.OutputTokens != 300 || main.ToolCalls != 4 {
		t.Errorf("client:main rollup wrong: %+v", main)
	}
	if main.LastTs != now {
		t.Errorf("LastTs = %d, want %d", main.LastTs, now)
	}
}

func TestAggregateBySession_NilAndEmpty(t *testing.T) {
	var w *Writer
	if got := w.AggregateBySession(0); got != nil {
		t.Errorf("nil writer must return nil, got %+v", got)
	}
	if got := NewWriter(t.TempDir()).AggregateBySession(0); len(got) != 0 {
		t.Errorf("empty dir must return no stats, got %+v", got)
	}
}
