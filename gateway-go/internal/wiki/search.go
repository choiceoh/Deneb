package wiki

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// SearchResult is a single search hit from ripgrep.
type SearchResult struct {
	Path    string  // relative path within wiki dir
	Line    int     // 1-based line number
	Content string  // matching line content
	Score   float64 // relevance score (0-1, based on match density)
}

// Search runs a ripgrep-based full-text search across wiki pages.
// Returns matching results sorted by relevance.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	// Build ripgrep command.
	args := []string{
		"--json",
		"--ignore-case",
		"--max-count", "3", // max matches per file
		"--type", "md",
		"--no-heading",
		query,
		s.dir,
	}

	cmd := exec.CommandContext(ctx, "rg", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// ripgrep returns exit code 1 for no matches.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("wiki: rg: %w: %s", err, stderr.String())
	}

	results := parseRipgrepJSON(stdout.Bytes(), s.dir)

	// Score by match count per file.
	results = scoreAndDedup(results)

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// SearchFiles returns wiki file paths matching a query (fast, no content).
func (s *Store) SearchFiles(ctx context.Context, query string, limit int) ([]string, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	args := []string{
		"--files-with-matches",
		"--ignore-case",
		"--type", "md",
		query,
		s.dir,
	}

	cmd := exec.CommandContext(ctx, "rg", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("wiki: rg files: %w", err)
	}

	var files []string
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		path := scanner.Text()
		if rel, err := relPath(s.dir, path); err == nil {
			files = append(files, rel)
		}
		if len(files) >= limit {
			break
		}
	}

	return files, nil
}

// ripgrepMatch is a subset of ripgrep's JSON output.
type ripgrepMatch struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ripgrepMatchData struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	LineNumber int `json:"line_number"`
	Lines      struct {
		Text string `json:"text"`
	} `json:"lines"`
}

func parseRipgrepJSON(data []byte, baseDir string) []SearchResult {
	var results []SearchResult
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// ripgrep JSON lines can be long.
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		var msg ripgrepMatch
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Type != "match" {
			continue
		}

		var d ripgrepMatchData
		if err := json.Unmarshal(msg.Data, &d); err != nil {
			continue
		}

		rel, err := relPath(baseDir, d.Path.Text)
		if err != nil {
			continue
		}

		results = append(results, SearchResult{
			Path:    rel,
			Line:    d.LineNumber,
			Content: strings.TrimSpace(d.Lines.Text),
		})
	}

	return results
}

// scoreAndDedup groups results by file and scores by match density.
// Returns one result per file (best match), sorted by score descending.
func scoreAndDedup(results []SearchResult) []SearchResult {
	type fileGroup struct {
		best  SearchResult
		count int
	}
	groups := map[string]*fileGroup{}
	for _, r := range results {
		g, ok := groups[r.Path]
		if !ok {
			groups[r.Path] = &fileGroup{best: r, count: 1}
			continue
		}
		g.count++
	}

	var deduped []SearchResult
	for _, g := range groups {
		g.best.Score = float64(g.count) / 3.0 // normalize to 0-1 (max 3 matches per file)
		if g.best.Score > 1 {
			g.best.Score = 1
		}
		deduped = append(deduped, g.best)
	}

	// Sort by score descending.
	for i := 0; i < len(deduped); i++ {
		for j := i + 1; j < len(deduped); j++ {
			if deduped[j].Score > deduped[i].Score {
				deduped[i], deduped[j] = deduped[j], deduped[i]
			}
		}
	}

	return deduped
}

func relPath(base, abs string) (string, error) {
	if !strings.HasPrefix(abs, base) {
		return abs, nil
	}
	rel := strings.TrimPrefix(abs, base)
	rel = strings.TrimPrefix(rel, "/")
	return rel, nil
}
