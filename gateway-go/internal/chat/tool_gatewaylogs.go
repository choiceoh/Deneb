package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	gatewayLogPath     = "/tmp/deneb-gateway.log"
	gatewayLogMaxLines = 500
	gatewayLogDefault  = 100
)

func gatewayLogsToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lines": map[string]any{
				"type":        "integer",
				"description": "최근 N줄 반환 (기본 100, 최대 500)",
				"default":     gatewayLogDefault,
				"minimum":     1,
				"maximum":     gatewayLogMaxLines,
			},
			"level": map[string]any{
				"type":        "string",
				"description": "로그 레벨 필터: error, warn, info, debug (해당 레벨 이상만 표시)",
				"enum":        []string{"error", "warn", "info", "debug"},
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "정규식 패턴 필터 (매칭되는 줄만 표시)",
			},
			"pkg": map[string]any{
				"type":        "string",
				"description": "패키지 필터 (예: server, chat, telegram, rpc)",
			},
		},
	}
}

func toolGatewayLogs() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Lines   int    `json:"lines"`
			Level   string `json:"level"`
			Pattern string `json:"pattern"`
			Pkg     string `json:"pkg"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}

		if p.Lines <= 0 {
			p.Lines = gatewayLogDefault
		}
		if p.Lines > gatewayLogMaxLines {
			p.Lines = gatewayLogMaxLines
		}

		lines, err := readTailLines(gatewayLogPath, p.Lines+200) // read extra for filtering
		if err != nil {
			return "", fmt.Errorf("게이트웨이 로그를 읽을 수 없습니다: %w", err)
		}

		if len(lines) == 0 {
			return "게이트웨이 로그가 비어 있습니다", nil
		}

		// Apply filters.
		filtered := lines
		if p.Level != "" {
			filtered = filterByLevel(filtered, p.Level)
		}
		if p.Pkg != "" {
			pkgTag := "[" + p.Pkg + "]"
			var pkgFiltered []string
			for _, line := range filtered {
				if strings.Contains(line, pkgTag) {
					pkgFiltered = append(pkgFiltered, line)
				}
			}
			filtered = pkgFiltered
		}
		if p.Pattern != "" {
			re, err := regexp.Compile(p.Pattern)
			if err != nil {
				return "", fmt.Errorf("invalid pattern: %w", err)
			}
			var reFiltered []string
			for _, line := range filtered {
				if re.MatchString(line) {
					reFiltered = append(reFiltered, line)
				}
			}
			filtered = reFiltered
		}

		// Take last N lines after filtering.
		total := len(filtered)
		if len(filtered) > p.Lines {
			filtered = filtered[len(filtered)-p.Lines:]
		}

		// Build result.
		type result struct {
			Lines int    `json:"lines"`
			Total int    `json:"total"`
			Log   string `json:"log"`
		}
		out, _ := json.Marshal(result{
			Lines: len(filtered),
			Total: total,
			Log:   strings.Join(filtered, "\n"),
		})
		return string(out), nil
	}
}

// readTailLines reads the last N lines from a file efficiently.
func readTailLines(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var all []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(all) > maxLines {
		all = all[len(all)-maxLines:]
	}
	return all, nil
}

// filterByLevel filters log lines by minimum level.
// Console handler format: "HH:MM:SS.d LVL │ ..."
// Levels in order: DBG < INF < WRN < ERR
func filterByLevel(lines []string, minLevel string) []string {
	minRank := levelRank(minLevel)
	var filtered []string
	for _, line := range lines {
		rank := extractLevelRank(line)
		if rank >= minRank {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func levelRank(level string) int {
	switch strings.ToLower(level) {
	case "debug", "dbg":
		return 0
	case "info", "inf":
		return 1
	case "warn", "wrn", "warning":
		return 2
	case "error", "err":
		return 3
	default:
		return 0
	}
}

// extractLevelRank extracts the level rank from a console log line.
// The format is: "HH:MM:SS.d LVL │ ..." where LVL is DBG/INF/WRN/ERR.
// ANSI codes may be present, so we strip them for matching.
func extractLevelRank(line string) int {
	// Strip ANSI escape codes for matching.
	clean := stripANSI(line)

	// Look for level indicators after timestamp.
	// Format: "14:05:09.1 INF │" or "14:05:09.1 ERR │"
	if len(clean) < 15 {
		return -1 // too short, include by default
	}

	// Find the level token in the first 25 chars (after "HH:MM:SS.d ").
	prefix := clean
	if len(prefix) > 25 {
		prefix = prefix[:25]
	}
	for _, lvl := range []struct {
		tag  string
		rank int
	}{
		{"ERR", 3},
		{"WRN", 2},
		{"INF", 1},
		{"DBG", 0},
	} {
		if strings.Contains(prefix, lvl.tag) {
			return lvl.rank
		}
	}
	return -1
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
