package pilot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ExpandShortcuts converts convenience params (file, exec, grep, find, url) into SourceSpecs.
func ExpandShortcuts(p PilotParams) []SourceSpec {
	var specs []SourceSpec

	if p.File != "" {
		specs = append(specs, SourceSpec{
			Tool:  "read",
			Input: MustJSON(map[string]any{"file_path": p.File}),
			Label: p.File,
		})
	}

	for _, f := range p.Files {
		specs = append(specs, SourceSpec{
			Tool:  "read",
			Input: MustJSON(map[string]any{"file_path": f}),
			Label: f,
		})
	}

	if p.Exec != "" {
		specs = append(specs, SourceSpec{
			Tool:  "exec",
			Input: MustJSON(map[string]any{"command": p.Exec, "timeout": 15}),
			Label: "$ " + p.Exec,
		})
	}

	if p.Grep != "" {
		grepInput := map[string]any{"pattern": p.Grep, "maxResults": 50}
		if p.Path != "" {
			grepInput["path"] = p.Path
		}
		specs = append(specs, SourceSpec{
			Tool:  "grep",
			Input: MustJSON(grepInput),
			Label: "grep: " + p.Grep,
		})
	}

	if p.Find != "" {
		findInput := map[string]any{"pattern": p.Find}
		if p.Path != "" {
			findInput["path"] = p.Path
		}
		specs = append(specs, SourceSpec{
			Tool:  "find",
			Input: MustJSON(findInput),
			Label: "find: " + p.Find,
		})
	}

	if p.URL != "" {
		specs = append(specs, SourceSpec{
			Tool:  "web_fetch",
			Input: MustJSON(map[string]any{"url": p.URL}),
			Label: p.URL,
		})
	}

	if p.HTTP != "" {
		specs = append(specs, SourceSpec{
			Tool:  "http",
			Input: MustJSON(map[string]any{"url": p.HTTP, "method": "GET"}),
			Label: "http: " + p.HTTP,
		})
	}

	if p.Diff != "" {
		diffInput := map[string]any{}
		switch p.Diff {
		case "all":
			diffInput["action"] = "all"
		case "staged":
			diffInput["action"] = "staged"
		case "unstaged":
			diffInput["action"] = "unstaged"
		default:
			// Treat as a commit hash.
			diffInput["action"] = "commit"
			diffInput["commit"] = p.Diff
		}
		specs = append(specs, SourceSpec{
			Tool:  "diff",
			Input: MustJSON(diffInput),
			Label: "diff: " + p.Diff,
		})
	}

	if p.Test != "" {
		testInput := map[string]any{"action": "run"}
		if p.Test != "all" {
			testInput["path"] = p.Test
		}
		specs = append(specs, SourceSpec{
			Tool:  "test",
			Input: MustJSON(testInput),
			Label: "test: " + p.Test,
		})
	}

	if p.Tree != "" {
		specs = append(specs, SourceSpec{
			Tool:  "tree",
			Input: MustJSON(map[string]any{"path": p.Tree, "depth": 3}),
			Label: "tree: " + p.Tree,
		})
	}

	if p.GitLog != "" {
		gitInput := map[string]any{"action": "log"}
		switch p.GitLog {
		case "oneline":
			gitInput["oneline"] = true
			gitInput["count"] = 30
		case "recent":
			gitInput["count"] = 20
		default:
			// Treat as a count if numeric, otherwise default.
			gitInput["count"] = 20
		}
		specs = append(specs, SourceSpec{
			Tool:  "git",
			Input: MustJSON(gitInput),
			Label: "git_log: " + p.GitLog,
		})
	}

	if p.Health {
		specs = append(specs, SourceSpec{
			Tool:  "health_check",
			Input: MustJSON(map[string]any{}),
			Label: "health_check",
		})
	}

	if p.KVKey != "" {
		specs = append(specs, SourceSpec{
			Tool:  "kv",
			Input: MustJSON(map[string]any{"action": "get", "key": p.KVKey}),
			Label: "kv: " + p.KVKey,
		})
	}

	if p.Memory != "" {
		specs = append(specs, SourceSpec{
			Tool:  "memory",
			Input: MustJSON(map[string]any{"action": "search", "query": p.Memory}),
			Label: "memory: " + p.Memory,
		})
	}

	if p.Gmail != "" {
		specs = append(specs, SourceSpec{
			Tool:  "gmail",
			Input: MustJSON(map[string]any{"action": "search", "query": p.Gmail}),
			Label: "gmail: " + p.Gmail,
		})
	}

	if p.YouTube != "" {
		specs = append(specs, SourceSpec{
			Tool:  "youtube_transcript",
			Input: MustJSON(map[string]any{"url": p.YouTube}),
			Label: "youtube: " + p.YouTube,
		})
	}

	if p.Polaris != "" {
		specs = append(specs, SourceSpec{
			Tool:  "polaris",
			Input: MustJSON(map[string]any{"action": "search", "query": p.Polaris}),
			Label: "polaris: " + p.Polaris,
		})
	}

	if p.Image != "" {
		specs = append(specs, SourceSpec{
			Tool:  "image",
			Input: MustJSON(map[string]any{"paths": []string{p.Image}}),
			Label: "image: " + p.Image,
		})
	}

	if p.AgentLogs != "" {
		input := map[string]any{"limit": 50}
		switch p.AgentLogs {
		case "all":
			// No filter — return recent logs.
		case "tools":
			input["type"] = "turn.tool"
		case "errors":
			input["type"] = "run.error"
		default:
			// Treat as a specific run_id.
			input["run_id"] = p.AgentLogs
		}
		specs = append(specs, SourceSpec{
			Tool:  "agent_logs",
			Input: MustJSON(input),
			Label: "agent_logs: " + p.AgentLogs,
		})
	}

	if p.GatewayLogs != "" {
		input := map[string]any{"lines": 100}
		switch p.GatewayLogs {
		case "all":
			// No filter — return recent lines.
		case "errors":
			input["level"] = "error"
		case "warnings":
			input["level"] = "warn"
		default:
			// Treat as a package name filter.
			input["pkg"] = p.GatewayLogs
		}
		specs = append(specs, SourceSpec{
			Tool:  "gateway_logs",
			Input: MustJSON(input),
			Label: "gateway_logs: " + p.GatewayLogs,
		})
	}

	return specs
}

