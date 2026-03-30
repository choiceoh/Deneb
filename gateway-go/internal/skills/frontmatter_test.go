package skills

import (
	"strings"
	"testing"
)

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

func TestParseFrontmatter_Empty(t *testing.T) {
	fm := ParseFrontmatter("")
	if len(fm) != 0 {
		t.Errorf("expected empty frontmatter, got %v", fm)
	}
}

func TestParseFrontmatter_NoDelimiter(t *testing.T) {
	fm := ParseFrontmatter("just some text\nno frontmatter")
	if len(fm) != 0 {
		t.Errorf("expected empty frontmatter, got %v", fm)
	}
}

func TestParseFrontmatter_NonEmptyBeforeDelimiter(t *testing.T) {
	content := `some content
---
title: Should Not Parse
---`
	fm := ParseFrontmatter(content)
	if len(fm) != 0 {
		t.Errorf("expected empty when non-empty line before delimiter, got %v", fm)
	}
}

func TestParseFrontmatter_NoClosingDelimiter(t *testing.T) {
	content := `---
title: Open
description: No close`

	fm := ParseFrontmatter(content)
	// Should still parse key-value pairs (just doesn't find closing ---)
	if fm["title"] != "Open" {
		t.Errorf("title = %q, want 'Open'", fm["title"])
	}
}

func TestParseFrontmatter_EmptyValue(t *testing.T) {
	content := `---
empty:
filled: yes
---`

	fm := ParseFrontmatter(content)
	if fm["empty"] != "" {
		t.Errorf("empty = %q, want empty string", fm["empty"])
	}
	if fm["filled"] != "yes" {
		t.Errorf("filled = %q, want 'yes'", fm["filled"])
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

func TestResolveSkillInvocationPolicy_Defaults(t *testing.T) {
	fm := ParsedFrontmatter{}
	policy := ResolveSkillInvocationPolicy(fm)

	if !policy.UserInvocable {
		t.Error("expected UserInvocable=true by default")
	}
	if policy.DisableModelInvocation {
		t.Error("expected DisableModelInvocation=false by default")
	}
}

func TestResolveSkillInvocationPolicy_Overrides(t *testing.T) {
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

func TestResolveDenebMetadata_NoMetadata(t *testing.T) {
	fm := ParsedFrontmatter{"title": "Test"}
	meta := ResolveDenebMetadata(fm)
	if meta != nil {
		t.Errorf("expected nil for no metadata, got %+v", meta)
	}
}

func TestResolveDenebMetadata_EmptyMetadata(t *testing.T) {
	fm := ParsedFrontmatter{"metadata": ""}
	meta := ResolveDenebMetadata(fm)
	if meta != nil {
		t.Errorf("expected nil for empty metadata, got %+v", meta)
	}
}

func TestResolveDenebMetadata_InvalidJSON(t *testing.T) {
	fm := ParsedFrontmatter{"metadata": "{invalid"}
	meta := ResolveDenebMetadata(fm)
	if meta != nil {
		t.Errorf("expected nil for invalid JSON, got %+v", meta)
	}
}

func TestResolveDenebMetadata_NoDenebKey(t *testing.T) {
	fm := ParsedFrontmatter{"metadata": `{"other": {}}`}
	meta := ResolveDenebMetadata(fm)
	if meta != nil {
		t.Errorf("expected nil when no 'deneb' key, got %+v", meta)
	}
}

func TestResolveDenebMetadata_ValidMetadata(t *testing.T) {
	fm := ParsedFrontmatter{
		"metadata": `{"deneb": {"always": true, "skillKey": "weather", "emoji": "☀️", "os": ["linux", "macos"]}}`,
	}
	meta := ResolveDenebMetadata(fm)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if !meta.Always {
		t.Error("expected Always=true")
	}
	if meta.SkillKey != "weather" {
		t.Errorf("expected skillKey='weather', got %q", meta.SkillKey)
	}
	if meta.Emoji != "☀️" {
		t.Errorf("expected emoji='☀️', got %q", meta.Emoji)
	}
	if len(meta.OS) != 2 {
		t.Fatalf("expected 2 OS entries, got %d", len(meta.OS))
	}
}

func TestResolveDenebMetadata_WithRequires(t *testing.T) {
	fm := ParsedFrontmatter{
		"metadata": `{"deneb": {"requires": {"bins": ["rg", "fd"], "env": ["API_KEY"]}}}`,
	}
	meta := ResolveDenebMetadata(fm)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta.Requires == nil {
		t.Fatal("expected non-nil Requires")
	}
	if len(meta.Requires.Bins) != 2 {
		t.Errorf("expected 2 bins, got %d", len(meta.Requires.Bins))
	}
	if len(meta.Requires.Env) != 1 {
		t.Errorf("expected 1 env, got %d", len(meta.Requires.Env))
	}
}

func TestResolveDenebMetadata_WithTags(t *testing.T) {
	fm := ParsedFrontmatter{
		"metadata": `{"deneb": {"tags": ["cli", "productivity", "google"]}}`,
	}
	meta := ResolveDenebMetadata(fm)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if len(meta.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(meta.Tags))
	}
	if meta.Tags[0] != "cli" || meta.Tags[1] != "productivity" || meta.Tags[2] != "google" {
		t.Errorf("unexpected tags: %v", meta.Tags)
	}
}

func TestExtractFrontmatterBlock_Valid(t *testing.T) {
	content := "---\nname: test\ndescription: A test\n---\n# Body\n\nSome content here."
	header, offset := ExtractFrontmatterBlock(content)
	if header == "" {
		t.Fatal("expected non-empty header")
	}
	if !strings.Contains(header, "name: test") {
		t.Errorf("header should contain frontmatter, got %q", header)
	}
	if offset <= 0 {
		t.Errorf("expected positive offset, got %d", offset)
	}
	body := content[offset:]
	if !strings.Contains(body, "# Body") {
		t.Errorf("body should contain content after frontmatter, got %q", body)
	}
}

func TestExtractFrontmatterBlock_NoFrontmatter(t *testing.T) {
	content := "Just some text\nNo frontmatter here"
	header, offset := ExtractFrontmatterBlock(content)
	if header != "" {
		t.Errorf("expected empty header for no frontmatter, got %q", header)
	}
	if offset != 0 {
		t.Errorf("expected 0 offset, got %d", offset)
	}
}

func TestExtractFrontmatterBlock_NoClosing(t *testing.T) {
	content := "---\nname: open\ndescription: No close"
	header, _ := ExtractFrontmatterBlock(content)
	if header == "" {
		t.Fatal("expected non-empty header even without closing delimiter")
	}
	if !strings.Contains(header, "name: open") {
		t.Errorf("header should contain frontmatter, got %q", header)
	}
}

func TestExtractFrontmatterBlock_Empty(t *testing.T) {
	header, offset := ExtractFrontmatterBlock("")
	if header != "" || offset != 0 {
		t.Errorf("expected empty result for empty content")
	}
}
