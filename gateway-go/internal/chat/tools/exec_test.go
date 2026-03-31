package tools

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/process"
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

func TestTruncateForLLM(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		s := "hello world"
		got := TruncateForLLM(s)
		if got != s {
			t.Errorf("got %q, want %q", got, s)
		}
	})

	t.Run("exact limit unchanged", func(t *testing.T) {
		s := string(make([]rune, maxOutputRunes))
		got := TruncateForLLM(s)
		if len([]rune(got)) != maxOutputRunes {
			t.Errorf("expected %d runes, got %d", maxOutputRunes, len([]rune(got)))
		}
	})

	t.Run("over limit truncated with marker", func(t *testing.T) {
		runes := make([]rune, maxOutputRunes+1000)
		for i := range runes {
			runes[i] = 'A'
		}
		s := string(runes)
		got := TruncateForLLM(s)
		if len([]rune(got)) >= len(runes) {
			t.Error("expected truncation")
		}
		if !strings.Contains(got, "omitted") {
			t.Error("expected elision marker")
		}
	})

	t.Run("korean multibyte safe", func(t *testing.T) {
		runes := make([]rune, maxOutputRunes+500)
		for i := range runes {
			runes[i] = '가'
		}
		got := TruncateForLLM(string(runes))
		if !strings.Contains(got, "omitted") {
			t.Error("expected elision marker for Korean text")
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
