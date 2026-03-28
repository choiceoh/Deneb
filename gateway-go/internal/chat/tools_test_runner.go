package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- Test/Build runner tool ---
// Provides structured test results, build output, and lint/vet checks.

func testToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform",
				"enum":        []string{"run", "build", "check"},
			},
			"framework": map[string]any{
				"type":        "string",
				"description": "Build/test framework",
				"enum":        []string{"go", "cargo", "make"},
				"default":     "go",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Package path or directory (e.g. './internal/chat/...')",
			},
			"filter": map[string]any{
				"type":        "string",
				"description": "Test name filter (go: -run, cargo: --test)",
			},
			"verbose": map[string]any{
				"type":        "boolean",
				"description": "Verbose output (show all test names)",
				"default":     false,
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 120, max: 600)",
				"default":     120,
				"minimum":     10,
				"maximum":     600,
			},
			"coverage": map[string]any{
				"type":        "boolean",
				"description": "Enable coverage reporting (go only)",
				"default":     false,
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Build target (make: target name, cargo: --bin/--lib)",
			},
			"release": map[string]any{
				"type":        "boolean",
				"description": "Release build (cargo --release)",
				"default":     false,
			},
		},
		"required": []string{"action"},
	}
}

func toolTest(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p testParams
		if err := jsonutil.UnmarshalInto("test params", input, &p); err != nil {
			return "", err
		}
		if p.Framework == "" {
			p.Framework = "go"
		}

		timeout := time.Duration(p.Timeout) * time.Second
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}
		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		switch p.Action {
		case "run":
			return testRun(execCtx, defaultDir, p)
		case "build":
			return testBuild(execCtx, defaultDir, p)
		case "check":
			return testCheck(execCtx, defaultDir, p)
		default:
			return "", fmt.Errorf("unknown test action: %q", p.Action)
		}
	}
}

type testParams struct {
	Action    string  `json:"action"`
	Framework string  `json:"framework"`
	Path      string  `json:"path"`
	Filter    string  `json:"filter"`
	Verbose   bool    `json:"verbose"`
	Timeout   float64 `json:"timeout"`
	Coverage  bool    `json:"coverage"`
	Target    string  `json:"target"`
	Release   bool    `json:"release"`
}

// --- Run tests ---

func testRun(ctx context.Context, dir string, p testParams) (string, error) {
	switch p.Framework {
	case "go":
		return testRunGo(ctx, dir, p)
	case "cargo":
		return testRunCargo(ctx, dir, p)
	case "make":
		return testRunMake(ctx, dir, p)
	default:
		return "", fmt.Errorf("unsupported test framework: %q", p.Framework)
	}
}

func testRunGo(ctx context.Context, dir string, p testParams) (string, error) {
	pkgPath := p.Path
	if pkgPath == "" {
		pkgPath = "./..."
	}

	args := []string{"test", "-json"}
	if p.Verbose {
		args = append(args, "-v")
	}
	if p.Coverage {
		args = append(args, "-cover")
	}
	if p.Filter != "" {
		args = append(args, "-run", p.Filter)
	}
	if p.Timeout > 0 {
		args = append(args, fmt.Sprintf("-timeout=%ds", int(p.Timeout)))
	}
	args = append(args, pkgPath)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	// Parse JSON test events.
	results := parseGoTestJSON(string(out))

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Test Results: go test %s\n\n", pkgPath)

	// Summary line.
	fmt.Fprintf(&sb, "%s %d passed", passIcon(results.passed > 0 && results.failed == 0), results.passed)
	if results.failed > 0 {
		fmt.Fprintf(&sb, "  %s %d failed", failIcon(), results.failed)
	}
	if results.skipped > 0 {
		fmt.Fprintf(&sb, "  %s %d skipped", skipIcon(), results.skipped)
	}
	if results.elapsed > 0 {
		fmt.Fprintf(&sb, "  [%.1fs]", results.elapsed)
	}
	sb.WriteString("\n")

	// Coverage.
	if results.coverage != "" {
		fmt.Fprintf(&sb, "\nCoverage: %s\n", results.coverage)
	}

	// Failures.
	if len(results.failures) > 0 {
		sb.WriteString("\n### Failures\n\n")
		for i, f := range results.failures {
			fmt.Fprintf(&sb, "%d. %s", i+1, f.name)
			if f.pkg != "" {
				fmt.Fprintf(&sb, " (%s)", f.pkg)
			}
			sb.WriteString("\n")
			if f.output != "" {
				sb.WriteString("   ```\n")
				for _, line := range strings.Split(strings.TrimSpace(f.output), "\n") {
					fmt.Fprintf(&sb, "   %s\n", line)
				}
				sb.WriteString("   ```\n")
			}
		}
	}

	// If parsing failed entirely, include raw output.
	if results.passed == 0 && results.failed == 0 && results.skipped == 0 {
		if err != nil {
			sb.WriteString("\n### Raw Output\n\n```\n")
			rawOut := string(out)
			if len(rawOut) > 8000 {
				rawOut = rawOut[:8000] + "\n[... truncated]"
			}
			sb.WriteString(rawOut)
			sb.WriteString("\n```\n")
		}
	}

	return sb.String(), nil
}

