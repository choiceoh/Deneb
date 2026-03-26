package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Pilot tool: the main AI agent's fast local helper with integrated data sources.
//
// Why pilot instead of sessions_spawn?
//   - 1 call does everything: gather data + process it (vs read→pilot or exec→pilot = 2+ calls)
//   - Synchronous: instant inline result
//   - Zero overhead: no session, transcript, lifecycle
//
// Data sources (use one or combine):
//   - content: direct text
//   - file/files: read files automatically
//   - exec: run a command, process the output
//   - grep+path: search codebase, process matches
//   - url: fetch web content
//   - items: batch multiple items

const (
	pilotTimeout   = 2 * time.Minute
	pilotMaxInput  = 24000 // chars — auto-truncate beyond this
	pilotMaxTokens = 4096
	pilotExecTimeout = 15 * time.Second
)

const pilotSystemPrompt = `You are Pilot, a fast local AI assistant.
Rules:
- Execute the task directly. No preamble, no pleasantries.
- Match the user's language (Korean if Korean input, English if English).
- If output_format is "json", return valid JSON only.
- If output_format is "list", return a numbered list.
- If processing multiple items or files, handle each and label results clearly.
- Be concise. Substance over length.`

func pilotToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "What to do — free-form (e.g., '버그 찾아줘', 'summarize', '한국어로 번역')",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Direct text/code input",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Read this file and process it (relative or absolute path)",
			},
			"files": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Read multiple files and process them",
			},
			"exec": map[string]any{
				"type":        "string",
				"description": "Run this shell command and process the output",
			},
			"grep": map[string]any{
				"type":        "string",
				"description": "Grep for this pattern and process the matches",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory/file path for grep (defaults to workspace root)",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Fetch this URL and process the content",
			},
			"items": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Multiple items to process in batch",
			},
			"output_format": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "json", "list"},
				"description": "Desired output format (default: text)",
			},
		},
		"required": []string{"task"},
	}
}

// toolPilot creates the pilot ToolFunc. workspaceDir is needed for file/grep
// path resolution.
func toolPilot(workspaceDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p pilotParams
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid pilot input: %w", err)
		}
		if p.Task == "" {
			return "", fmt.Errorf("task is required")
		}

		// Phase 1: Gather data from all sources.
		gathered, err := gatherSources(ctx, p, workspaceDir)
		if err != nil {
			return "", fmt.Errorf("pilot gather: %w", err)
		}

		// Phase 2: Build prompt and call local LLM.
		userMsg := buildPilotPrompt(p.Task, p.OutputFormat, gathered)
		result, err := callLocalLLM(ctx, pilotSystemPrompt, userMsg, pilotMaxTokens)
		if err != nil {
			return "", fmt.Errorf("pilot: %w", err)
		}

		return result, nil
	}
}

// pilotParams is the parsed tool input.
type pilotParams struct {
	Task         string   `json:"task"`
	Content      string   `json:"content"`
	File         string   `json:"file"`
	Files        []string `json:"files"`
	Exec         string   `json:"exec"`
	Grep         string   `json:"grep"`
	Path         string   `json:"path"`
	URL          string   `json:"url"`
	Items        []string `json:"items"`
	OutputFormat string   `json:"output_format"`
}

// sourceBlock is a labeled chunk of gathered data.
type sourceBlock struct {
	label   string
	content string
}

// gatherSources collects data from all specified sources.
func gatherSources(ctx context.Context, p pilotParams, workspaceDir string) ([]sourceBlock, error) {
	var blocks []sourceBlock

	// Direct content.
	if p.Content != "" {
		blocks = append(blocks, sourceBlock{"input", p.Content})
	}

	// Single file.
	if p.File != "" {
		content, err := readFileForPilot(p.File, workspaceDir)
		if err != nil {
			return nil, fmt.Errorf("file %q: %w", p.File, err)
		}
		blocks = append(blocks, sourceBlock{p.File, content})
	}

	// Multiple files.
	for _, f := range p.Files {
		content, err := readFileForPilot(f, workspaceDir)
		if err != nil {
			// Non-fatal: include error message as content.
			blocks = append(blocks, sourceBlock{f, fmt.Sprintf("[error reading file: %s]", err)})
			continue
		}
		blocks = append(blocks, sourceBlock{f, content})
	}

	// Shell command.
	if p.Exec != "" {
		output, err := execForPilot(ctx, p.Exec, workspaceDir)
		if err != nil {
			blocks = append(blocks, sourceBlock{"exec: " + p.Exec, fmt.Sprintf("[command failed: %s]", err)})
		} else {
			blocks = append(blocks, sourceBlock{"exec: " + p.Exec, output})
		}
	}

	// Grep.
	if p.Grep != "" {
		searchPath := workspaceDir
		if p.Path != "" {
			searchPath = resolvePathForPilot(p.Path, workspaceDir)
		}
		output, err := grepForPilot(ctx, p.Grep, searchPath)
		if err != nil {
			blocks = append(blocks, sourceBlock{"grep: " + p.Grep, fmt.Sprintf("[grep failed: %s]", err)})
		} else {
			blocks = append(blocks, sourceBlock{"grep: " + p.Grep, output})
		}
	}

	// URL fetch.
	if p.URL != "" {
		output, err := fetchURLForPilot(ctx, p.URL)
		if err != nil {
			blocks = append(blocks, sourceBlock{p.URL, fmt.Sprintf("[fetch failed: %s]", err)})
		} else {
			blocks = append(blocks, sourceBlock{p.URL, output})
		}
	}

	// Batch items.
	for i, item := range p.Items {
		blocks = append(blocks, sourceBlock{fmt.Sprintf("item[%d]", i+1), item})
	}

	return blocks, nil
}

