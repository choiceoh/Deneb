package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// memoryParams is the shared parameter struct for all memory tool actions.
type memoryParams struct {
	Action     string  `json:"action"`
	Query      string  `json:"query"`
	FactID     *int64  `json:"fact_id"`
	Category   string  `json:"category"`
	Importance float64 `json:"importance"`
	Limit      int     `json:"limit"`
	Sort       string  `json:"sort"`
	Offset     int     `json:"offset"`
	Title      string  `json:"title"` // log action: entry heading
	Days       int     `json:"days"`  // recall action: how many days back (default 1 = today only)
}

// ToolMemory creates the unified memory tool that combines structured fact store
// with file-based memory search. Degrades gracefully when memory store is nil
// (falls back to file-based search only).
func ToolMemory(d *toolctx.VegaDeps, workspaceDir string, logger *slog.Logger) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p memoryParams
		if err := jsonutil.UnmarshalInto("memory params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		case "search", "":
			return memorySearch(ctx, d, workspaceDir, p)
		case "get":
			return memoryGet(ctx, d, p)
		case "set":
			return memorySet(ctx, d, p)
		case "forget":
			return memoryForget(ctx, d, p)
		case "browse":
			return memoryBrowse(ctx, d, p)
		case "recall":
			return memoryRecall(ctx, d, p, logger)
		case "status":
			return memoryStatus(ctx, d, workspaceDir)
		case "log":
			return memoryLog(workspaceDir, p)
		case "daily":
			return memoryDaily(workspaceDir, p)
		default:
			return "", fmt.Errorf("unknown action: %s (use: search, get, set, forget, recall, status, browse, log, daily)", p.Action)
		}
	}
}

func memorySearch(_ context.Context, _ *toolctx.VegaDeps, workspaceDir string, p memoryParams) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query is required for search")
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}

	// Structured memory store replaced by wiki; only file-based search remains.
	var parts []string

	if workspaceDir != "" {
		fileMatches := SearchMemoryFiles(workspaceDir, p.Query, p.Limit)
		if fileMatches == nil {
			// no files found: skip
		} else {
			for _, m := range fileMatches {
				parts = append(parts, fmt.Sprintf("- [file] %s (line %d): %s",
					m.File, m.Line, m.Snippet))
			}
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("No results found for %q.", p.Query), nil
	}

	header := fmt.Sprintf("## Memory search: %q (%d results)\n", p.Query, len(parts))
	return header + strings.Join(parts, "\n"), nil
}

func memoryGet(_ context.Context, _ *toolctx.VegaDeps, _ memoryParams) (string, error) {
	return "[memory store replaced by wiki]", nil
}

func memorySet(_ context.Context, _ *toolctx.VegaDeps, _ memoryParams) (string, error) {
	return "[memory store replaced by wiki]", nil
}

func memoryForget(_ context.Context, _ *toolctx.VegaDeps, _ memoryParams) (string, error) {
	return "[memory store replaced by wiki]", nil
}

func memoryBrowse(_ context.Context, _ *toolctx.VegaDeps, _ memoryParams) (string, error) {
	return "[memory store replaced by wiki]", nil
}

func memoryStatus(_ context.Context, _ *toolctx.VegaDeps, workspaceDir string) (string, error) {
	var sb strings.Builder
	sb.WriteString("## Memory Status\n\n")

	// Structured memory store replaced by wiki.
	sb.WriteString("### Structured Store\n- **Status**: replaced by wiki\n")

	if workspaceDir != "" {
		files := CollectMemoryFiles(workspaceDir)
		sb.WriteString(fmt.Sprintf("\n### File-based Memory\n- **Files**: %d\n", len(files)))
		for _, f := range files {
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	return sb.String(), nil
}

func memoryRecall(_ context.Context, _ *toolctx.VegaDeps, _ memoryParams, _ *slog.Logger) (string, error) {
	return "[memory store replaced by wiki]", nil
}

// ---------------------------------------------------------------------------
// Daily activity log — real-time, high-resolution narrative entries in
// memory/YYYY-MM-DD.md. Complements the SQL fact store which is too
// granular for "what happened today" retrieval.
// ---------------------------------------------------------------------------

// seoulLoc is pre-loaded Asia/Seoul timezone for daily log timestamps.
var seoulLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// SeoulLoc returns the pre-loaded Asia/Seoul timezone.
func SeoulLoc() *time.Location { return seoulLoc }

// DiaryDir is the subdirectory under memory/ where diary files are stored.
const DiaryDir = "diary"

// DiaryFilename returns the diary filename for a given date (diary-YYYY-MM-DD.md).
func DiaryFilename(dateStr string) string {
	return "diary-" + dateStr + ".md"
}

// DiaryPath returns the full path to a diary file for a given date.
func DiaryPath(workspaceDir, dateStr string) string {
	return filepath.Join(workspaceDir, "memory", DiaryDir, DiaryFilename(dateStr))
}

// memoryLog appends a timestamped narrative entry to today's diary file.
func memoryLog(workspaceDir string, p memoryParams) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query (log content) is required for log action")
	}

	now := time.Now().In(seoulLoc)
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04")

	diaryDir := filepath.Join(workspaceDir, "memory", DiaryDir)
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		return "", fmt.Errorf("log: create diary dir: %w", err)
	}
	datePath := DiaryPath(workspaceDir, dateStr)

	f, err := os.OpenFile(datePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("log: open file: %w", err)
	}
	defer f.Close()

	// Write date header if file is new/empty.
	if info, err := f.Stat(); err == nil && info.Size() == 0 {
		fmt.Fprintf(f, "# %s 일지\n\n", dateStr)
	}

	// Build entry: ### HH:MM — title (optional)\ncontent\n
	title := p.Title
	if title != "" {
		fmt.Fprintf(f, "### %s — %s\n\n%s\n\n", timeStr, title, p.Query)
	} else {
		fmt.Fprintf(f, "### %s\n\n%s\n\n", timeStr, p.Query)
	}

	rel := filepath.Join("memory", DiaryDir, DiaryFilename(dateStr))
	return fmt.Sprintf("Logged to %s at %s.", rel, timeStr), nil
}

// memoryDaily reads today's (and optionally previous days') diary files.
// days=1 returns today only; days=2 (default) returns today + yesterday, etc.
func memoryDaily(workspaceDir string, p memoryParams) (string, error) {
	days := p.Days
	if days <= 0 {
		days = 2 // default: today + yesterday
	}
	if days > 7 {
		days = 7
	}

	now := time.Now().In(seoulLoc)

	var sb strings.Builder
	found := 0

	// Read from oldest to newest so the output is chronological.
	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")
		datePath := DiaryPath(workspaceDir, dateStr)

		data, err := os.ReadFile(datePath)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}

		if found > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString(content)
		sb.WriteString("\n")
		found++
	}

	if found == 0 {
		dateRange := now.Format("2006-01-02")
		if days > 1 {
			oldest := now.AddDate(0, 0, -(days - 1))
			dateRange = oldest.Format("2006-01-02") + " ~ " + dateRange
		}
		return fmt.Sprintf("No diary entries found for %s.", dateRange), nil
	}

	return sb.String(), nil
}
