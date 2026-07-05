package tools

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
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
