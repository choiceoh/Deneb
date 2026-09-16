package enginelive

import (
	"path/filepath"
	"testing"
	"time"
)

func ms(t time.Time) int64 { return t.UnixMilli() }

// A gateway restarted inside an outage records the down it finds again. That
// is the same outage: consecutive downs must fold into one span, and an up
// closes it.
func TestOutagesFoldConsecutiveDowns(t *testing.T) {
	base := time.Date(2026, 9, 16, 13, 17, 42, 0, time.UTC)
	trs := []Transition{
		{Endpoint: ep, AtMs: ms(base), Down: true, Reason: "connection refused"},
		{Endpoint: ep, AtMs: ms(base.Add(38 * time.Minute)), Down: true, Reason: "connection refused"}, // gateway restart
		{Endpoint: ep, AtMs: ms(base.Add(83 * time.Minute)), Down: false},
		{Endpoint: ep, AtMs: ms(base.Add(90 * time.Minute)), Down: false}, // duplicate up: ignored
		{Endpoint: ep, AtMs: ms(base.Add(100 * time.Minute)), Down: true, Reason: "health 503 draining"},
	}
	got := Outages(trs)
	if len(got) != 2 {
		t.Fatalf("outages = %+v, want 2", got)
	}
	if got[0].SinceMs != ms(base) || got[0].UntilMs != ms(base.Add(83*time.Minute)) || got[0].Reason != "connection refused" {
		t.Errorf("first outage = %+v", got[0])
	}
	if got[1].UntilMs != 0 || got[1].Reason != "health 503 draining" {
		t.Errorf("second outage must be ongoing with its own reason: %+v", got[1])
	}
	if d := got[1].Duration(ms(base.Add(110 * time.Minute))); d != 10*time.Minute {
		t.Errorf("ongoing duration = %s, want 10m", d)
	}
}

// An outage across midnight is one episode (on the day it began) whose
// seconds are split between the two days.
func TestDowntimeByDayClipsAtMidnight(t *testing.T) {
	loc := time.FixedZone("KST", 9*3600)
	start := time.Date(2026, 9, 15, 23, 30, 0, 0, loc)
	outages := []Outage{
		{SinceMs: ms(start), UntilMs: ms(start.Add(60 * time.Minute))}, // 23:30 → 00:30
		{SinceMs: ms(start.Add(2 * time.Hour))},                        // ongoing from 01:30
	}
	now := start.Add(3 * time.Hour) // 02:30 on the 16th
	got := DowntimeByDay(outages, ms(now), loc)

	d15 := got["2026-09-15"]
	if d15.Episodes != 1 || d15.Seconds != 30*60 {
		t.Errorf("15th = %+v, want 1 episode, 1800s", d15)
	}
	d16 := got["2026-09-16"]
	if d16.Episodes != 1 || d16.Seconds != 30*60+60*60 {
		t.Errorf("16th = %+v, want 1 episode (the ongoing one), 5400s", d16)
	}
}

func TestLedgerPersistsAndWindowsWithTheEntryBefore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-liveness.json")
	l := NewLedger(path)
	base := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	l.Record(Transition{Endpoint: ep, AtMs: ms(base), Down: true, Reason: "connection refused"})
	l.Record(Transition{Endpoint: ep, AtMs: ms(base.Add(time.Hour)), Down: false})
	l.Record(Transition{Endpoint: "http://10.0.0.6:8000/metrics", AtMs: ms(base.Add(2 * time.Hour)), Down: true})

	reloaded := NewLedger(path)
	got := reloaded.Transitions(ep, ms(base.Add(30*time.Minute)))
	if len(got) != 2 || !got[0].Down || got[1].Down {
		t.Fatalf("window must carry the entry before it plus the ones inside: %+v", got)
	}
	if last, ok := reloaded.Last(ep); !ok || last.Down {
		t.Errorf("Last = %+v, want the up transition", last)
	}
	if first, ok := reloaded.EarliestMs(ep); !ok || first != ms(base) {
		t.Errorf("EarliestMs = %d, want %d", first, ms(base))
	}
	if _, ok := reloaded.EarliestMs("http://10.0.0.9:8000/metrics"); ok {
		t.Error("an endpoint never seen has no earliest")
	}
}

func TestLedgerPruneKeepsTheLastEntryBeforeTheCut(t *testing.T) {
	l := NewLedger("")
	old := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	l.Record(Transition{Endpoint: ep, AtMs: ms(old), Down: false})
	l.Record(Transition{Endpoint: ep, AtMs: ms(old.Add(time.Hour)), Down: true})
	now := old.Add(45 * 24 * time.Hour)
	l.Record(Transition{Endpoint: ep, AtMs: ms(now), Down: false})
	if n := len(l.entries); n != 2 {
		t.Fatalf("entries = %d, want the pre-cut down (the outage's start) and the new up", n)
	}
	if !l.entries[0].Down {
		t.Error("the kept pre-cut entry must be the last one before the cut")
	}
}

// Nil ledgers are the no-ledger configuration; every method must be inert.
func TestNilLedgerIsInert(t *testing.T) {
	var l *Ledger
	l.Record(Transition{Endpoint: ep, AtMs: 1, Down: true})
	if _, ok := l.Last(ep); ok {
		t.Error("nil ledger has no last")
	}
	if got := l.Transitions(ep, 0); got != nil {
		t.Error("nil ledger has no transitions")
	}
	if _, ok := l.EarliestMs(ep); ok {
		t.Error("nil ledger has no earliest")
	}
}