// --- Source execution ---

// SourceTypeFromTool maps tool name to source type for smart truncation.
func SourceTypeFromTool(tool string) string {
	switch tool {
	case "read":
		return "file"
	case "exec":
		return "exec"
	case "grep":
		return "grep"
	case "find":
		return "find"
	case "web_fetch":
		return "url"
	case "diff", "tree":
		return "file"
	case "agent_logs", "gateway_logs", "test", "http":
		return "exec"
	case "gmail", "youtube_transcript", "polaris", "image":
		return "content"
	default:
		return "content"
	}
}

// ExecuteSources runs source tool calls via the ToolExecutor.
// Unconditional sources (no only_if/skip_if) run in parallel with per-source timeout.
// Conditional sources run sequentially after, evaluating their conditions.
func ExecuteSources(ctx context.Context, sources []SourceSpec, tools ToolExecutor) []SourceResult {
	if len(sources) == 0 {
		return nil
	}

	results := make([]SourceResult, len(sources))

	// Split into unconditional and conditional.
	type indexedSource struct {
		idx int
		src SourceSpec
	}
	var unconditional, conditional []indexedSource
	for i, src := range sources {
		label := src.Label
		if label == "" {
			label = fmt.Sprintf("%s[%d]", src.Tool, i+1)
			sources[i].Label = label
		}
		if src.OnlyIf != "" || src.SkipIf != "" {
			conditional = append(conditional, indexedSource{i, src})
		} else {
			unconditional = append(unconditional, indexedSource{i, src})
		}
	}

	// Phase 1: Run unconditional sources in parallel.
	var wg sync.WaitGroup
	for _, is := range unconditional {
		if is.src.Tool == "pilot" {
			results[is.idx] = SourceResult{
				Label:      is.src.Label,
				Content:    "[error: pilot cannot call itself]",
				SourceType: "content",
			}
			continue
		}
		wg.Add(1)
		go func(idx int, s SourceSpec) {
			defer wg.Done()
			srcCtx, srcCancel := context.WithTimeout(ctx, sourceTimeout)
			defer srcCancel()
			output, err := tools.Execute(srcCtx, s.Tool, s.Input)
			if err != nil {
				results[idx] = SourceResult{s.Label, fmt.Sprintf("[tool error: %s]", err), SourceTypeFromTool(s.Tool)}
				return
			}
			results[idx] = SourceResult{s.Label, output, SourceTypeFromTool(s.Tool)}
		}(is.idx, is.src)
	}
	wg.Wait()

	// Phase 2: Run conditional sources sequentially.
	for _, is := range conditional {
		src := is.src
		if src.Tool == "pilot" {
			results[is.idx] = SourceResult{src.Label, "[error: pilot cannot call itself]", "content"}
			continue
		}
		if src.OnlyIf != "" && !sourceSucceeded(results, src.OnlyIf) {
			results[is.idx] = SourceResult{src.Label, fmt.Sprintf("[skipped: %q did not succeed]", src.OnlyIf), "content"}
			continue
		}
		if src.SkipIf != "" && sourceSucceeded(results, src.SkipIf) {
			results[is.idx] = SourceResult{src.Label, fmt.Sprintf("[skipped: %q succeeded]", src.SkipIf), "content"}
			continue
		}
		srcCtx, srcCancel := context.WithTimeout(ctx, sourceTimeout)
		output, err := tools.Execute(srcCtx, src.Tool, src.Input)
		srcCancel()
		if err != nil {
			results[is.idx] = SourceResult{src.Label, fmt.Sprintf("[tool error: %s]", err), SourceTypeFromTool(src.Tool)}
			continue
		}
		results[is.idx] = SourceResult{src.Label, output, SourceTypeFromTool(src.Tool)}
	}

	return results
}

