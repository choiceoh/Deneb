// check-composite-action-input-interpolation scans GitHub Actions composite
// action files and fails if any `run:` block directly interpolates
// `${{ inputs.* }}`.  Use env: and reference shell variables instead.
//
// Usage: go run scripts/check-composite-action-input-interpolation.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	inputInterpolationRE = regexp.MustCompile(`\$\{\{\s*inputs\.`)
	runLineRE            = regexp.MustCompile(`^(\s*)run:\s*(.*)$`)
	usingCompositeRE     = regexp.MustCompile(`(?m)^\s*using:\s*composite\s*$`)
)

type violation struct {
	file string
	line int
	text string
}

func indentation(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func scanFile(path string) ([]violation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)

	if !usingCompositeRE.MatchString(text) {
		return nil, nil
	}

	lines := strings.Split(text, "\n")
	var violations []violation
	i := 0

	for i < len(lines) {
		line := lines[i]
		m := runLineRE.FindStringSubmatch(line)
		if m == nil {
			i++
			continue
		}

		runIndent := len(m[1])
		runValue := strings.TrimSpace(m[2])
		lineNo := i + 1

		if runValue != "" && runValue[0] != '|' && runValue[0] != '>' {
			if inputInterpolationRE.MatchString(runValue) {
				violations = append(violations, violation{path, lineNo, strings.TrimSpace(line)})
			}
			i++
			continue
		}

		i++
		for i < len(lines) {
			scriptLine := lines[i]
			if strings.TrimSpace(scriptLine) == "" {
				i++
				continue
			}
			if indentation(scriptLine) <= runIndent {
				break
			}
			if inputInterpolationRE.MatchString(scriptLine) {
				violations = append(violations, violation{path, i + 1, strings.TrimSpace(scriptLine)})
			}
			i++
		}
	}

	return violations, nil
}

func main() {
	root := filepath.Join(".github", "actions")
	var allViolations []violation

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		base := strings.TrimSuffix(filepath.Base(path), ext)
		if base != "action" || (ext != ".yml" && ext != ".yaml") {
			return nil
		}
		vs, scanErr := scanFile(path)
		if scanErr != nil {
			return scanErr
		}
		allViolations = append(allViolations, vs...)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking %s: %v\n", root, err)
		os.Exit(1)
	}

	if len(allViolations) > 0 {
		fmt.Println("Disallowed direct inputs interpolation in composite run blocks:")
		for _, v := range allViolations {
			fmt.Printf("- %s:%d: %s\n", v.file, v.line, v.text)
		}
		fmt.Println("Use env: and reference shell variables instead.")
		os.Exit(1)
	}

	fmt.Println("No direct inputs interpolation found in composite run blocks.")
}
