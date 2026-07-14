package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestPostProcess_PreservesSpilloverMarker pins the registry↔postprocess
// contract: ToolRegistry.Execute spill+truncates BEFORE post-processing, and
// no processor in the default chain may shrink that output again — the former
// 32K OutputTrimmer re-trimmed the registry's budget+marker output and deleted
// the read_spillover pointer from the middle (2MB exec result → 3,069 chars
// with no marker while the spill file sat orphaned on disk).
func TestPostProcess_PreservesSpilloverMarker(t *testing.T) {
	pp := NewPostProcessRegistry()
	pp.AddGlobal(CompactToolOutput)
	pp.AddGlobal(ErrorEnricher)

	marker := "\n\n... [149798 lines truncated — use read_spillover(\"sp_test\") for full content] ...\n\n"
	// Registry-truncated shape: 16K head + marker + 16K tail (budget+marker
	// chars — one marker over the old trimmer's equal 32K cap). Distinct head
	// and tail lines so the duplicate-line compactor cannot collapse the body.
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "head-%03d %s\n", i, strings.Repeat("h", 70))
	}
	head := b.String()
	b.Reset()
	for i := range 200 {
		fmt.Fprintf(&b, "tail-%03d %s\n", i, strings.Repeat("t", 70))
	}
	tail := b.String()
	output := head + marker + tail

	got := pp.Apply(context.Background(), "exec", output)
	if !strings.Contains(got, "read_spillover(\"sp_test\")") {
		t.Fatalf("post-processing destroyed the spillover pointer:\n%.200s", got)
	}
}

func TestErrorEnricher_PermissionDenied(t *testing.T) {
	output := "Error: permission denied"
	result := ErrorEnricher(context.Background(), "exec", output)
	if !strings.Contains(result, "hint:") {
		t.Error("expected hint for permission denied")
	}
}

func TestErrorEnricher_CommandNotFound(t *testing.T) {
	output := "Error: bash: foo: command not found"
	result := ErrorEnricher(context.Background(), "exec", output)
	if !strings.Contains(result, "hint:") {
		t.Error("expected hint for command not found")
	}
}

func TestGrepResultSummarizerTruncatesLongOutput(t *testing.T) {
	var lines []string
	for range 300 {
		lines = append(lines, "file.go:"+strings.Repeat("x", 10))
	}
	output := strings.Join(lines, "\n")
	result := GrepResultSummarizer(context.Background(), "grep", output)
	if !strings.Contains(result, "more matches omitted") {
		t.Error("expected omission notice")
	}
}

func TestStructuredFormatter_CompactJSON(t *testing.T) {
	output := `{"key":"value","num":42}`
	result := StructuredFormatter(context.Background(), "http", output)
	if !strings.Contains(result, "\n") {
		t.Error("expected pretty-printed JSON")
	}
}

func TestExecAnnotatorFlagsFailureOnNonZeroExit(t *testing.T) {
	output := "some error\nExit code: 1"
	result := ExecAnnotator(context.Background(), "exec", output)
	if !strings.HasPrefix(result, "[command failed") {
		t.Error("expected failure annotation")
	}
}

func TestPostProcessRegistryEmitsGlobalAndToolSpecificMarkers(t *testing.T) {
	pp := NewPostProcessRegistry()

	// Global: uppercase marker.
	pp.AddGlobal(func(_ context.Context, _ string, output string) string {
		return output + " [global]"
	})

	// Tool-specific.
	pp.Add("grep", func(_ context.Context, _ string, output string) string {
		return output + " [grep-specific]"
	})

	result := pp.Apply(context.Background(), "grep", "data")
	if result != "data [grep-specific] [global]" {
		t.Errorf("unexpected result: %q", result)
	}

	// Tool without specific processor.
	result2 := pp.Apply(context.Background(), "read", "data")
	if result2 != "data [global]" {
		t.Errorf("unexpected result for read: %q", result2)
	}
}
