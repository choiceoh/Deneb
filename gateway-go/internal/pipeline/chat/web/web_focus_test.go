package web

import (
	"strings"
	"testing"
)

// A long reference page fetched to answer one question should cost the tokens of
// that question's section, not of the whole page. Truncation cannot do this: it
// keeps the front of the document, which is where the answer usually is not.
const focusTestPage = `# Rust

Rust is a systems programming language.

## History

Work began in 2006 at Mozilla Research. The 1.0 release shipped in 2015 after a
long period of syntax churn, and the language has kept a six-week train since.

## Ownership and references

Ownership is how Rust achieves memory safety without a garbage collector. Each
value has a single owner; references borrow it under rules the compiler checks,
so a use-after-free is a compile error rather than a crash.

### Borrow checker

The borrow checker enforces that a value has either one mutable reference or any
number of shared references, never both at once.

## Tooling

Cargo builds and tests. Clippy lints.
`

func TestFocusKeepsTheAskedAboutSection(t *testing.T) {
	got, ok := focusExcerpt(focusTestPage, "memory safety ownership", 400)
	if !ok {
		t.Fatal("focus produced nothing")
	}
	if !strings.Contains(got.Text, "Ownership is how Rust achieves memory safety") {
		t.Fatalf("the matching section was dropped:\n%s", got.Text)
	}
	// The unrelated sections are what the caller is paying to NOT receive.
	if strings.Contains(got.Text, "Work began in 2006") {
		t.Fatalf("unrelated History section survived:\n%s", got.Text)
	}
	if strings.Contains(got.Text, "Cargo builds and tests") {
		t.Fatalf("unrelated Tooling section survived:\n%s", got.Text)
	}
	if got.KeptChars >= got.TotalChars {
		t.Fatalf("excerpt did not shrink the page: %d/%d", got.KeptChars, got.TotalChars)
	}
}

func TestFocusCarriesParentHeadingsForContext(t *testing.T) {
	// "borrow checker" only matches the nested section; without its ancestor the
	// model cannot tell what the paragraph is scoped to.
	got, ok := focusExcerpt(focusTestPage, "borrow checker mutable reference", 300)
	if !ok {
		t.Fatal("focus produced nothing")
	}
	if !strings.Contains(got.Text, "Borrow checker") {
		t.Fatalf("target section missing:\n%s", got.Text)
	}
	if !strings.Contains(got.Text, "Ownership and references") {
		t.Fatalf("ancestor heading was not carried:\n%s", got.Text)
	}
}

func TestFocusMarksWhatItRemoved(t *testing.T) {
	got, ok := focusExcerpt(focusTestPage, "ownership", 300)
	if !ok {
		t.Fatal("focus produced nothing")
	}
	if !strings.Contains(got.Text, "[...]") {
		t.Fatalf("elision was silent — the model cannot tell the page was narrowed:\n%s", got.Text)
	}
}

func TestFocusDeclinesRatherThanReturningAnEmptyPage(t *testing.T) {
	// A focus nobody's page matches must fall through to the caller's normal
	// handling, never to an empty result.
	if _, ok := focusExcerpt(focusTestPage, "설거지 세제 추천", 300); ok {
		t.Fatal("focus claimed a match it did not have")
	}
	// No focus, and a page that already fits, are both no-ops.
	if _, ok := focusExcerpt(focusTestPage, "", 300); ok {
		t.Fatal("empty focus should not narrow")
	}
	if _, ok := focusExcerpt(focusTestPage, "ownership", len(focusTestPage)+1); ok {
		t.Fatal("a page within budget should not be narrowed")
	}
}

func TestFocusMatchesKoreanCompounds(t *testing.T) {
	// Korean writes compounds without spaces, so whole-word matching alone would
	// miss "메모리" inside "메모리안전성".
	page := "# 문서\n\n## 배포\n\n배포는 타이머가 한다.\n\n## 메모리안전성 보장\n\n소유권으로 보장한다.\n"
	got, ok := focusExcerpt(page, "메모리", 60)
	if !ok {
		t.Fatal("Korean compound was not matched")
	}
	if !strings.Contains(got.Text, "소유권으로 보장한다") {
		t.Fatalf("matched the wrong section:\n%s", got.Text)
	}
}

func TestSectionSplitIgnoresHashesInsideCodeFences(t *testing.T) {
	page := "# Title\n\n## Setup\n\n```sh\n# not a heading\napt install foo\n```\n\n## Usage\n\nrun foo\n"
	sections := splitMarkdownSections(page)
	for _, s := range sections {
		if s.title == "not a heading" {
			t.Fatal("a shell comment inside a fence was parsed as a heading")
		}
	}
	if len(sections) != 3 {
		t.Fatalf("want Title/Setup/Usage, got %d sections", len(sections))
	}
}

func TestFocusAndTruncationReportsTheNarrowing(t *testing.T) {
	envelope := "<metadata>\nURL: https://example.com\n</metadata>\n<content>\n" + focusTestPage + "\n</content>"
	out := applyFocusAndTruncation(envelope, "memory safety ownership", 600)

	if !strings.Contains(out, "Focus: memory safety ownership") {
		t.Fatalf("narrowing was not reported in metadata:\n%s", out)
	}
	if !strings.Contains(out, "kept ") || !strings.Contains(out, "sections") {
		t.Fatalf("metadata does not say how much was kept:\n%s", out)
	}
	if strings.Contains(out, "Work began in 2006") {
		t.Fatalf("unrelated section survived the envelope path:\n%s", out)
	}
}

func TestFocusAndTruncationFallsBackToPlainTruncation(t *testing.T) {
	envelope := "<metadata>\nURL: https://example.com\n</metadata>\n<content>\n" + focusTestPage + "\n</content>"
	out := applyFocusAndTruncation(envelope, "설거지 세제", 600)

	if strings.Contains(out, "Focus:") {
		t.Fatalf("claimed a focus it did not apply:\n%s", out)
	}
	if !strings.Contains(out, "Rust is a systems programming language") {
		t.Fatalf("fallback did not return the page:\n%s", out)
	}
}
