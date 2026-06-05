package cron

import (
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// Writers (UpdateJobState/SetJobEnabled) must not race readers — neither the
// locked single-job read (Job) nor a reader that operates on a Load() snapshot
// without the lock (List returns Jobs directly; ListPage sorts it in place).
// Before Load returned an independent clone, the snapshot aliased s.cached and
// all of these raced its backing array. Run under -race to prove the fix.
func TestStoreConcurrentAccessNoRace(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "jobs.json"))
	for _, id := range []string{"a", "b", "c"} {
		if err := s.AddJob(StoreJob{
			ID:       id,
			Name:     id,
			Enabled:  true,
			Schedule: StoreSchedule{Kind: "every", EveryMs: 60000},
			Payload:  StorePayload{Kind: "agentTurn"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	const iters = 300
	var wg sync.WaitGroup
	wg.Add(4)

	// Writer: mutate one job's state in a loop.
	go func() {
		defer wg.Done()
		for i := range iters {
			_ = s.UpdateJobState("a", JobState{NextRunAtMs: int64(i)})
		}
	}()
	// Writer: toggle another job's enabled flag.
	go func() {
		defer wg.Done()
		for i := range iters {
			_ = s.SetJobEnabled("b", i%2 == 0)
		}
	}()
	// Reader: locked single-job lookup.
	go func() {
		defer wg.Done()
		for range iters {
			_ = s.Job("a")
		}
	}()
	// Reader: take a Load() snapshot and sort it in place (mirrors ListPage,
	// which sorts the returned Jobs slice).
	go func() {
		defer wg.Done()
		for range iters {
			sd, err := s.Load()
			if err != nil || sd == nil {
				continue
			}
			sort.SliceStable(sd.Jobs, func(a, b int) bool {
				return sd.Jobs[a].State.NextRunAtMs < sd.Jobs[b].State.NextRunAtMs
			})
		}
	}()

	wg.Wait()
}
