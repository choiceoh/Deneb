package common

import (
	"errors"
	"math"
	"reflect"
	"sort"
	"testing"
)

func TestTruncateRunesBoundaryMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{
			name:  "empty",
			input: "",
			max:   0,
			want:  "",
		},
		{
			name:  "zero-ascii",
			input: "abc",
			max:   0,
			want:  "...",
		},
		{
			name:  "zero-unicode",
			input: "한글",
			max:   0,
			want:  "...",
		},
		{
			name:  "negative",
			input: "abc",
			max:   -1,
			want:  "...",
		},
		{
			name:  "short",
			input: "ab",
			max:   5,
			want:  "ab",
		},
		{
			name:  "exact",
			input: "abc",
			max:   3,
			want:  "abc",
		},
		{
			name:  "ascii-cut",
			input: "abcdef",
			max:   3,
			want:  "abc...",
		},
		{
			name:  "korean-cut",
			input: "가나다라마바사",
			max:   3,
			want:  "가나다...",
		},
		{
			name:  "emoji-cut",
			input: "😀😃😄😁",
			max:   2,
			want:  "😀😃...",
		},
		{
			name:  "combining",
			input: "éclair",
			max:   2,
			want:  "é...",
		},
		{
			name:  "newline",
			input: "a\nb\nc",
			max:   3,
			want:  "a\nb...",
		},
		{
			name:  "nul",
			input: "a\u0000b",
			max:   2,
			want:  "a\u0000...",
		},
		{
			name:  "astral",
			input: "𝄞music",
			max:   1,
			want:  "𝄞...",
		},
		{
			name:  "mixed",
			input: "A가😀Z",
			max:   3,
			want:  "A가😀...",
		},
		{
			name:  "space",
			input: "   ",
			max:   2,
			want:  "  ...",
		},
		{
			name:  "suffix-input",
			input: "abc...",
			max:   3,
			want:  "abc...",
		},
		{
			name:  "one",
			input: "x",
			max:   1,
			want:  "x",
		},
		{
			name:  "one-over",
			input: "xy",
			max:   1,
			want:  "x...",
		},
		{
			name:  "cjk-exact",
			input: "中文",
			max:   2,
			want:  "中文",
		},
		{
			name:  "cjk-over",
			input: "中文本",
			max:   2,
			want:  "中文...",
		},
		{
			name:  "flag",
			input: "🇰🇷x",
			max:   2,
			want:  "🇰🇷...",
		},
		{
			name:  "tab",
			input: "a\tb",
			max:   2,
			want:  "a\t...",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TruncateRunes(tc.input, tc.max); got != tc.want {
				t.Fatalf("TruncateRunes(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
			}
		})
	}
}

func TestSanitizeSkillNameBoundaryMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trim",
			input: "  Alpha Beta  ",
			want:  "alpha-beta",
		},
		{
			name:  "underscore",
			input: "alpha_beta",
			want:  "alpha-beta",
		},
		{
			name:  "mixed-separators",
			input: "alpha _ beta",
			want:  "alpha-beta",
		},
		{
			name:  "uppercase",
			input: "HTTP Client",
			want:  "http-client",
		},
		{
			name:  "digits",
			input: "Model 42",
			want:  "model-42",
		},
		{
			name:  "punctuation",
			input: "mail: archive!",
			want:  "mail-archive",
		},
		{
			name:  "slashes",
			input: "alpha/beta",
			want:  "alphabeta",
		},
		{
			name:  "dots",
			input: "alpha.beta",
			want:  "alphabeta",
		},
		{
			name:  "leading-hyphen",
			input: "---alpha",
			want:  "alpha",
		},
		{
			name:  "trailing-hyphen",
			input: "alpha---",
			want:  "alpha",
		},
		{
			name:  "collapse",
			input: "alpha-----beta",
			want:  "alpha-beta",
		},
		{
			name:  "single",
			input: "a",
			want:  "",
		},
		{
			name:  "single-digit",
			input: "1",
			want:  "",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "spaces",
			input: "    ",
			want:  "",
		},
		{
			name:  "unicode-only",
			input: "한글",
			want:  "",
		},
		{
			name:  "unicode-between",
			input: "alpha한글beta",
			want:  "alphabeta",
		},
		{
			name:  "emoji",
			input: "alpha😀beta",
			want:  "alphabeta",
		},
		{
			name:  "newline",
			input: "alpha\nbeta",
			want:  "alphabeta",
		},
		{
			name:  "tab",
			input: "alpha\tbeta",
			want:  "alphabeta",
		},
		{
			name:  "already",
			input: "alpha-beta",
			want:  "alpha-beta",
		},
		{
			name:  "numeric",
			input: "123",
			want:  "123",
		},
		{
			name:  "hyphen-digit",
			input: "a-1",
			want:  "a-1",
		},
		{
			name:  "symbols",
			input: "@@alpha##beta$$",
			want:  "alphabeta",
		},
		{
			name:  "double-space",
			input: "alpha  beta",
			want:  "alpha-beta",
		},
		{
			name:  "underscore-run",
			input: "alpha___beta",
			want:  "alpha-beta",
		},
		{
			name:  "case-underscore",
			input: "Foo_BAR",
			want:  "foo-bar",
		},
		{
			name:  "hyphen-space",
			input: "foo- bar",
			want:  "foo-bar",
		},
		{
			name:  "space-hyphen",
			input: "foo -bar",
			want:  "foo-bar",
		},
		{
			name:  "edge-symbol",
			input: "!foo!",
			want:  "foo",
		},
		{
			name:  "urlish",
			input: "foo.com/path",
			want:  "foocompath",
		},
		{
			name:  "quoted",
			input: "\"foo bar\"",
			want:  "foo-bar",
		},
		{
			name:  "apostrophe",
			input: "user's flow",
			want:  "users-flow",
		},
		{
			name:  "paren",
			input: "foo (bar)",
			want:  "foo-bar",
		},
		{
			name:  "plus",
			input: "c++ helper",
			want:  "c-helper",
		},
		{
			name:  "dotnet",
			input: ".NET Tool",
			want:  "net-tool",
		},
		{
			name:  "version",
			input: "skill v2.1",
			want:  "skill-v21",
		},
		{
			name:  "carriage",
			input: "foo\rbar",
			want:  "foobar",
		},
		{
			name:  "nonbreaking",
			input: "foo bar",
			want:  "foobar",
		},
		{
			name:  "em-dash",
			input: "foo—bar",
			want:  "foobar",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SanitizeSkillName(tc.input); got != tc.want {
				t.Fatalf("SanitizeSkillName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestEnvBoolParsesRecognizedSpellingsElseFallback(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		fallback bool
		want     bool
	}{
		{
			name:     "empty-false",
			raw:      "",
			fallback: false,
			want:     false,
		},
		{
			name:     "empty-true",
			raw:      "",
			fallback: true,
			want:     true,
		},
		{
			name:     "one",
			raw:      "1",
			fallback: false,
			want:     true,
		},
		{
			name:     "zero",
			raw:      "0",
			fallback: true,
			want:     false,
		},
		{
			name:     "true",
			raw:      "true",
			fallback: false,
			want:     true,
		},
		{
			name:     "true-upper",
			raw:      "TRUE",
			fallback: false,
			want:     true,
		},
		{
			name:     "true-space",
			raw:      " true ",
			fallback: false,
			want:     true,
		},
		{
			name:     "yes",
			raw:      "yes",
			fallback: false,
			want:     true,
		},
		{
			name:     "on",
			raw:      "on",
			fallback: false,
			want:     true,
		},
		{
			name:     "false",
			raw:      "false",
			fallback: true,
			want:     false,
		},
		{
			name:     "false-upper",
			raw:      "FALSE",
			fallback: true,
			want:     false,
		},
		{
			name:     "false-space",
			raw:      " false ",
			fallback: true,
			want:     false,
		},
		{
			name:     "no",
			raw:      "no",
			fallback: true,
			want:     false,
		},
		{
			name:     "off",
			raw:      "off",
			fallback: true,
			want:     false,
		},
		{
			name:     "invalid-false",
			raw:      "maybe",
			fallback: false,
			want:     false,
		},
		{
			name:     "invalid-true",
			raw:      "maybe",
			fallback: true,
			want:     true,
		},
		{
			name:     "two",
			raw:      "2",
			fallback: false,
			want:     false,
		},
		{
			name:     "negative",
			raw:      "-1",
			fallback: true,
			want:     true,
		},
		{
			name:     "whitespace",
			raw:      " \t ",
			fallback: true,
			want:     true,
		},
		{
			name:     "mixed-case",
			raw:      "YeS",
			fallback: false,
			want:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DENEB_COMMON_TEST_BOOL", tc.raw)
			if got := EnvBool("DENEB_COMMON_TEST_BOOL", tc.fallback); got != tc.want {
				t.Fatalf("EnvBool(%q, %v) = %v, want %v", tc.raw, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestEnvIntParsesValidIntegersElseFallback(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		fallback int
		want     int
	}{
		{
			name:     "empty",
			raw:      "",
			fallback: 7,
			want:     7,
		},
		{
			name:     "spaces",
			raw:      "   ",
			fallback: 7,
			want:     7,
		},
		{
			name:     "zero",
			raw:      "0",
			fallback: 7,
			want:     0,
		},
		{
			name:     "positive",
			raw:      "42",
			fallback: 7,
			want:     42,
		},
		{
			name:     "negative",
			raw:      "-42",
			fallback: 7,
			want:     -42,
		},
		{
			name:     "plus",
			raw:      "+9",
			fallback: 7,
			want:     9,
		},
		{
			name:     "trim",
			raw:      " 12 ",
			fallback: 7,
			want:     12,
		},
		{
			name:     "invalid",
			raw:      "x",
			fallback: 7,
			want:     7,
		},
		{
			name:     "decimal",
			raw:      "1.5",
			fallback: 7,
			want:     7,
		},
		{
			name:     "bool",
			raw:      "true",
			fallback: 7,
			want:     7,
		},
		{
			name:     "leading-zero",
			raw:      "007",
			fallback: 7,
			want:     7,
		},
		{
			name:     "hex",
			raw:      "0x10",
			fallback: 7,
			want:     7,
		},
		{
			name:     "huge",
			raw:      "999999999999999999999999999",
			fallback: 7,
			want:     7,
		},
		{
			name:     "min",
			raw:      "-9223372036854775808",
			fallback: 7,
			want:     -int(^uint(0)>>1) - 1,
		},
		{
			name:     "max",
			raw:      "9223372036854775807",
			fallback: 7,
			want:     int(^uint(0) >> 1),
		},
		{
			name:     "newline",
			raw:      "\n3\n",
			fallback: 7,
			want:     3,
		},
		{
			name:     "sign-only",
			raw:      "-",
			fallback: 7,
			want:     7,
		},
		{
			name:     "double-sign",
			raw:      "--1",
			fallback: 7,
			want:     7,
		},
		{
			name:     "internal-space",
			raw:      "1 2",
			fallback: 7,
			want:     7,
		},
		{
			name:     "fallback-negative",
			raw:      "",
			fallback: -3,
			want:     -3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DENEB_COMMON_TEST_INT", tc.raw)
			if got := EnvInt("DENEB_COMMON_TEST_INT", tc.fallback); got != tc.want {
				t.Fatalf("EnvInt(%q, %d) = %d, want %d", tc.raw, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestSkillDedupTokensNormalizesAndDedupesTerms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		title       string
		description string
		want        []string
	}{
		{
			name:        "ascii",
			title:       "Alpha Beta",
			description: "Gamma",
			want:        []string{"alpha", "beta", "gamma"},
		},
		{
			name:        "punctuation",
			title:       "alpha,beta",
			description: "gamma/delta",
			want:        []string{"alpha", "beta", "gamma", "delta"},
		},
		{
			name:        "short-filter",
			title:       "a bb c",
			description: "dd e",
			want:        []string{"bb", "dd"},
		},
		{
			name:        "digits",
			title:       "model42 v2",
			description: "2026 build",
			want:        []string{"model42", "v2", "2026", "build"},
		},
		{
			name:        "unicode",
			title:       "한글 도구",
			description: "메일 분석",
			want:        []string{"한글", "도구", "메일", "분석"},
		},
		{
			name:        "case-dedupe",
			title:       "Alpha alpha",
			description: "ALPHA",
			want:        []string{"alpha"},
		},
		{
			name:        "hyphen",
			title:       "mail-archive",
			description: "archive mail",
			want:        []string{"mail", "archive"},
		},
		{
			name:        "underscore",
			title:       "foo_bar",
			description: "bar_baz",
			want:        []string{"foo", "bar", "baz"},
		},
		{
			name:        "empty",
			title:       "",
			description: "",
			want:        []string{},
		},
		{
			name:        "symbols",
			title:       "!!!",
			description: "@@@",
			want:        []string{},
		},
		{
			name:        "mixed",
			title:       "A1 b2",
			description: "C3",
			want:        []string{"a1", "b2", "c3"},
		},
		{
			name:        "newlines",
			title:       "alpha\nbeta",
			description: "gamma\tdelta",
			want:        []string{"alpha", "beta", "gamma", "delta"},
		},
		{
			name:        "accent",
			title:       "café résumé",
			description: "naïve",
			want:        []string{"café", "résumé", "naïve"},
		},
		{
			name:        "emoji",
			title:       "😀 alpha",
			description: "beta 😃",
			want:        []string{"alpha", "beta"},
		},
		{
			name:        "cjk-one",
			title:       "中 文",
			description: "中文",
			want:        []string{"中文"},
		},
		{
			name:        "apostrophe",
			title:       "user's",
			description: "agent's",
			want:        []string{"user", "agent"},
		},
		{
			name:        "dot",
			title:       "foo.bar",
			description: "bar.baz",
			want:        []string{"foo", "bar", "baz"},
		},
		{
			name:        "colon",
			title:       "tool:exec",
			description: "path:file",
			want:        []string{"tool", "exec", "path", "file"},
		},
		{
			name:        "numeric-short",
			title:       "1 22",
			description: "333 4",
			want:        []string{"22", "333"},
		},
		{
			name:        "spaces",
			title:       "  alpha  ",
			description: " beta ",
			want:        []string{"alpha", "beta"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotSet := SkillDedupTokens(tc.title, tc.description)
			got := make([]string, 0, len(gotSet))
			for token := range gotSet {
				got = append(got, token)
			}
			sort.Strings(got)
			if len(got) == 0 {
				got = nil
			}
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("SkillDedupTokens(%q, %q) = %#v, want %#v", tc.title, tc.description, got, want)
			}
		})
	}
}

func TestJaccardSimilarityReturnsIntersectionOverUnion(t *testing.T) {
	t.Parallel()
	set := func(values any) map[string]struct{} {
		out := map[string]struct{}{}
		switch v := values.(type) {
		case string:
			out[v] = struct{}{}
		case []string:
			for _, item := range v {
				out[item] = struct{}{}
			}
		}
		return out
	}
	cases := []struct {
		name string
		a    any
		b    any
		want float64
	}{
		{
			name: "both-empty",
			a:    []string{},
			b:    []string{},
			want: 0,
		},
		{
			name: "left-empty",
			a:    []string{},
			b:    []string{"a"},
			want: 0,
		},
		{
			name: "right-empty",
			a:    []string{"a"},
			b:    []string{},
			want: 0,
		},
		{
			name: "equal-one",
			a:    []string{"a"},
			b:    []string{"a"},
			want: 1,
		},
		{
			name: "equal-two",
			a:    []string{"a", "b"},
			b:    []string{"a", "b"},
			want: 1,
		},
		{
			name: "disjoint",
			a:    []string{"a"},
			b:    []string{"b"},
			want: 0,
		},
		{
			name: "half",
			a:    []string{"a", "b"},
			b:    []string{"b", "c"},
			want: 0.3333333333333333,
		},
		{
			name: "one-of-two",
			a:    []string{"a", "b"},
			b:    []string{"a"},
			want: 0.5,
		},
		{
			name: "two-of-three",
			a:    []string{"a", "b", "c"},
			b:    []string{"b", "c"},
			want: 0.6666666666666666,
		},
		{
			name: "subset",
			a:    []string{"a", "b", "c", "d"},
			b:    []string{"b", "c"},
			want: 0.5,
		},
		{
			name: "one-of-four",
			a:    []string{"a", "b"},
			b:    []string{"b", "c", "d"},
			want: 0.25,
		},
		{
			name: "unicode",
			a:    []string{"한글", "도구"},
			b:    []string{"도구", "메일"},
			want: 0.3333333333333333,
		},
		{
			name: "numbers",
			a:    []string{"1", "2"},
			b:    []string{"2", "3"},
			want: 0.3333333333333333,
		},
		{
			name: "case-distinct",
			a:    []string{"A"},
			b:    []string{"a"},
			want: 0,
		},
		{
			name: "duplicate-source",
			a:    []string{"a", "a", "b"},
			b:    []string{"a"},
			want: 0.5,
		},
		{
			name: "three-disjoint",
			a:    []string{"a", "b", "c"},
			b:    []string{"d", "e"},
			want: 0,
		},
		{
			name: "four-overlap",
			a:    []string{"a", "b", "c", "d"},
			b:    []string{"b", "c", "d", "e"},
			want: 0.6,
		},
		{
			name: "single-v-many",
			a:    []string{"a"},
			b:    []string{"a", "b", "c"},
			want: 0.3333333333333333,
		},
		{
			name: "same-five",
			a:    []string{"a", "b", "c", "d", "e"},
			b:    []string{"a", "b", "c", "d", "e"},
			want: 1,
		},
		{
			name: "near",
			a:    "alpha",
			b:    "alpha",
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := JaccardSimilarity(set(tc.a), set(tc.b))
			if math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("JaccardSimilarity() = %.12f, want %.12f", got, tc.want)
			}
		})
	}
}

func TestErrorStringNilAndWrappedErrors(t *testing.T) {
	t.Parallel()
	if got := ErrorString(nil); got != "" {
		t.Fatalf("ErrorString(nil) = %q", got)
	}
	base := errors.New("disk unavailable")
	if got := ErrorString(base); got != "disk unavailable" {
		t.Fatalf("ErrorString(base) = %q", got)
	}
	wrapped := errors.Join(errors.New("outer"), base)
	if got := ErrorString(wrapped); got != "outer\ndisk unavailable" {
		t.Fatalf("ErrorString(joined) = %q", got)
	}
}
