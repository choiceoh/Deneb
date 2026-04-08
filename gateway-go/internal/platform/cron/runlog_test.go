package cron

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestRunLogAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	rl := NewPersistentRunLog(storePath)

	// Append entries.
	for i := range 5 {
		err := rl.Append(RunLogEntry{
			Ts:     time.Now().UnixMilli() + int64(i),
			JobID:  "job-1",
			Action: "finished",
			Status: "ok",
		})
		testutil.NoError(t, err)
	}

	// Read page.
	page := rl.ReadPage("job-1", RunLogReadOpts{Limit: 10})
	if page.Total != 5 {
		t.Errorf("total = %d, want 5", page.Total)
	}
	if len(page.Entries) != 5 {
		t.Errorf("entries = %d, want 5", len(page.Entries))
	}
}

func TestRunLogStatusFilter(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	rl := NewPersistentRunLog(storePath)

	rl.Append(RunLogEntry{Ts: 1, JobID: "j", Action: "finished", Status: "ok"})
	rl.Append(RunLogEntry{Ts: 2, JobID: "j", Action: "finished", Status: "error", Error: "fail"})
	rl.Append(RunLogEntry{Ts: 3, JobID: "j", Action: "finished", Status: "ok"})

	page := rl.ReadPage("j", RunLogReadOpts{Status: "error"})
	if page.Total != 1 {
		t.Errorf("got %d, want 1 error entry", page.Total)
	}
}

func TestRunLogPagination(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	rl := NewPersistentRunLog(storePath)

	for i := range 10 {
		rl.Append(RunLogEntry{Ts: int64(i), JobID: "j", Action: "finished", Status: "ok"})
	}

	page := rl.ReadPage("j", RunLogReadOpts{Limit: 3, Offset: 0})
	if len(page.Entries) != 3 {
		t.Errorf("page 1: got %d entries, want 3", len(page.Entries))
	}
	if !page.HasMore {
		t.Error("expected hasMore")
	}
	if page.NextOffset == nil || *page.NextOffset != 3 {
		t.Error("expected nextOffset = 3")
	}
}

func TestRunLogTextSearch(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	rl := NewPersistentRunLog(storePath)

	rl.Append(RunLogEntry{Ts: 1, JobID: "j", Action: "finished", Status: "ok", Summary: "weather update"})
	rl.Append(RunLogEntry{Ts: 2, JobID: "j", Action: "finished", Status: "ok", Summary: "news digest"})

	page := rl.ReadPage("j", RunLogReadOpts{Query: "weather"})
	if page.Total != 1 {
		t.Errorf("got %d, want 1 match", page.Total)
	}
}
