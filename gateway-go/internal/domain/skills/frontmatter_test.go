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





func TestResolveDenebMetadata_ValidMetadata(t *testing.T) {
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
		t.Errorf("got %d, want positive offset", offset)
	}
	body := content[offset:]
	if !strings.Contains(body, "# Body") {
		t.Errorf("body should contain content after frontmatter, got %q", body)
	}
}



