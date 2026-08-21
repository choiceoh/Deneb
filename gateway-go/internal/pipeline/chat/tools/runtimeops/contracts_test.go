package runtimeops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
)

func TestFormatExecResultJSONContract(t *testing.T) {
	r := &process.ExecResult{Stdout: "out", Stderr: "err", ExitCode: 7, RuntimeMs: 123, Error: "failed", StdoutDroppedBytes: 11, StderrDroppedBytes: 22}
	got := formatExecResultJSON(r)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"stdout": "out", "stderr": "err", "exit_code": float64(7), "runtime_ms": float64(123),
		"error": "failed", "stdout_dropped_bytes": float64(11), "stderr_dropped_bytes": float64(22),
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
	minimal := formatExecResultJSON(&process.ExecResult{})
	if strings.Contains(minimal, "dropped") || strings.Contains(minimal, `"error"`) {
		t.Fatalf("minimal has optional fields: %s", minimal)
	}
}

func TestFormatExecResultContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		name string
		r    *process.ExecResult
		want []string
		gone []string
	}{
		{name: "empty", r: &process.ExecResult{}, want: []string{"(no output)"}},
		{name: "stdout", r: &process.ExecResult{Stdout: "hello"}, want: []string{"hello"}, gone: []string{"Exit code"}},
		{name: "stderr", r: &process.ExecResult{Stderr: "warning"}, want: []string{"STDERR:\nwarning"}},
		{name: "all", r: &process.ExecResult{Stdout: "out", Stderr: "err", Error: "boom", ExitCode: 2}, want: []string{"out", "STDERR:\nerr", "Error: boom", "Exit code: 2"}},
		{name: "truncated", r: &process.ExecResult{Stdout: "tail", Stderr: "etail", StdoutDroppedBytes: 10, StderrDroppedBytes: 20}, want: []string{"first 10 bytes dropped", "first 20 bytes dropped", "tail", "etail"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := formatExecResult(tt.r)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q: %s", want, got)
				}
			}
			for _, gone := range tt.gone {
				if strings.Contains(got, gone) {
					t.Errorf("unexpected %q: %s", gone, got)
				}
			}
		})
	}
}

func TestValidateWorkdirCacheContracts(t *testing.T) {
	workdirCache = sync.Map{}
	dir := t.TempDir()
	if err := validateWorkdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	// Positive result remains valid inside the deliberate short cache TTL.
	if err := validateWorkdir(dir); err != nil {
		t.Fatalf("cached positive = %v", err)
	}
	workdirCache.Delete(dir)
	if err := validateWorkdir(dir); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing = %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Negative result is cached too.
	if err := validateWorkdir(dir); err == nil {
		t.Fatal("negative cache unexpectedly refreshed")
	}
	workdirCache.Delete(dir)
	if err := validateWorkdir(dir); err != nil {
		t.Fatalf("recreated = %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkdir(file); err == nil {
		t.Fatal("regular file accepted")
	}
}

func TestToolExecFallbackValidationStructuredAndHints(t *testing.T) {
	tool := ToolExec(nil, t.TempDir())
	if _, err := callTool(t, tool, map[string]any{}); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("missing command = %v", err)
	}
	if _, err := callTool(t, tool, map[string]any{"command": "pwd", "workdir": filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("missing workdir accepted")
	}
	out, err := callTool(t, tool, map[string]any{"command": "printf hello", "structured": true})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result["stdout"] != "hello" || result["exit_code"] != float64(0) || result["timed_out"] != false {
		t.Fatalf("structured = %#v", result)
	}
	out, err = callTool(t, tool, map[string]any{"command": "grep needle /dev/null"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no matches") || !strings.Contains(out, "exit status 1") {
		t.Fatalf("grep hint = %q", out)
	}
	blocked, err := callTool(t, tool, map[string]any{"command": "rm -rf /"})
	if err != nil || !strings.Contains(blocked, "실행 거부") {
		t.Fatalf("blocked = %q/%v", blocked, err)
	}
}

func TestToolProcessNilManagerContracts(t *testing.T) {
	tool := ToolProcess(nil)
	for _, action := range []string{"list", "poll", "log", "write", "kill", "unknown"} {
		out, err := callTool(t, tool, map[string]any{"action": action})
		if err != nil || out != "Process manager not available." {
			t.Errorf("action %s = %q/%v", action, out, err)
		}
	}
}