// goTestEvent represents a single JSON event from `go test -json`.
type goTestEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

type goTestResults struct {
	passed   int
	failed   int
	skipped  int
	elapsed  float64
	coverage string
	failures []testFailure
}

type testFailure struct {
	name   string
	pkg    string
	output string
}

func parseGoTestJSON(output string) goTestResults {
	var r goTestResults
	testOutputs := make(map[string]*strings.Builder) // "pkg/TestName" -> output

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		var ev goTestEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}

		key := ev.Package + "/" + ev.Test

		switch ev.Action {
		case "pass":
			if ev.Test != "" {
				r.passed++
			} else if ev.Elapsed > r.elapsed {
				r.elapsed = ev.Elapsed
			}
		case "fail":
			if ev.Test != "" {
				r.failed++
				failure := testFailure{name: ev.Test, pkg: ev.Package}
				if sb, ok := testOutputs[key]; ok {
					failure.output = sb.String()
				}
				r.failures = append(r.failures, failure)
			} else if ev.Elapsed > r.elapsed {
				r.elapsed = ev.Elapsed
			}
		case "skip":
			if ev.Test != "" {
				r.skipped++
			}
		case "output":
			if ev.Test != "" {
				if _, ok := testOutputs[key]; !ok {
					testOutputs[key] = &strings.Builder{}
				}
				testOutputs[key].WriteString(ev.Output)
			}
			// Detect coverage output.
			if strings.Contains(ev.Output, "coverage:") {
				r.coverage = strings.TrimSpace(ev.Output)
			}
		}
	}
	return r
}

