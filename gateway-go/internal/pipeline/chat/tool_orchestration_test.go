package chat

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// testConcurrencySafe mirrors the original hardcoded read-only set for tests.
var testConcurrencySafe = map[string]struct{}{
	"read": {}, "grep": {}, "glob": {}, "find": {},
	"tree": {}, "process": {}, "kv": {}, "knowledge": {},
	"memory": {},
}

func testIsSafe(name string) bool { _, ok := testConcurrencySafe[name]; return ok }

func TestPartitionToolCalls(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		batches := PartitionToolCalls(nil, testIsSafe)
		if len(batches) != 0 {
			t.Error("expected empty")
		}
	})

	t.Run("all read-only", func(t *testing.T) {
		calls := []ToolCall{
			{ID: "1", Name: "read"},
			{ID: "2", Name: "grep"},
			{ID: "3", Name: "glob"},
		}
		batches := PartitionToolCalls(calls, testIsSafe)
		if len(batches) != 1 {
			t.Fatalf("expected 1 batch, got %d", len(batches))
		}
		if !batches[0].Concurrent {
			t.Error("should be concurrent")
		}
		if len(batches[0].Calls) != 3 {
			t.Errorf("expected 3 calls, got %d", len(batches[0].Calls))
		}
	})

	t.Run("mixed", func(t *testing.T) {
		calls := []ToolCall{
			{ID: "1", Name: "read"},
			{ID: "2", Name: "grep"},
			{ID: "3", Name: "edit"}, // write — breaks the batch
			{ID: "4", Name: "read"},
		}
		batches := PartitionToolCalls(calls, testIsSafe)
		if len(batches) != 3 {
			t.Fatalf("expected 3 batches, got %d", len(batches))
		}
		if !batches[0].Concurrent || len(batches[0].Calls) != 2 {
			t.Error("first batch: 2 concurrent reads")
		}
		if batches[1].Concurrent || len(batches[1].Calls) != 1 {
			t.Error("second batch: 1 serial write")
		}
		if !batches[2].Concurrent || len(batches[2].Calls) != 1 {
			t.Error("third batch: 1 concurrent read")
		}
	})

	t.Run("all writes", func(t *testing.T) {
		calls := []ToolCall{
			{ID: "1", Name: "edit"},
			{ID: "2", Name: "exec"},
		}
		batches := PartitionToolCalls(calls, testIsSafe)
		if len(batches) != 2 {
			t.Fatalf("expected 2 batches, got %d", len(batches))
		}
		for _, b := range batches {
			if b.Concurrent {
				t.Error("write batches should not be concurrent")
			}
		}
	})

	t.Run("nil checker treats all as serial", func(t *testing.T) {
		calls := []ToolCall{
			{ID: "1", Name: "read"},
			{ID: "2", Name: "grep"},
		}
		batches := PartitionToolCalls(calls, nil)
		if len(batches) != 2 {
			t.Fatalf("expected 2 serial batches, got %d", len(batches))
		}
		for _, b := range batches {
			if b.Concurrent {
				t.Error("nil checker should produce serial batches")
			}
		}
	})
}

func TestExecuteBatch_Serial(t *testing.T) {
	batch := ToolBatch{
		Calls: []ToolCall{
			{ID: "1", Name: "edit"},
			{ID: "2", Name: "exec"},
		},
		Concurrent: false,
	}

	var order []string
	executor := func(_ context.Context, call ToolCall) ToolResult {
		order = append(order, call.ID)
		return ToolResult{ID: call.ID, Name: call.Name, Output: "ok"}
	}

	results := ExecuteBatch(context.Background(), batch, executor)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if order[0] != "1" || order[1] != "2" {
		t.Errorf("serial order wrong: %v", order)
	}
}

func TestExecuteBatch_Concurrent(t *testing.T) {
	batch := ToolBatch{
		Calls: []ToolCall{
			{ID: "1", Name: "read"},
			{ID: "2", Name: "grep"},
			{ID: "3", Name: "glob"},
		},
		Concurrent: true,
	}

	var concurrent int64
	executor := func(_ context.Context, call ToolCall) ToolResult {
		atomic.AddInt64(&concurrent, 1)
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt64(&concurrent, -1)
		return ToolResult{ID: call.ID, Name: call.Name, Output: "ok"}
	}

	results := ExecuteBatch(context.Background(), batch, executor)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.IsError {
			t.Errorf("result %s should not be error", r.ID)
		}
	}
}

func TestExecuteBatch_SiblingError(t *testing.T) {
	batch := ToolBatch{
		Calls: []ToolCall{
			{ID: "1", Name: "read"},
			{ID: "2", Name: "read"},
			{ID: "3", Name: "read"},
		},
		Concurrent: true,
	}

	executor := func(_ context.Context, call ToolCall) ToolResult {
		if call.ID == "1" {
			return ToolResult{ID: call.ID, Name: call.Name, Output: "fail", IsError: true}
		}
		time.Sleep(50 * time.Millisecond) // slow — should be skipped
		return ToolResult{ID: call.ID, Name: call.Name, Output: "ok"}
	}

	results := ExecuteBatch(context.Background(), batch, executor)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].IsError {
		t.Error("first should error")
	}
}
