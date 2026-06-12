package genesis

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func TestStripEchoedFrontmatter(t *testing.T) {
	fm := "---\nname: demo\nversion: \"1.1.0\"\n---\n"
	body := "# Demo\n\n## Procedure\n- step one"
	hr := "---\njust a section divider\n---\n" + body

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain body untouched", body, body},
		{"single echoed block stripped", fm + "\n" + body, body},
		{"stacked echoed blocks stripped", fm + "\n" + fm + "\n" + body, body},
		{"divider without name key kept", hr, hr},
		{"frontmatter-only input kept", fm, fm},
		{"empty input", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripEchoedFrontmatter(tc.in); got != tc.want {
				t.Errorf("stripEchoedFrontmatter() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseAndApply_StripsEchoedFrontmatterAndBumpsVersion reproduces the
// production triple-frontmatter corruption: the LLM echoes the frontmatter
// into changes.body and returns the unchanged version. The committed file
// must contain exactly one frontmatter block with a bumped patch version.
func TestParseAndApply_StripsEchoedFrontmatterAndBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := "---\nname: demo\nversion: \"1.1.0\"\n---\n\n# Demo\n\nold body\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &Evolver{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		selfTest: false,
	}
	entry := &skills.SkillEntry{Skill: skills.Skill{
		Name:     "demo",
		Version:  "1.1.0",
		FilePath: path,
	}}

	// LLM response: body echoes the frontmatter, new_version is unchanged.
	resp := `{"skip":false,"changes":{"description":"d","new_version":"1.1.0","body":"---\nname: demo\nversion: \"1.1.0\"\n---\n\n# Demo\n\nnew body"}}`

	result, err := e.parseAndApply(context.Background(), resp, entry, original, &UsageStats{SkillName: "demo"})
	if err != nil {
		t.Fatalf("parseAndApply: %v", err)
	}
	if !result.Evolved {
		t.Fatalf("expected Evolved=true, got %+v", result)
	}
	if result.NewVersion != "1.1.1" {
		t.Errorf("expected forced patch bump to 1.1.1, got %q", result.NewVersion)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)
	if n := strings.Count(content, "name: demo"); n != 1 {
		t.Errorf("expected exactly 1 frontmatter block, found %d name keys:\n%s", n, content)
	}
	if !strings.Contains(content, `version: "1.1.1"`) {
		t.Errorf("expected bumped version in header:\n%s", content)
	}
	if !strings.Contains(content, "new body") {
		t.Errorf("expected rewritten body to be committed:\n%s", content)
	}
}