func testRunCargo(ctx context.Context, dir string, p testParams) (string, error) {
	args := []string{"test"}
	if p.Filter != "" {
		args = append(args, p.Filter)
	}
	if p.Path != "" {
		args = append(args, "--manifest-path", p.Path)
	}
	args = append(args, "--", "--color=never")

	cmd := exec.CommandContext(ctx, "cargo", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	rawOut := string(out)

	return formatCargoTestResults(rawOut, err), nil
}

// cargoResultPattern matches "test result: ok. X passed; Y failed; Z ignored".
var cargoResultPattern = regexp.MustCompile(`test result: (\w+)\.\s+(\d+) passed;\s+(\d+) failed;\s+(\d+) ignored`)

func formatCargoTestResults(output string, execErr error) string {
	var sb strings.Builder
	sb.WriteString("## Test Results: cargo test\n\n")

	match := cargoResultPattern.FindStringSubmatch(output)
	if match != nil {
		status := match[1]
		passed := match[2]
		failed := match[3]
		ignored := match[4]

		icon := passIcon(status == "ok")
		fmt.Fprintf(&sb, "%s %s passed", icon, passed)
		if failed != "0" {
			fmt.Fprintf(&sb, "  %s %s failed", failIcon(), failed)
		}
		if ignored != "0" {
			fmt.Fprintf(&sb, "  %s %s ignored", skipIcon(), ignored)
		}
		sb.WriteString("\n")
	}

	// Include output for failures.
	if execErr != nil || strings.Contains(output, "FAILED") {
		sb.WriteString("\n### Output\n\n```\n")
		if len(output) > 8000 {
			output = output[:8000] + "\n[... truncated]"
		}
		sb.WriteString(output)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}

func testRunMake(ctx context.Context, dir string, p testParams) (string, error) {
	target := p.Target
	if target == "" {
		target = "test"
	}
	cmd := exec.CommandContext(ctx, "make", target)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Make: %s\n\n", target)

	if err != nil {
		fmt.Fprintf(&sb, "%s Failed (exit code: %s)\n\n", failIcon(), exitCodeFromError(err))
	} else {
		fmt.Fprintf(&sb, "%s Success\n\n", passIcon(true))
	}

	sb.WriteString("```\n")
	rawOut := string(out)
	if len(rawOut) > 8000 {
		rawOut = rawOut[:8000] + "\n[... truncated]"
	}
	sb.WriteString(rawOut)
	sb.WriteString("\n```\n")

	return sb.String(), nil
}

// --- Build ---

func testBuild(ctx context.Context, dir string, p testParams) (string, error) {
	switch p.Framework {
	case "go":
		return testBuildGo(ctx, dir, p)
	case "cargo":
		return testBuildCargo(ctx, dir, p)
	case "make":
		return testRunMake(ctx, dir, p) // make build targets work the same way
	default:
		return "", fmt.Errorf("unsupported build framework: %q", p.Framework)
	}
}

func testBuildGo(ctx context.Context, dir string, p testParams) (string, error) {
	pkgPath := p.Path
	if pkgPath == "" {
		pkgPath = "./..."
	}

	cmd := exec.CommandContext(ctx, "go", "build", pkgPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	var sb strings.Builder
	sb.WriteString("## Build: go build\n\n")

	if err != nil {
		fmt.Fprintf(&sb, "%s Build failed\n\n", failIcon())
		sb.WriteString("```\n")
		sb.WriteString(string(out))
		sb.WriteString("\n```\n")
	} else {
		fmt.Fprintf(&sb, "%s Build succeeded\n", passIcon(true))
		if len(out) > 0 {
			sb.WriteString(string(out))
		}
	}
	return sb.String(), nil
}

func testBuildCargo(ctx context.Context, dir string, p testParams) (string, error) {
	args := []string{"build"}
	if p.Release {
		args = append(args, "--release")
	}
	if p.Target != "" {
		args = append(args, "--bin", p.Target)
	}
	if p.Path != "" {
		args = append(args, "--manifest-path", p.Path)
	}

	cmd := exec.CommandContext(ctx, "cargo", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	var sb strings.Builder
	sb.WriteString("## Build: cargo build\n\n")

	if err != nil {
		fmt.Fprintf(&sb, "%s Build failed\n\n", failIcon())
		sb.WriteString("```\n")
		rawOut := string(out)
		if len(rawOut) > 8000 {
			rawOut = rawOut[:8000] + "\n[... truncated]"
		}
		sb.WriteString(rawOut)
		sb.WriteString("\n```\n")
	} else {
		fmt.Fprintf(&sb, "%s Build succeeded\n", passIcon(true))
	}
	return sb.String(), nil
}

// --- Check (lint/vet) ---

func testCheck(ctx context.Context, dir string, p testParams) (string, error) {
	switch p.Framework {
	case "go":
		return testCheckGo(ctx, dir, p)
	case "cargo":
		return testCheckCargo(ctx, dir, p)
	default:
		return "", fmt.Errorf("unsupported check framework: %q (use go or cargo)", p.Framework)
	}
}

func testCheckGo(ctx context.Context, dir string, p testParams) (string, error) {
	pkgPath := p.Path
	if pkgPath == "" {
		pkgPath = "./..."
	}

	var sb strings.Builder
	sb.WriteString("## Check: go vet\n\n")

	cmd := exec.CommandContext(ctx, "go", "vet", pkgPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	if err != nil {
		fmt.Fprintf(&sb, "%s Issues found\n\n", failIcon())
		sb.WriteString("```\n")
		sb.WriteString(string(out))
		sb.WriteString("\n```\n")
	} else {
		fmt.Fprintf(&sb, "%s No issues found\n", passIcon(true))
	}
	return sb.String(), nil
}

func testCheckCargo(ctx context.Context, dir string, p testParams) (string, error) {
	args := []string{"clippy"}
	if p.Path != "" {
		args = append(args, "--manifest-path", p.Path)
	}
	args = append(args, "--", "-D", "warnings")

	var sb strings.Builder
	sb.WriteString("## Check: cargo clippy\n\n")

	cmd := exec.CommandContext(ctx, "cargo", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	if err != nil {
		fmt.Fprintf(&sb, "%s Issues found\n\n", failIcon())
		sb.WriteString("```\n")
		rawOut := string(out)
		if len(rawOut) > 8000 {
			rawOut = rawOut[:8000] + "\n[... truncated]"
		}
		sb.WriteString(rawOut)
		sb.WriteString("\n```\n")
	} else {
		fmt.Fprintf(&sb, "%s No issues found\n", passIcon(true))
	}
	return sb.String(), nil
}

// --- Formatting helpers ---

func passIcon(ok bool) string {
	if ok {
		return "[PASS]"
	}
	return "[FAIL]"
}

func failIcon() string {
	return "[FAIL]"
}

func skipIcon() string {
	return "[SKIP]"
}

func exitCodeFromError(err error) string {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return fmt.Sprintf("%d", exitErr.ExitCode())
	}
	return err.Error()
}
