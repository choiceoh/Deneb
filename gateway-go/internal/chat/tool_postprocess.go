package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Pre-compiled regex for ExecAnnotator (avoid re-compiling on every tool call).
var exitCodeRe = regexp.MustCompile(`Exit code: (\d+)`)

// PostProcessor transforms a tool's output after execution.
// Returning the input unchanged is valid (no-op).
type PostProcessor func(ctx context.Context, toolName string, output string) string

// PostProcessRegistry holds per-tool and global post-processors.
// Global processors run first, then tool-specific ones.
type PostProcessRegistry struct {
	perTool map[string][]PostProcessor
	global  []PostProcessor
}

// NewPostProcessRegistry creates an empty registry.
func NewPostProcessRegistry() *PostProcessRegistry {
	return &PostProcessRegistry{
		perTool: make(map[string][]PostProcessor),
	}
}

// AddGlobal registers a post-processor that runs on all tool outputs.
func (r *PostProcessRegistry) AddGlobal(p PostProcessor) {
	r.global = append(r.global, p)
}

// Add registers a post-processor for a specific tool.
func (r *PostProcessRegistry) Add(toolName string, p PostProcessor) {
	r.perTool[toolName] = append(r.perTool[toolName], p)
}

// Apply runs all applicable post-processors on the output.
func (r *PostProcessRegistry) Apply(ctx context.Context, toolName string, output string) string {
	for _, p := range r.global {
		output = p(ctx, toolName, output)
	}
	if processors, ok := r.perTool[toolName]; ok {
		for _, p := range processors {
			output = p(ctx, toolName, output)
		}
	}
	return output
}

// --- Built-in post-processors ---

const (
	outputTrimMax     = 64000 // chars — hard cap for any tool output
	outputTrimPreview = 2000  // chars preserved from head and tail when trimming
	grepMaxMatches    = 200   // max match lines before summarizing
	findMaxEntries    = 500   // max find results before summarizing
)

// OutputTrimmer caps output at outputTrimMax chars, preserving head and tail.
func OutputTrimmer(_ context.Context, _ string, output string) string {
	if len(output) <= outputTrimMax {
		return output
	}
	head := output[:outputTrimPreview]
	tail := output[len(output)-outputTrimPreview:]
	return fmt.Sprintf("%s\n\n[... trimmed %d chars — showing first and last %d chars ...]\n\n%s",
		head, len(output), outputTrimPreview, tail)
}

// ErrorEnricher adds actionable hints to common error patterns.
func ErrorEnricher(_ context.Context, _ string, output string) string {
	if !strings.Contains(output, "Error:") && !strings.Contains(output, "STDERR:") {
		return output
	}

	hints := []struct {
		pattern string
		hint    string
	}{
		{"permission denied", "hint: check file permissions or try with elevated privileges"},
		{"command not found", "hint: the command may not be installed or not in PATH"},
		{"no such file or directory", "hint: verify the file path exists (use find or ls)"},
		{"connection refused", "hint: the target service may not be running"},
		{"ENOSPC", "hint: disk space may be full"},
	}

	lower := strings.ToLower(output)
	for _, h := range hints {
		if strings.Contains(lower, h.pattern) {
			return output + "\n\n" + h.hint
		}
	}
	return output
}

// GrepResultSummarizer caps grep output and adds match count summary.
func GrepResultSummarizer(_ context.Context, toolName string, output string) string {
	if toolName != "grep" {
		return output
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= grepMaxMatches {
		return output
	}
	kept := strings.Join(lines[:grepMaxMatches], "\n")
	return fmt.Sprintf("%s\n\n[... %d more matches omitted (total: %d lines)]", kept, len(lines)-grepMaxMatches, len(lines))
}

// FindResultSummarizer caps find output and groups by directory.
func FindResultSummarizer(_ context.Context, toolName string, output string) string {
	if toolName != "find" {
		return output
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) <= findMaxEntries {
		return output
	}

	// Group by top-level directory for a summary.
	dirCounts := make(map[string]int)
	for _, line := range lines {
		parts := strings.SplitN(strings.TrimSpace(line), "/", 3)
		dir := "."
		if len(parts) >= 2 {
			dir = parts[0] + "/" + parts[1]
		}
		dirCounts[dir]++
	}

	var sb strings.Builder
	sb.WriteString(strings.Join(lines[:findMaxEntries], "\n"))
	sb.WriteString(fmt.Sprintf("\n\n[... %d more files omitted (total: %d)]", len(lines)-findMaxEntries, len(lines)))
	sb.WriteString("\n\nDirectory summary:")
	for dir, count := range dirCounts {
		fmt.Fprintf(&sb, "\n  %s: %d files", dir, count)
	}
	return sb.String()
}

// StructuredFormatter pretty-prints compact JSON outputs for readability.
func StructuredFormatter(_ context.Context, _ string, output string) string {
	trimmed := strings.TrimSpace(output)
	if len(trimmed) == 0 || len(trimmed) > 10000 {
		return output
	}
	// Only attempt if it looks like JSON.
	if (trimmed[0] != '{' && trimmed[0] != '[') {
		return output
	}
	var parsed any
	if json.Unmarshal([]byte(trimmed), &parsed) != nil {
		return output
	}
	// Already pretty-printed (has newlines) — skip.
	if strings.Contains(trimmed, "\n") {
		return output
	}
	formatted, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return output
	}
	return string(formatted)
}

// ExecAnnotator adds a structured header to exec results with metadata.
func ExecAnnotator(_ context.Context, toolName string, output string) string {
	if toolName != "exec" {
		return output
	}
	// Only annotate if there's an exit code (non-zero indicates failure).
	if !strings.Contains(output, "Exit code:") {
		return output
	}
	// Extract exit code for emphasis.
	if matches := exitCodeRe.FindStringSubmatch(output); len(matches) == 2 && matches[1] != "0" {
		return fmt.Sprintf("[command failed with exit code %s]\n%s", matches[1], output)
	}
	return output
}

// RegisterDefaultPostProcessors sets up the standard post-processing pipeline.
// Execution order: global processors run first (in registration order), then
// per-tool processors. Tool-specific summarizers (grep, find) are registered
// as global so they run BEFORE OutputTrimmer — summarizing 10K lines down to
// 200 is far cheaper than trimming 100K to 64K first and then summarizing.
func RegisterDefaultPostProcessors(registry *ToolRegistry) {
	pp := NewPostProcessRegistry()

	// Global processors (run on all tools, in registration order).
	// 1. Summarizers first: reduce tool-specific output before generic trimming.
	pp.AddGlobal(GrepResultSummarizer)
	pp.AddGlobal(FindResultSummarizer)
	// 2. Generic trimmer: caps any remaining large output at 64K chars.
	pp.AddGlobal(OutputTrimmer)
	// 3. Error enrichment: adds actionable hints to error patterns.
	pp.AddGlobal(ErrorEnricher)

	// Tool-specific processors.
	pp.Add("exec", ExecAnnotator)

	// JSON formatting for structured tools.
	for _, tool := range []string{"http", "kv", "sessions_list"} {
		pp.Add(tool, StructuredFormatter)
	}

	registry.SetPostProcess(pp)
}
