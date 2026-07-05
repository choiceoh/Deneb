package toolctx

import (
	"context"
	"sync"
	"testing"
)

func TestToolExecStats_RecordAndSnapshot(t *testing.T) {
	s := NewToolExecStats()
	if got := s.RepairedCounts(); got != nil {
		t.Fatalf("empty stats should return nil, got %v", got)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.RecordRepaired("wiki")
		}()
	}
	wg.Wait()
	s.RecordRepaired("exec")

	counts := s.RepairedCounts()
	if counts["wiki"] != 10 || counts["exec"] != 1 {
		t.Fatalf("counts = %v, want wiki:10 exec:1", counts)
	}

	// Snapshot is a copy — mutating it must not affect the collector.
	counts["wiki"] = 999
	if got := s.RepairedCounts()["wiki"]; got != 10 {
		t.Fatalf("collector mutated via snapshot: %d", got)
	}
}

func TestToolExecStats_NilSafe(t *testing.T) {
	var s *ToolExecStats
	s.RecordRepaired("wiki") // must not panic
	if got := s.RepairedCounts(); got != nil {
		t.Fatalf("nil stats should return nil, got %v", got)
	}
	// Absent from context → nil-safe chain.
	ToolExecStatsFromContext(context.Background()).RecordRepaired("exec")
}
