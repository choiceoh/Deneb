package briefcase

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPureGrepCharacterizesScopeOrderingCaseAndLimit(t *testing.T) {
	grep, workspace := newPureGrepTestTool(t)
	writePureGrepFixture(t, workspace, "a.txt", "Needle alpha\nother\n")
	writePureGrepFixture(t, workspace, "nested/b.txt", "needle beta\nNEEDLE gamma\n")
	writePureGrepFixture(t, workspace, "nested/c.txt", "needle delta\n")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "relative directory scope and case-sensitive matches",
			input: `{"pattern":"needle","path":"nested"}`,
			want:  "nested/b.txt:1:needle beta\nnested/c.txt:1:needle delta",
		},
		{
			name:  "case-insensitive traversal stops at the requested limit",
			input: `{"pattern":"needle","ignoreCase":true,"maxResults":2}`,
			want:  "a.txt:1:Needle alpha\nnested/b.txt:1:needle beta",
		},
		{
			name:  "single file scope keeps workspace-relative output",
			input: `{"pattern":"NEEDLE","path":"nested/b.txt"}`,
			want:  "nested/b.txt:2:NEEDLE gamma",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := grep(context.Background(), json.RawMessage(tt.input))
			if err != nil {
				t.Fatalf("pureGrep: %v", err)
			}
			if got != tt.want {
				t.Fatalf("pureGrep output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPureGrepCharacterizesRejectedInputs(t *testing.T) {
	grep, _ := newPureGrepTestTool(t)
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "object required", input: `[]`, wantErr: "tool input must be a JSON object"},
		{name: "pattern required", input: `{"pattern":""}`, wantErr: "pattern is required"},
		{name: "invalid regular expression", input: `{"pattern":"["}`, wantErr: "invalid grep pattern"},
		{name: "unknown field", input: `{"pattern":"x","before":2}`, wantErr: `unknown tool input field "before"`},
		{name: "workspace escape", input: `{"pattern":"x","path":"../outside"}`, wantErr: "path escapes workspace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := grep(context.Background(), json.RawMessage(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("pureGrep error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestPureGrepCharacterizesCancellationAndSymlinkRejection(t *testing.T) {
	grep, workspace := newPureGrepTestTool(t)
	writePureGrepFixture(t, workspace, "target.txt", "needle\n")

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := grep(canceled, json.RawMessage(`{"pattern":"needle"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("pureGrep canceled error = %v, want context.Canceled", err)
	}

	link := filepath.Join(workspace, "link.txt")
	if err := os.Symlink("target.txt", link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := grep(context.Background(), json.RawMessage(`{"pattern":"needle","path":"link.txt"}`))
	if err == nil || !strings.Contains(err.Error(), `grep encountered symlink "link.txt"`) {
		t.Fatalf("pureGrep symlink error = %v", err)
	}
}

func newPureGrepTestTool(t *testing.T) (func(context.Context, json.RawMessage) (string, error), string) {
	t.Helper()
	root, err := NewRunRoot("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	policy, err := NewPolicy(root, PolicyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := root.Paths()
	if err != nil {
		t.Fatal(err)
	}
	return pureGrep(paths.Workspace, policy), paths.Workspace
}

func writePureGrepFixture(t *testing.T, workspace, relative, content string) {
	t.Helper()
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
