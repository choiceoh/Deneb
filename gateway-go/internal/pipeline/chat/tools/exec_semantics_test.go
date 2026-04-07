package tools

import "testing"

func TestInterpretExitCode(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		exitCode int
		wantErr  bool
		wantHint string
	}{
		// grep family
		{"grep no match", "grep foo bar.txt", 1, false, "(no matches found)"},
		{"grep error", "grep foo bar.txt", 2, true, ""},
		{"grep success", "grep foo bar.txt", 0, false, ""},
		{"rg no match", "rg pattern .", 1, false, "(no matches found)"},

		// diff
		{"diff differences", "diff a.txt b.txt", 1, false, "(differences found)"},
		{"diff error", "diff a.txt b.txt", 2, true, ""},

		// test/[
		{"test false", "test -f /nonexistent", 1, false, "(condition evaluated to false)"},
		{"test syntax error", "test -f", 2, true, ""},

		// find
		{"find partial", "find / -name foo", 1, false, "(partial: some paths inaccessible)"},

		// pipelines: last command determines exit code
		{"pipe grep", "cat file | grep pattern", 1, false, "(no matches found)"},
		{"pipe unknown", "grep foo | wc -l", 1, true, ""},

		// unknown command: default to error
		{"unknown cmd", "mycommand --flag", 1, true, ""},
		{"unknown cmd success", "mycommand", 0, false, ""},

		// complex commands
		{"sudo grep", "sudo grep pattern file", 1, false, "(no matches found)"},
		{"env grep", "env FOO=bar grep pattern file", 1, false, "(no matches found)"},
		{"full path grep", "/usr/bin/grep pattern file", 1, false, "(no matches found)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isErr, hint := InterpretExitCode(tt.command, tt.exitCode)
			if isErr != tt.wantErr {
				t.Errorf("isError = %v, want %v", isErr, tt.wantErr)
			}
			if hint != tt.wantHint {
				t.Errorf("hint = %q, want %q", hint, tt.wantHint)
			}
		})
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"grep foo", "grep"},
		{"cat file | grep foo", "grep"},
		{"FOO=bar grep foo", "grep"},
		{"sudo grep foo", "grep"},
		{"/usr/bin/grep foo", "grep"},
		{"env FOO=bar sudo /usr/bin/diff a b", "diff"},
		{"", ""},
		{"FOO=bar", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractBaseCommand(tt.input)
			if got != tt.want {
				t.Errorf("extractBaseCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