// sourceSucceeded checks if a source with the given label has a non-empty, non-error result.
func sourceSucceeded(results []SourceResult, label string) bool {
	for _, r := range results {
		if r.Label == label {
			return r.Content != "" && !strings.HasPrefix(r.Content, "[tool error:") && !strings.HasPrefix(r.Content, "[skipped:")
		}
	}
	return false
}

// --- Prompt building ---

// BuildPilotPrompt assembles the user message from task + gathered data.
func BuildPilotPrompt(task, outputFormat, maxLength string, blocks []SourceResult) string {
	var sb strings.Builder

	sb.WriteString("Task: ")
	sb.WriteString(task)

	if outputFormat != "" && outputFormat != "text" {
		sb.WriteString("\nOutput format: ")
		sb.WriteString(outputFormat)
	}

	if maxLength != "" && maxLength != "normal" {
		sb.WriteString("\nOutput length: ")
		switch maxLength {
		case "brief":
			sb.WriteString("Keep response under 500 characters. Be extremely concise.")
		case "detailed":
			sb.WriteString("Provide thorough, detailed analysis.")
		}
	}

	if len(blocks) == 0 {
		return sb.String()
	}

	// Budget per block to stay within total limit.
	perBlock := pilotMaxInput
	if len(blocks) > 1 {
		perBlock = pilotMaxInput / len(blocks)
		if perBlock < 2000 {
			perBlock = 2000
		}
	}

	for _, b := range blocks {
		sb.WriteString("\n\n--- ")
		sb.WriteString(b.Label)
		sb.WriteString(" ---\n")
		sb.WriteString(SmartTruncate(b.Content, perBlock, b.SourceType))
	}

	return sb.String()
}

// --- Smart truncation ---

// SmartTruncate truncates content based on source type:
//   - file: preserves beginning (60%) + end (40%) for code context
//   - exec: preserves end (80%) — errors/results at the bottom
//   - default: simple head truncation
func SmartTruncate(s string, maxChars int, sourceType string) string {
	if len(s) <= maxChars {
		return s
	}

	marker := fmt.Sprintf("\n\n[... truncated, original %d chars ...]\n\n", len(s))

	budget := maxChars - len(marker)
	if budget < 200 {
		// Not enough room for head+tail split — fall back to simple head truncation.
		return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
	}

	switch sourceType {
	case "file":
		// Preserve start + end for file content (function signatures + tail).
		headSize := budget * 6 / 10
		tailSize := budget - headSize
		// Ensure head+tail don't exceed content length (when s is only slightly over maxChars).
		if headSize+tailSize >= len(s) {
			return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
		}
		return s[:headSize] + marker + s[len(s)-tailSize:]

	case "exec":
		// Preserve end for command output (errors/results typically at bottom).
		headSize := budget * 2 / 10
		if headSize < 200 {
			headSize = 200
		}
		tailSize := budget - headSize
		if headSize+tailSize >= len(s) {
			return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
		}
		return s[:headSize] + marker + s[len(s)-tailSize:]

	default:
		return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
	}
}

// TruncateInput is a simple head-only truncation.
func TruncateInput(s string, maxChars int) string {
	return TruncateHead(s, maxChars)
}

// TruncateHead is a simple head-only truncation (used for chain prompts, fallback).
func TruncateHead(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
}

// MustJSON marshals v to JSON, panicking on error.
func MustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
