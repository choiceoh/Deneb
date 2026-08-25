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
// Per-tool processors run first, then global ones.
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
// Per-tool processors run first (e.g., summarizers before trimming), then global ones.
func (r *PostProcessRegistry) Apply(ctx context.Context, toolName, output string) string {
	if processors, ok := r.perTool[toolName]; ok {
		for _, p := range processors {
			output = p(ctx, toolName, output)
		}
	}
	for _, p := range r.global {
		output = p(ctx, toolName, output)
	}
	return output
}

// --- Built-in post-processors ---

const grepMaxMatches = 200 // max match lines before summarizing

// NOTE: there is deliberately no generic output trimmer here. Size capping is
// owned by ToolRegistry.Execute (head/tail truncation + disk spillover with a
// read_spillover pointer), which runs BEFORE post-processing. A second cap in
// this chain re-trimmed the registry's already-budgeted output — the registry
// emits budget+marker chars, one marker over any equal cap — and deleted the
// spillover pointer from the middle (puppet measurement: a 2MB exec result
// reached the model as 3,069 chars with no marker while the 1MB spill file
// sat orphaned on disk). If a processor here ever needs to shrink output, it
// must preserve the read_spillover marker.

// ErrorEnricher adds actionable hints to common error patterns.
func ErrorEnricher(_ context.Context, _, output string) string {
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
// Registered as a per-tool processor for "grep".
func GrepResultSummarizer(_ context.Context, _, output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= grepMaxMatches {
		return output
	}
	kept := strings.Join(lines[:grepMaxMatches], "\n")
	return fmt.Sprintf("%s\n\n[... %d more matches omitted (total: %d lines)]", kept, len(lines)-grepMaxMatches, len(lines))
}

// StructuredFormatter pretty-prints compact JSON outputs for readability.
func StructuredFormatter(_ context.Context, _, output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || len(trimmed) > 10000 {
		return output
	}
	// Only attempt if it looks like JSON.
	if trimmed[0] != '{' && trimmed[0] != '[' {
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
func ExecAnnotator(_ context.Context, toolName, output string) string {
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
// Execution order: per-tool processors run first, then global ones. Size
// capping is NOT done here — ToolRegistry.Execute already spill+truncated the
// output to the tool's budget before post-processing (see the note above).
func RegisterDefaultPostProcessors(registry *ToolRegistry) {
	pp := NewPostProcessRegistry()

	// Global processors (run on all tools after per-tool processors).
	// 1. Compactor: strip ANSI + collapse adjacent duplicate lines (cheap,
	//    lossless, deterministic).
	pp.AddGlobal(CompactToolOutput)
	// 2. Error enrichment: adds actionable hints to error patterns.
	pp.AddGlobal(ErrorEnricher)

	// Tool-specific processors (run before global processors).
	// Summarizers are per-tool so they only run for their respective tools,
	// avoiding unnecessary function calls across all 34+ tools every turn.
	pp.Add("exec", ExecAnnotator)
	// Overflowing grep output keeps the matches relevant to the turn's request
	// instead of the first N (tool_grep_rerank.go); without a query signal it
	// degrades to the old positional cut.
	pp.Add("grep", GrepResultRelevanceSummarizer)
	// Skill-consult attribution + required-tool activation: a SKILL.md load —
	// via plain `read` (the compact index teaches exactly that path) or via
	// skills(action=read) — counts as a consult for the usage ledger, and the
	// skill's requires_tools deferred tools activate in the same step so the
	// procedure's tools are callable without a fetch_tools round-trip. See
	// tool_skill_read_consult.go and tool_skill_required_tools.go.
	pp.Add("read", NewReadSkillConsultRecorder(registry))
	pp.Add("skills", NewSkillsReadToolsActivator(registry))

	// JSON formatting for structured tools.
	for _, tool := range []string{"web", "kv", "sessions"} {
		pp.Add(tool, StructuredFormatter)
	}

	// Mutation outcome verification: surface buried in-band failures (a mutation
	// tool returning "…실패" as text with a nil error) so the agent cannot mistake
	// them for success. See tool_mutation_verify.go (research finding A).
	for _, tool := range mutationVerifyTools() {
		pp.Add(tool, MutationFailureAnnotator)
	}

	registry.SetPostProcess(pp)
}
