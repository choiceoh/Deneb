package agent

import (
	"encoding/json"
	"testing"
)

func TestIsReadOnlyExecCommand(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]any
		wantOK bool
	}{
		// Simple read-only binaries.
		{"ls", m("command", "ls -la"), true},
		{"cat file", m("command", "cat main.go"), true},
		{"grep pattern", m("command", "grep -rn TODO ."), true},
		{"head", m("command", "head -20 file.go"), true},
		{"wc", m("command", "wc -l *.go"), true},
		{"find", m("command", "find . -name '*.go'"), true},
		{"diff", m("command", "diff a.go b.go"), true},
		{"echo", m("command", "echo hello"), true},
		{"ps", m("command", "ps aux"), true},
		{"env", m("command", "env"), true},

		// Compound commands with safe subcommands.
		{"go test", m("command", "go test ./..."), true},
		{"go vet", m("command", "go vet ./..."), true},
		{"go build", m("command", "go build -v ./..."), true},
		{"go list", m("command", "go list -m all"), true},
		{"cargo test", m("command", "cargo test --workspace"), true},
		{"cargo check", m("command", "cargo check"), true},
		{"cargo clippy", m("command", "cargo clippy --workspace"), true},
		{"git status", m("command", "git status"), true},
		{"git log", m("command", "git log --oneline -10"), true},
		{"git diff", m("command", "git diff HEAD~1"), true},
		{"git blame", m("command", "git blame main.go"), true},
		{"docker ps", m("command", "docker ps -a"), true},

		// Pipelines (all stages read-only).
		{"go test | grep", m("command", "go test ./... | grep FAIL"), true},
		{"ls | wc", m("command", "ls -la | wc -l"), true},
		{"git log | head", m("command", "git log --oneline | head -5"), true},

		// Chains (all segments read-only).
		{"go test && go vet", m("command", "go test ./... && go vet ./..."), true},
		{"ls; echo done", m("command", "ls; echo done"), true},

		// Env var prefix.
		{"GOOS=linux go build", m("command", "GOOS=linux go build ./..."), true},
		{"CGO_ENABLED=0 go test", m("command", "CGO_ENABLED=0 go test ./..."), true},

		// Unsafe: mutating commands.
		{"rm -rf", m("command", "rm -rf /tmp/test"), false},
		{"mkdir", m("command", "mkdir -p /tmp/new"), false},
		{"cp", m("command", "cp a.go b.go"), false},
		{"mv", m("command", "mv old.go new.go"), false},
		{"touch", m("command", "touch newfile"), false},
		{"chmod", m("command", "chmod 755 script.sh"), false},

		// Unsafe: git write subcommands.
		{"git push", m("command", "git push origin main"), false},
		{"git commit", m("command", "git commit -m 'test'"), false},
		{"git checkout", m("command", "git checkout -b new-branch"), false},
		{"git reset", m("command", "git reset --hard HEAD~1"), false},
		{"git add", m("command", "git add ."), false},

		// Unsafe: go write subcommands.
		{"go install", m("command", "go install ./..."), false},
		{"go mod tidy", m("command", "go mod tidy"), false},
		{"go get", m("command", "go get -u ./..."), false},

		// Unsafe: output redirection to file.
		{"redirect to file", m("command", "echo hello > output.txt"), false},
		{"append to file", m("command", "echo hello >> log.txt"), false},

		// Safe: redirect to /dev/null.
		{"redirect to devnull", m("command", "go test ./... 2>/dev/null"), true},
		{"stdout to devnull", m("command", "ls > /dev/null"), true},

		// Unsafe: background mode.
		{"background exec", m2("command", "ls", "background", true), false},

		// Unsafe: unknown commands.
		{"unknown binary", m("command", "deploy.sh"), false},
		{"python script", m("command", "python3 script.py"), false},

		// Edge cases.
		{"empty command", m("command", ""), false},
		{"invalid json", nil, false},
		{"no command field", m("workdir", "/tmp"), false},

		// Mixed pipeline: safe | unsafe.
		{"safe pipe unsafe", m("command", "cat file | tee output.txt"), false},

		// Mixed chain: safe && unsafe.
		{"safe chain unsafe", m("command", "go test && rm -rf build/"), false},

		// Path-prefixed binary.
		{"/usr/bin/git status", m("command", "/usr/bin/git status"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input json.RawMessage
			if tt.input != nil {
				input, _ = json.Marshal(tt.input)
			}
			got := IsReadOnlyExecCommand(input)
			if got != tt.wantOK {
				t.Errorf("IsReadOnlyExecCommand(%s) = %v, want %v", string(input), got, tt.wantOK)
			}
		})
	}
}

func TestExtractBinaryAndSubcommand(t *testing.T) {
	tests := []struct {
		cmd     string
		wantBin string
		wantSub string
	}{
		{"go test ./...", "go", "test"},
		{"git status", "git", "status"},
		{"ls -la", "ls", ""},
		{"GOOS=linux go build", "go", "build"},
		{"/usr/bin/git log", "git", "log"},
		{"cargo --verbose test", "cargo", "test"},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			bin, sub := extractBinaryAndSubcommand(tt.cmd)
			if bin != tt.wantBin || sub != tt.wantSub {
				t.Errorf("extractBinaryAndSubcommand(%q) = (%q, %q), want (%q, %q)",
					tt.cmd, bin, sub, tt.wantBin, tt.wantSub)
			}
		})
	}
}

func TestHasFileRedirection(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"echo hello", false},
		{"echo hello > file.txt", true},
		{"echo hello >> file.txt", true},
		{"cmd 2>/dev/null", false},
		{"cmd > /dev/null", false},
		{"cmd 2>&1", false},
		{"cmd > /dev/null 2>&1", false},
		{"cmd 2>/dev/null > output.txt", true},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := hasFileRedirection(tt.cmd)
			if got != tt.want {
				t.Errorf("hasFileRedirection(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestSplitCommandChain(t *testing.T) {
	tests := []struct {
		cmd  string
		want int // number of segments
	}{
		{"ls", 1},
		{"ls && echo done", 2},
		{"a; b; c", 3},
		{"a || b && c", 3},
		{`echo "a && b"`, 1}, // quoted — no split
		{`echo 'a; b'`, 1},   // quoted — no split
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := splitCommandChain(tt.cmd)
			if len(got) != tt.want {
				t.Errorf("splitCommandChain(%q) got %d segments %v, want %d",
					tt.cmd, len(got), got, tt.want)
			}
		})
	}
}

// m is a test helper that creates a map with a single string field.
func m(key, val string) map[string]any {
	return map[string]any{key: val}
}

// m2 creates a map with a string and a bool field.
func m2(k1, v1 string, k2 string, v2 bool) map[string]any {
	return map[string]any{k1: v1, k2: v2}
}
