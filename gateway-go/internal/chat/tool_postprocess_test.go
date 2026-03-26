package chat

import (
	"context"
	"strings"
	"testing"
)

func TestOutputTrimmer_Short(t *testing.T) {
	output := "short output"
	result := OutputTrimmer(context.Background(), "test", output)
	if result != output {
		t.Error("expected no change for short output")
	}
}

func TestOutputTrimmer_Long(t *testing.T) {
	output := strings.Repeat("x", 70000)
	result := OutputTrimmer(context.Background(), "test", output)
	if len(result) >= len(output) {
		t.Error("expected trimmed output to be shorter")
	}
	if !strings.Contains(result, "trimmed") {
		t.Error("expected trimmed marker in output")
	}
}

func TestErrorEnricher_NoError(t *testing.T) {
	output := "all good"
	result := ErrorEnricher(context.Background(), "exec", output)
	if result != output {
		t.Error("expected no change for non-error output")
	}
}

func TestErrorEnricher_PermissionDenied(t *testing.T) {
	output := "Error: permission denied"
	result := ErrorEnricher(context.Background(), "exec", output)
	if !strings.Contains(result, "hint:") {
		t.Error("expected hint for permission denied")
	}
}

func TestErrorEnricher_CommandNotFound(t *testing.T) {
	output := "Error: bash: foo: command not found"
	result := ErrorEnricher(context.Background(), "exec", output)
	if !strings.Contains(result, "hint:") {
		t.Error("expected hint for command not found")
	}
}

func TestGrepResultSummarizer_Short(t *testing.T) {
	output := "file.go:10:match1\nfile.go:20:match2"
	result := GrepResultSummarizer(context.Background(), "grep", output)
	if result != output {
		t.Error("expected no change for short grep output")
	}
}

func TestGrepResultSummarizer_Long(t *testing.T) {
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "file.go:"+strings.Repeat("x", 10))
	}
	output := strings.Join(lines, "\n")
	result := GrepResultSummarizer(context.Background(), "grep", output)
	if !strings.Contains(result, "more matches omitted") {
		t.Error("expected omission notice")
	}
}

func TestGrepResultSummarizer_WrongTool(t *testing.T) {
	output := strings.Repeat("line\n", 300)
	result := GrepResultSummarizer(context.Background(), "read", output)
	if result != output {
		t.Error("expected no change for non-grep tool")
	}
}

func TestFindResultSummarizer_Short(t *testing.T) {
	output := "src/a.go\nsrc/b.go"
	result := FindResultSummarizer(context.Background(), "find", output)
	if result != output {
		t.Error("expected no change for short find output")
	}
}

func TestStructuredFormatter_CompactJSON(t *testing.T) {
	output := `{"key":"value","num":42}`
	result := StructuredFormatter(context.Background(), "http", output)
	if !strings.Contains(result, "\n") {
		t.Error("expected pretty-printed JSON")
	}
}

func TestStructuredFormatter_AlreadyPretty(t *testing.T) {
	output := "{\n  \"key\": \"value\"\n}"
	result := StructuredFormatter(context.Background(), "http", output)
	if result != output {
		t.Error("expected no change for already pretty JSON")
	}
}

func TestStructuredFormatter_NonJSON(t *testing.T) {
	output := "just plain text"
	result := StructuredFormatter(context.Background(), "http", output)
	if result != output {
		t.Error("expected no change for non-JSON")
	}
}

func TestExecAnnotator_NoExitCode(t *testing.T) {
	output := "hello world"
	result := ExecAnnotator(context.Background(), "exec", output)
	if result != output {
		t.Error("expected no change without exit code")
	}
}

func TestExecAnnotator_NonZeroExit(t *testing.T) {
	output := "some error\nExit code: 1"
	result := ExecAnnotator(context.Background(), "exec", output)
	if !strings.HasPrefix(result, "[command failed") {
		t.Error("expected failure annotation")
	}
}

func TestPostProcessRegistry_Apply(t *testing.T) {
	pp := NewPostProcessRegistry()

	// Global: uppercase marker.
	pp.AddGlobal(func(_ context.Context, _ string, output string) string {
		return output + " [global]"
	})

	// Tool-specific.
	pp.Add("grep", func(_ context.Context, _ string, output string) string {
		return output + " [grep-specific]"
	})

	result := pp.Apply(context.Background(), "grep", "data")
	if result != "data [global] [grep-specific]" {
		t.Errorf("unexpected result: %q", result)
	}

	// Tool without specific processor.
	result2 := pp.Apply(context.Background(), "read", "data")
	if result2 != "data [global]" {
		t.Errorf("unexpected result for read: %q", result2)
	}
}
