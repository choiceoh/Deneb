package chat

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
		got := truncateForLLM(s)
		if got != s {
			t.Errorf("got %q, want %q", got, s)
		}
	})

	t.Run("exact limit unchanged", func(t *testing.T) {
		s := string(make([]rune, maxOutputRunes))
		got := truncateForLLM(s)
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
		got := truncateForLLM(s)
		if len([]rune(got)) >= len(runes) {
			t.Error("expected truncation")
		}
		if !strings.Contains(got, "omitted") {
			t.Error("expected elision marker")
		}
	})

	t.Run("korean multibyte safe", func(t *testing.T) {
		// Build string of Korean characters exceeding the limit.
		runes := make([]rune, maxOutputRunes+500)
		for i := range runes {
			runes[i] = '가'
		}
		got := truncateForLLM(string(runes))
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
		// Second call should hit cache.
		if err := validateWorkdir("/tmp"); err != nil {
			t.Errorf("unexpected error on cached call: %v", err)
		}
	})
}

func TestToolSchemas(t *testing.T) {
	// Verify all schema generators return valid structures with required fields.
	schemas := map[string]func() map[string]any{
		"exec":              execToolSchema,
		"process":           processToolSchema,
		"web":               webToolSchema,
		"youtubeTranscript": youtubeTranscriptToolSchema,
		"memorySearch":      memorySearchToolSchema,
		"message":           messageToolSchema,
		"read":              readToolSchema,
		"write":             writeToolSchema,
		"edit":              editToolSchema,
		"grep":              grepToolSchema,
		"find":              findToolSchema,
		"vega":              vegaToolSchema,
	}

	for name, fn := range schemas {
		t.Run(name, func(t *testing.T) {
			schema := fn()
			if schema == nil {
				t.Fatal("schema is nil")
			}
			if schema["type"] != "object" {
				t.Errorf("schema type = %v, want %q", schema["type"], "object")
			}
			props, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatal("schema properties is not a map")
			}
			if len(props) == 0 {
				t.Error("schema has no properties")
			}
		})
	}
}

func TestRegisterCoreTools(t *testing.T) {
	registry := NewToolRegistry()
	deps := &CoreToolDeps{
		WorkspaceDir: "/tmp/test-workspace",
	}
	RegisterCoreTools(registry, deps)

	// Verify expected tools are registered.
	expectedTools := []string{
		"read", "write", "edit", "grep", "find",
		"exec", "process", "web",
		"memory", "memory_search", "polaris", "vega", "message",
		"cron", "gateway",
		"sessions_list", "sessions_history", "sessions_search",
		"sessions_send", "sessions_spawn",
		"subagents", "image", "youtube_transcript",
	}

	registered := make(map[string]bool)
	for _, name := range registry.Names() {
		registered[name] = true
	}
	for _, name := range expectedTools {
		if !registered[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	// Verify total count.
	defs := registry.Definitions()
	if len(defs) < len(expectedTools) {
		t.Errorf("registered %d tools, expected at least %d", len(defs), len(expectedTools))
	}
}
