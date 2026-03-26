package chat

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
)

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			"5xx server error",
			&media.MediaFetchError{Code: media.ErrHTTPError, Status: 500},
			true,
		},
		{
			"503 service unavailable",
			&media.MediaFetchError{Code: media.ErrHTTPError, Status: 503},
			true,
		},
		{
			"404 not found",
			&media.MediaFetchError{Code: media.ErrHTTPError, Status: 404},
			false,
		},
		{
			"403 forbidden",
			&media.MediaFetchError{Code: media.ErrHTTPError, Status: 403},
			false,
		},
		{
			"fetch failed (connection error)",
			&media.MediaFetchError{Code: media.ErrFetchFailed, Message: "connection reset"},
			true,
		},
		{
			"max bytes exceeded",
			&media.MediaFetchError{Code: media.ErrMaxBytes},
			false,
		},
		{
			"context deadline exceeded",
			context.DeadlineExceeded,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
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
