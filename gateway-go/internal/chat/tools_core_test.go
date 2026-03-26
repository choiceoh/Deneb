package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/process"
)

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"simple tags", "<p>hello</p>", "hello"},
		{"nested tags", "<div><span>text</span></div>", "text"},
		{"attributes", `<a href="link">click</a>`, "click"},
		{"script content", "<script>alert('x')</script>", "alert('x')"},
		{"empty", "", ""},
		{"whitespace collapse", "<p>a</p>\n\n\n\n<p>b</p>", "a\n\nb"},
		{"mixed content", "before <b>bold</b> after", "before bold after"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTMLTags(tt.input)
			if got != tt.want {
				t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

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

func TestToolSchemas(t *testing.T) {
	// Verify all schema generators return valid structures with required fields.
	schemas := map[string]func() map[string]any{
		"exec":               execToolSchema,
		"process":            processToolSchema,
		"webFetch":           webFetchToolSchema,
		"youtubeTranscript":  youtubeTranscriptToolSchema,
		"applyPatch":         applyPatchToolSchema,
		"memorySearch":       memorySearchToolSchema,
		"memoryGet":          memoryGetToolSchema,
		"message":            messageToolSchema,
		"read":               readToolSchema,
		"write":              writeToolSchema,
		"edit":               editToolSchema,
		"grep":               grepToolSchema,
		"find":               findToolSchema,
		"ls":                 lsToolSchema,
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
		"read", "write", "edit", "grep", "find", "ls",
		"exec", "process", "web_fetch",
		"memory_search", "memory_get", "message",
		"apply_patch", "web_search", "cron", "gateway",
		"sessions_list", "sessions_history", "sessions_send", "sessions_spawn",
		"subagents", "session_status", "image", "youtube_transcript", "nodes",
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
