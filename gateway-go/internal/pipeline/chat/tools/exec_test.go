package tools

import (
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
