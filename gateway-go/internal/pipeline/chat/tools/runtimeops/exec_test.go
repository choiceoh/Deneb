package runtimeops

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
)

func TestFormatExecResult(t *testing.T) {
	t.Run("stdout only", func(t *testing.T) {
		r := &process.ExecResult{Stdout: "hello"}
		got := formatExecResult(r)
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("stderr appended", func(t *testing.T) {
		r := &process.ExecResult{
			Stdout: "out",
			Stderr: "err",
		}
		got := formatExecResult(r)
		if got != "out\nSTDERR:\nerr" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("error and exit code", func(t *testing.T) {
		r := &process.ExecResult{
			Error:    "timeout",
			ExitCode: 1,
		}
		got := formatExecResult(r)
		if got != "Error: timeout\nExit code: 1" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		r := &process.ExecResult{}
		got := formatExecResult(r)
		if got != "(no output)" {
			t.Errorf("got %q, want %q", got, "(no output)")
		}
	})

	t.Run("exit code only", func(t *testing.T) {
		r := &process.ExecResult{ExitCode: 127}
		got := formatExecResult(r)
		if got != "\nExit code: 127" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ring-buffer head drop is annotated", func(t *testing.T) {
		// A tail-only capture must never read as complete output (measured:
		// seq 1..300000 → the model's "first line" was 150204, unmarked).
		r := &process.ExecResult{
			Stdout:             "150204\n150205",
			StdoutDroppedBytes: 1040319,
			Stderr:             "tail of errors",
			StderrDroppedBytes: 42,
		}
		got := formatExecResult(r)
		if !strings.HasPrefix(got, "[stdout truncated: first 1040319 bytes dropped") {
			t.Errorf("missing stdout head-drop note, got %q", got)
		}
		if !strings.Contains(got, "[stderr truncated: first 42 bytes dropped") {
			t.Errorf("missing stderr head-drop note, got %q", got)
		}
		if !strings.Contains(got, "STDERR:\ntail of errors") {
			t.Errorf("stderr body lost, got %q", got)
		}
	})
}

func TestValidateWorkdir(t *testing.T) {
	t.Run("valid directory", func(t *testing.T) {
		if err := validateWorkdir("/tmp"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("nonexistent directory", func(t *testing.T) {
		if err := validateWorkdir("/nonexistent/dir/xyz"); err == nil {
			t.Error("expected error for nonexistent dir")
		}
	})
	t.Run("cached valid directory", func(t *testing.T) {
		if err := validateWorkdir("/tmp"); err != nil {
			t.Errorf("unexpected error on cached call: %v", err)
		}
	})
}

// The direct-exec fallback (nil process manager) must strip blocked env vars
// (the gateway environment carries secrets/injection vectors like LD_PRELOAD)
// and must apply the caller's env param — both were dropped before.
func TestToolExec_FallbackSanitizesEnvAndAppliesParam(t *testing.T) {
	t.Setenv("LD_PRELOAD", "/tmp/evil.so")
	tmp := t.TempDir()
	fn := ToolExec(nil, tmp)

	out := mustCallTool(t, fn, map[string]any{
		"command": `printenv LD_PRELOAD || echo BLOCKED_ABSENT`,
	})
	if !strings.Contains(out, "BLOCKED_ABSENT") {
		t.Errorf("LD_PRELOAD must be stripped in the fallback path, got: %q", out)
	}

	out = mustCallTool(t, fn, map[string]any{
		"command": `printenv DENEB_TEST_CUSTOM`,
		"env":     map[string]string{"DENEB_TEST_CUSTOM": "hello-fallback"},
	})
	if !strings.Contains(out, "hello-fallback") {
		t.Errorf("env param must reach the fallback child, got: %q", out)
	}
}
