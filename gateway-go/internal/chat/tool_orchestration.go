// tool_orchestration.go implements concurrent tool execution with smart
// batching. When the LLM requests multiple tool calls in a single response,
// read-only tools can safely run in parallel while write tools must run serially.
//
// This reduces latency for multi-tool turns (common pattern: grep + read + read)
// by executing independent tools concurrently.
//
// Inspired by Claude Code's toolOrchestration.ts pattern.
package chat

import (
	"context"
	"sync"
)

// Default concurrency cap for parallel tool execution.
const defaultToolConcurrency = 10

// readOnlyTools is the set of tools safe for concurrent execution.
// These tools do not modify the filesystem or other shared state.
var readOnlyTools = map[string]bool{
	"read":      true,
	"grep":      true,
	"glob":      true,
	"find":      true,
	"tree":      true,
	"process":   true,
	"kv":        true,
	"knowledge": true,
	"memory":    true,
}

// ToolCall represents a pending tool call from the LLM.
type ToolCall struct {
	ID    string // tool_use_id
	Name  string
	Input []byte // JSON input
}

// ToolResult is the outcome of executing a tool call.
type ToolResult struct {
	ID      string
	Name    string
	Output  string
	IsError bool
}

// ToolBatch groups consecutive tool calls for execution strategy.
type ToolBatch struct {
	Calls      []ToolCall
	Concurrent bool // true if all calls in this batch are read-only
}

// PartitionToolCalls groups consecutive tool calls into batches.
// Consecutive read-only tools form concurrent batches; each write tool
// gets its own serial batch.
func PartitionToolCalls(calls []ToolCall) []ToolBatch {
	if len(calls) == 0 {
		return nil
	}

	var batches []ToolBatch
	var currentReadBatch []ToolCall

	flushReads := func() {
		if len(currentReadBatch) > 0 {
			batches = append(batches, ToolBatch{
				Calls:      currentReadBatch,
				Concurrent: true,
			})
			currentReadBatch = nil
		}
	}

	for _, call := range calls {
		if readOnlyTools[call.Name] {
			currentReadBatch = append(currentReadBatch, call)
		} else {
			flushReads()
			batches = append(batches, ToolBatch{
				Calls:      []ToolCall{call},
				Concurrent: false,
			})
		}
	}
	flushReads()

	return batches
}

// ExecuteBatch runs all tool calls in a batch, either concurrently or serially.
// The executor function is called for each tool call and must be safe for
// concurrent use when the batch is concurrent.
func ExecuteBatch(
	ctx context.Context,
	batch ToolBatch,
	executor func(ctx context.Context, call ToolCall) ToolResult,
) []ToolResult {
	if len(batch.Calls) == 0 {
		return nil
	}

	if !batch.Concurrent || len(batch.Calls) == 1 {
		// Serial execution.
		results := make([]ToolResult, len(batch.Calls))
		for i, call := range batch.Calls {
			if ctx.Err() != nil {
				results[i] = ToolResult{
					ID: call.ID, Name: call.Name,
					Output: "execution cancelled", IsError: true,
				}
				continue
			}
			results[i] = executor(ctx, call)
		}
		return results
	}

	// Concurrent execution with cap.
	results := make([]ToolResult, len(batch.Calls))
	sem := make(chan struct{}, defaultToolConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError string

	for i, call := range batch.Calls {
		// Check for sibling error cancellation.
		mu.Lock()
		hasError := firstError != ""
		mu.Unlock()
		if hasError || ctx.Err() != nil {
			results[i] = ToolResult{
				ID: call.ID, Name: call.Name,
				Output: "skipped: sibling tool error", IsError: true,
			}
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, c ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()

			result := executor(ctx, c)
			results[idx] = result

			// Sibling error cancellation: if a tool fails, skip remaining.
			if result.IsError {
				mu.Lock()
				if firstError == "" {
					firstError = result.Output
				}
				mu.Unlock()
			}
		}(i, call)
	}
	wg.Wait()

	return results
}

// IsReadOnlyTool returns true if the tool name is in the read-only set.
func IsReadOnlyTool(name string) bool {
	return readOnlyTools[name]
}