// buildPilotPrompt assembles the final user message from task + gathered data.
func buildPilotPrompt(task, outputFormat string, blocks []sourceBlock) string {
	var sb strings.Builder

	sb.WriteString("Task: ")
	sb.WriteString(task)

	if outputFormat != "" && outputFormat != "text" {
		sb.WriteString("\nOutput format: ")
		sb.WriteString(outputFormat)
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
		sb.WriteString(b.label)
		sb.WriteString(" ---\n")
		sb.WriteString(truncateInput(b.content, perBlock))
	}

	return sb.String()
}

// --- Data source helpers ---

func readFileForPilot(path, workspaceDir string) (string, error) {
	resolved := resolvePathForPilot(path, workspaceDir)
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func execForPilot(ctx context.Context, command, workdir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pilotExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return string(out), nil // Return output even on error (useful).
		}
		return "", err
	}
	return string(out), nil
}

func grepForPilot(ctx context.Context, pattern, searchPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pilotExecTimeout)
	defer cancel()

	args := []string{"-n", "--max-count=50", "-C1", pattern, searchPath}
	cmd := exec.CommandContext(ctx, "rg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// rg exit 1 = no matches.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return "No matches found.", nil
		}
		// Fallback to grep.
		grepArgs := []string{"-rn", "--max-count=50", "-C1", pattern, searchPath}
		cmd2 := exec.CommandContext(ctx, "grep", grepArgs...)
		out2, _ := cmd2.CombinedOutput()
		if len(out2) > 0 {
			return string(out2), nil
		}
		return "", fmt.Errorf("rg: %w", err)
	}
	return string(out), nil
}

func fetchURLForPilot(ctx context.Context, rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pilotExecTimeout)
	defer cancel()

	// Use curl + readability extraction via simple text dump.
	cmd := exec.CommandContext(ctx, "curl", "-sfL", "--max-time", "10",
		"-H", "User-Agent: Mozilla/5.0", rawURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("curl: %w", err)
	}

	// Strip HTML tags for a rough text extraction.
	text := stripHTMLTags(string(out))
	return text, nil
}

func resolvePathForPilot(path, workspaceDir string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(workspaceDir, path))
}

// truncateInput shortens input to maxChars with a notice.
func truncateInput(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("\n\n[... truncated at %d chars]", maxChars)
}

// --- Shared LLM call ---

// callLocalLLM sends a single-turn request to the local sglang server.
func callLocalLLM(ctx context.Context, system, userMessage string, maxTokens int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pilotTimeout)
	defer cancel()

	client := llm.NewClient(defaultSglangBaseURL, "", llm.WithLogger(slog.Default()))

	req := llm.ChatRequest{
		Model:     sglangModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", userMessage)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
	}

	events, err := client.StreamChatOpenAI(ctx, req)
	if err != nil {
		return "", fmt.Errorf("sglang stream: %w", err)
	}

	text, err := collectStream(ctx, events)
	if err != nil {
		return "", err
	}

	if text == "" {
		return "(no response from local model)", nil
	}
	return text, nil
}

// collectStream reads all events from a streaming LLM response and returns
// the concatenated text output.
func collectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return sb.String(), nil
			}
			return "", ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return sb.String(), nil
			}
			switch ev.Type {
			case "content_block_delta":
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
					sb.WriteString(delta.Delta.Text)
				}
			case "error":
				var errPayload struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if json.Unmarshal(ev.Payload, &errPayload) == nil && errPayload.Error.Message != "" {
					return sb.String(), fmt.Errorf("stream error: %s", errPayload.Error.Message)
				}
			}
		}
	}
}
