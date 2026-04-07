package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolSearchAndRead combines grep and read: searches for a pattern, then
// automatically reads the matching files with context around match locations.
func ToolSearchAndRead(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern      string `json:"pattern"`
			Path         string `json:"path"`
			Include      string `json:"include"`
			FileType     string `json:"fileType"`
			ContextLines int    `json:"context_lines"`
			MaxFiles     int    `json:"max_files"`
		}
		if err := jsonutil.UnmarshalInto("search_and_read params", input, &p); err != nil {
			return "", err
		}
		if p.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		contextLines := p.ContextLines
		if contextLines <= 0 {
			contextLines = 10
		}
		if contextLines > 50 {
			contextLines = 50
		}
		maxFiles := p.MaxFiles
		if maxFiles <= 0 {
			maxFiles = 5
		}
		if maxFiles > 40 {
			maxFiles = 40
		}

		searchPath := defaultDir
		if p.Path != "" {
			searchPath = ResolvePath(p.Path, defaultDir)
		}

		// Step 1: Run ripgrep to find matches with file:line format.
		// Use -e to avoid flag confusion when pattern starts with '-'.
		args := []string{"-n", "--max-count=20", "--no-heading", "-e", p.Pattern}
		if p.Include != "" {
			for _, glob := range splitGlobs(p.Include) {
				args = append(args, "--glob", glob)
			}
		}
		if p.FileType != "" {
			args = append(args, "--type", normalizeFileType(p.FileType))
		}
		args = append(args, "--", searchPath)

		out, _, err := runRg(ctx, args)
		if err != nil {
			exitCode := rgExitCode(err)
			if exitCode == 1 {
				return "No matches found.", nil
			}
			// If stdout has valid match lines despite the error, use them.
			if len(out) > 0 && hasGrepMatches(out) { //nolint:gocritic // ifElseChain — grep retry logic
				// Continue to step 2 with partial results.
			} else if exitCode == 2 {
				// Retry 1: treat pattern as literal string (-F).
				fixedArgs := make([]string, len(args))
				copy(fixedArgs, args)
				fixedArgs = append([]string{"-F"}, fixedArgs...)
				if retryOut, _, retryErr := runRg(ctx, fixedArgs); retryErr == nil {
					out = retryOut
				} else if rgExitCode(retryErr) == 1 { //nolint:gocritic // ifElseChain — grep retry cascade
					return "No matches found.", nil
				} else if p.FileType != "" {
					// Retry 2: strip --type (commonly unrecognized), keep -F.
					bareArgs := stripRgFlag(fixedArgs, "--type")
					if bareOut, _, bareErr := runRg(ctx, bareArgs); bareErr == nil {
						out = bareOut
					} else if rgExitCode(bareErr) == 1 {
						return "No matches found.", nil
					} else {
						return "", fmt.Errorf("grep failed: %s", strings.TrimSpace(string(out)))
					}
				} else {
					// Last resort: bare search, no type/glob/regex.
					bareMinArgs := []string{"-F", "-n", "--max-count=20", "--no-heading", "-e", p.Pattern, "--", searchPath}
					if bareMinOut, _, bareMinErr := runRg(ctx, bareMinArgs); bareMinErr == nil {
						out = bareMinOut
					} else if rgExitCode(bareMinErr) == 1 {
						return "No matches found.", nil
					} else {
						return "", fmt.Errorf("grep failed (rg %s): %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
					}
				}
			} else {
				// Non-regex error. Try bare minimum search.
				bareMinArgs := []string{"-F", "-n", "--max-count=20", "--no-heading", "-e", p.Pattern, "--", searchPath}
				if bareMinOut, _, bareMinErr := runRg(ctx, bareMinArgs); bareMinErr == nil {
					out = bareMinOut
				} else if rgExitCode(bareMinErr) == 1 {
					return "No matches found.", nil
				} else {
					return "", fmt.Errorf("grep failed (rg %s): %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
				}
			}
		}

		// Step 2: Parse results into file → line numbers map.
		type fileMatch struct {
			path  string
			lines []int
		}
		fileMap := make(map[string]*fileMatch)
		var fileOrder []string

		lineRe := regexp.MustCompile(`^(.+?):(\d+):`)
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			m := lineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			filePath := m[1]
			lineNum, _ := strconv.Atoi(m[2])

			if _, exists := fileMap[filePath]; !exists {
				fileMap[filePath] = &fileMatch{path: filePath}
				fileOrder = append(fileOrder, filePath)
			}
			fileMap[filePath].lines = append(fileMap[filePath].lines, lineNum)
		}

		if len(fileOrder) == 0 {
			return "No matches found.", nil
		}

		// Step 3: Read files in parallel (up to max_files), then assemble in order.
		filesToRead := fileOrder
		if len(filesToRead) > maxFiles {
			filesToRead = filesToRead[:maxFiles]
		}

		type fileResult struct {
			output string
			err    error
		}
		fileResults := make([]fileResult, len(filesToRead))

		var wg sync.WaitGroup
		wg.Add(len(filesToRead))
		for i, filePath := range filesToRead {
			go func(idx int, fp string) {
				defer wg.Done()
				fm := fileMap[fp]
				data, err := os.ReadFile(fp)
				if err != nil {
					fileResults[idx] = fileResult{err: err}
					return
				}

				lines := strings.Split(string(data), "\n")
				totalLines := len(lines)

				displayPath := fp
				if rel, relErr := filepath.Rel(defaultDir, fp); relErr == nil {
					displayPath = rel
				}

				ranges := mergeRanges(fm.lines, contextLines, totalLines)

				var fsb strings.Builder
				fmt.Fprintf(&fsb, "[File: %s | %d lines | matches at lines: %v]\n",
					displayPath, totalLines, fm.lines)

				for ri, r := range ranges {
					if ri > 0 {
						fsb.WriteString("  ...\n")
					}
					for j := r.start; j <= r.end && j < totalLines; j++ {
						marker := " "
						for _, ml := range fm.lines {
							if j+1 == ml {
								marker = ">"
								break
							}
						}
						fmt.Fprintf(&fsb, "%s%6d\t%s\n", marker, j+1, lines[j])
					}
				}
				fileResults[idx] = fileResult{output: fsb.String()}
			}(i, filePath)
		}
		wg.Wait()

		// Assemble results in original order.
		var sb strings.Builder
		fmt.Fprintf(&sb, "[search_and_read: pattern=%q, %d files matched", p.Pattern, len(fileOrder))
		if len(fileOrder) > maxFiles {
			fmt.Fprintf(&sb, ", showing first %d", maxFiles)
		}
		sb.WriteString("]\n")

		for i, r := range fileResults {
			sb.WriteString("\n---\n")
			if r.err != nil {
				fmt.Fprintf(&sb, "[Error reading %s: %s]\n", filesToRead[i], r.err.Error())
				continue
			}
			sb.WriteString(r.output)
		}

		if len(fileOrder) > maxFiles {
			fmt.Fprintf(&sb, "\n[... %d more files not shown. Increase max_files to see more.]\n",
				len(fileOrder)-maxFiles)
		}

		return TruncateForLLM(sb.String()), nil
	}
}

type lineRange struct {
	start, end int
}

// mergeRanges builds non-overlapping line ranges around match locations.
func mergeRanges(matchLines []int, surrounding, totalLines int) []lineRange {
	if len(matchLines) == 0 {
		return nil
	}

	sort.Ints(matchLines)

	var ranges []lineRange
	for _, ml := range matchLines {
		// Convert to 0-based index.
		start := ml - 1 - surrounding
		end := ml - 1 + surrounding
		if start < 0 {
			start = 0
		}
		if end >= totalLines {
			end = totalLines - 1
		}

		// Merge with previous range if overlapping.
		if len(ranges) > 0 && start <= ranges[len(ranges)-1].end+1 {
			ranges[len(ranges)-1].end = end
		} else {
			ranges = append(ranges, lineRange{start, end})
		}
	}
	return ranges
}
