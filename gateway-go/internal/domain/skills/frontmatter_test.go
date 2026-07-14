package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundledSkillMetadataParsesForAllSkillsAndPreservesKbInterviewTriggers sweeps the repo's bundled skills/ tree:
// every SKILL.md that declares a metadata block must parse to non-nil deneb
// metadata. ResolveDenebMetadata fails SILENTLY (nil) on malformed JSON, which
// drops triggers/tags/related_skills without any error — kb-interview shipped
// with a broken "tags": key and its auto-surfacing triggers were dead until
// this sweep existed. Skipped when the repo skills/ dir isn't present (e.g.
// the module built outside the repo).
func TestBundledSkillMetadataParsesForAllSkillsAndPreservesKbInterviewTriggers(t *testing.T) {
	skillsDir := filepath.Join("..", "..", "..", "..", "skills")
	if info, err := os.Stat(skillsDir); err != nil || !info.IsDir() {
		t.Skipf("bundled skills dir not found at %s", skillsDir)
	}
	checked := 0
	err := filepath.WalkDir(skillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return err
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Errorf("read %s: %v", path, rerr)
			return nil
		}
		fm := ParseFrontmatter(string(content))
		raw, ok := fm["metadata"]
		if !ok || raw == "" {
			return nil // no metadata block — nothing to validate
		}
		checked++
		if meta := ResolveDenebMetadata(fm); meta == nil {
			t.Errorf("%s: metadata block does not resolve (malformed JSON?) — triggers/tags are silently dropped", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if checked == 0 {
		t.Fatal("no SKILL.md with a metadata block found — sweep is not covering anything")
	}

	// Pin the kb-interview fix specifically: its triggers must survive to the
	// auto-surfacing matcher (skill_hints).
	kb, err := os.ReadFile(filepath.Join(skillsDir, "knowledge", "kb-interview", "SKILL.md"))
	if err != nil {
		t.Skipf("kb-interview skill not found: %v", err)
	}
	meta := ResolveDenebMetadata(ParseFrontmatter(string(kb)))
	if meta == nil {
		t.Fatal("kb-interview metadata must resolve")
	}
	if len(meta.Triggers) == 0 {
		t.Error("kb-interview triggers = empty, want the declared utterance triggers")
	}
	if len(meta.Tags) == 0 {
		t.Error("kb-interview tags = empty, want the declared tags")
	}
}

func TestParseFrontmatter_Basic(t *testing.T) {
	content := `---
title: Weather
description: Fetch weather data
version: 1.0
---
# Weather Skill
Some content here.`

	fm := ParseFrontmatter(content)
	if fm["title"] != "Weather" {
		t.Errorf("title = %q, want 'Weather'", fm["title"])
	}
	if fm["description"] != "Fetch weather data" {
		t.Errorf("description = %q", fm["description"])
	}
	if fm["version"] != "1.0" {
		t.Errorf("version = %q", fm["version"])
	}
}

func TestParseFrontmatter_QuotedValues(t *testing.T) {
	content := `---
name: "hello world"
alt: 'single quoted'
---`

	fm := ParseFrontmatter(content)
	if fm["name"] != "hello world" {
		t.Errorf("name = %q, want 'hello world'", fm["name"])
	}
	if fm["alt"] != "single quoted" {
		t.Errorf("alt = %q, want 'single quoted'", fm["alt"])
	}
}

func TestParseFrontmatter_MultilineMetadataBlock(t *testing.T) {
	content := `---
name: github
metadata:
  {
    "deneb":
      {
        "emoji": "🐙",
        "tags": ["git", "PR"],
      },
  }
user-invocable: true
---`

	fm := ParseFrontmatter(content)
	if fm["name"] != "github" {
		t.Fatalf("name = %q, want github", fm["name"])
	}
	if !strings.Contains(fm["metadata"], `"emoji": "🐙"`) {
		t.Fatalf("metadata block missing emoji: %q", fm["metadata"])
	}
	if fm["user-invocable"] != "true" {
		t.Fatalf("user-invocable = %q, want true", fm["user-invocable"])
	}
}

func TestParseFrontmatterBool(t *testing.T) {
	fm := ParsedFrontmatter{
		"enabled":  "true",
		"disabled": "false",
		"yes":      "yes",
		"no":       "no",
		"one":      "1",
		"zero":     "0",
		"invalid":  "maybe",
	}

	tests := []struct {
		key      string
		fallback bool
		want     bool
	}{
		{"enabled", false, true},
		{"disabled", true, false},
		{"yes", false, true},
		{"no", true, false},
		{"one", false, true},
		{"zero", true, false},
		{"invalid", true, true},   // fallback
		{"missing", false, false}, // fallback
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got := parseFrontmatterBool(fm, tc.key, tc.fallback)
			if got != tc.want {
				t.Errorf("parseFrontmatterBool(%q, %v) = %v, want %v", tc.key, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestResolveSkillInvocationPolicyReturnsUserInvocableTrueByDefault(t *testing.T) {
	fm := ParsedFrontmatter{}
	policy := ResolveSkillInvocationPolicy(fm)

	if !policy.UserInvocable {
		t.Error("expected UserInvocable=true by default")
	}
	if policy.DisableModelInvocation {
		t.Error("expected DisableModelInvocation=false by default")
	}
}

func TestResolveSkillInvocationPolicyAppliesFrontmatterOverridesWhenSet(t *testing.T) {
	fm := ParsedFrontmatter{
		"user-invocable":           "false",
		"disable-model-invocation": "true",
	}
	policy := ResolveSkillInvocationPolicy(fm)

	if policy.UserInvocable {
		t.Error("expected UserInvocable=false")
	}
	if !policy.DisableModelInvocation {
		t.Error("expected DisableModelInvocation=true")
	}
}

func TestNormalizeSafeBrewFormula_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ripgrep", "ripgrep"},
		{"homebrew/cask/firefox", "homebrew/cask/firefox"},
		{"jq@1.7", "jq@1.7"},
		{"", ""},
		{"-malicious", ""},
		{"with\\backslash", ""},
		{"path/../escape", ""},
		{"invalid chars!", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeSafeBrewFormula(tc.input)
			if got != tc.want {
				t.Errorf("normalizeSafeBrewFormula(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeSafeGoModule(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/user/repo@latest", "github.com/user/repo@latest"},
		{"golang.org/x/tools", "golang.org/x/tools"},
		{"", ""},
		{"-flag", ""},
		{"with\\backslash", ""},
		{"http://evil.com/module", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeSafeGoModule(tc.input)
			if got != tc.want {
				t.Errorf("normalizeSafeGoModule(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeSafeAptPackage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gh", "gh"},
		{"python3-venv", "python3-venv"},
		{"libssl3:arm64", "libssl3:arm64"},
		{"", ""},
		{"-bad", ""},
		{"with/slash", ""},
		{"with space", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeSafeAptPackage(tc.input)
			if got != tc.want {
				t.Errorf("normalizeSafeAptPackage(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeSafeUvPackage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"requests", "requests"},
		{"numpy>=1.0", "numpy>=1.0"},
		{"package[extra]", "package[extra]"},
		{"", ""},
		{"-p", ""},
		{"with\\slash", ""},
		{"http://pypi.org/package", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeSafeUvPackage(tc.input)
			if got != tc.want {
				t.Errorf("normalizeSafeUvPackage(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeSafeDownloadURL_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/file.tar.gz", "https://example.com/file.tar.gz"},
		{"http://mirror.org/tool", "http://mirror.org/tool"},
		{"", ""},
		{"ftp://evil.com/file", ""},
		{"file:///etc/passwd", ""},
		{"https://example.com/has space", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeSafeDownloadURL(tc.input)
			if got != tc.want {
				t.Errorf("normalizeSafeDownloadURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveDenebMetadataParsesValidMetadataFields(t *testing.T) {
	fm := ParsedFrontmatter{
		"metadata": `{"deneb": {"always": true, "skillKey": "weather", "emoji": "☀️", "os": ["linux"]}}`,
	}
	meta := ResolveDenebMetadata(fm)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if !meta.Always {
		t.Error("expected Always=true")
	}
	if meta.SkillKey != "weather" {
		t.Errorf("got %q, want skillKey='weather'", meta.SkillKey)
	}
	if meta.Emoji != "☀️" {
		t.Errorf("got %q, want emoji='☀️'", meta.Emoji)
	}
}

func TestResolveDenebMetadataParsesRelaxedJSONWithTrailingCommas(t *testing.T) {
	content := `---
name: github
metadata:
  {
    "deneb":
      {
        "emoji": "🐙",
        "requires": { "bins": ["gh"] },
        "tags": ["git", "PR", "issues"],
        "triggers": ["계약서", "독소조항"],
        "related_skills": ["skill-creator"],
        "install":
          [
            {
              "id": "brew",
              "kind": "brew",
              "formula": "gh",
              "bins": ["gh"],
            },
            {
              "id": "apt",
              "kind": "apt",
              "package": "gh",
              "bins": ["gh"],
            },
          ],
      },
  }
---`

	meta := ResolveDenebMetadata(ParseFrontmatter(content))
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta.Emoji != "🐙" {
		t.Fatalf("emoji = %q, want 🐙", meta.Emoji)
	}
	if len(meta.Tags) != 3 || meta.Tags[0] != "git" {
		t.Fatalf("tags = %#v, want parsed tags", meta.Tags)
	}
	if len(meta.RelatedSkills) != 1 || meta.RelatedSkills[0] != "skill-creator" {
		t.Fatalf("related skills = %#v, want skill-creator", meta.RelatedSkills)
	}
	if len(meta.Triggers) != 2 || meta.Triggers[0] != "계약서" {
		t.Fatalf("triggers = %#v, want parsed triggers", meta.Triggers)
	}
	if meta.Requires == nil || len(meta.Requires.Bins) != 1 || meta.Requires.Bins[0] != "gh" {
		t.Fatalf("requires = %#v, want gh bin", meta.Requires)
	}
	if len(meta.Install) != 2 {
		t.Fatalf("install count = %d, want 2", len(meta.Install))
	}
	if meta.Install[1].Kind != "apt" || meta.Install[1].Package != "gh" {
		t.Fatalf("apt install spec = %#v, want apt package gh", meta.Install[1])
	}
}

func TestExtractFrontmatterBlockReturnsHeaderAndBodyOffset(t *testing.T) {
	content := "---\nname: test\ndescription: A test\n---\n# Body\n\nSome content here."
	header, offset := ExtractFrontmatterBlock(content)
	if header == "" {
		t.Fatal("expected non-empty header")
	}
	if !strings.Contains(header, "name: test") {
		t.Errorf("header should contain frontmatter, got %q", header)
	}
	if offset <= 0 {
		t.Errorf("got %d, want positive offset", offset)
	}
	body := content[offset:]
	if !strings.Contains(body, "# Body") {
		t.Errorf("body should contain content after frontmatter, got %q", body)
	}
}
