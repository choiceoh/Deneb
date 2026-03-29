package reply

import (
	"strings"
	"testing"
)

// --- ExtractMentions ---

func TestExtractMentions_None(t *testing.T) {
	got := ExtractMentions("hello world, no mentions here")
	if len(got) != 0 {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestExtractMentions_Single(t *testing.T) {
	got := ExtractMentions("hey @alice how are you")
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("expected [alice], got %v", got)
	}
}

func TestExtractMentions_Multiple(t *testing.T) {
	got := ExtractMentions("@alice and @bob meet @carol")
	if len(got) != 3 {
		t.Fatalf("expected 3 mentions, got %d: %v", len(got), got)
	}
}

func TestExtractMentions_Deduplication(t *testing.T) {
	got := ExtractMentions("@alice @alice @alice")
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("expected [alice] (deduped), got %v", got)
	}
}

func TestExtractMentions_PreservesOrder(t *testing.T) {
	got := ExtractMentions("@charlie @alpha @beta")
	if len(got) != 3 || got[0] != "charlie" || got[1] != "alpha" || got[2] != "beta" {
		t.Errorf("unexpected order: %v", got)
	}
}

func TestExtractMentions_Empty(t *testing.T) {
	got := ExtractMentions("")
	if len(got) != 0 {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

// --- ContainsMention ---

func TestContainsMention_Match(t *testing.T) {
	if !ContainsMention("hello @alice", "alice") {
		t.Error("expected true for @alice in text")
	}
}

func TestContainsMention_CaseInsensitive(t *testing.T) {
	if !ContainsMention("hello @Alice", "alice") {
		t.Error("expected case-insensitive match")
	}
}

func TestContainsMention_NoMatch(t *testing.T) {
	if ContainsMention("hello @bob", "alice") {
		t.Error("expected false when mention not present")
	}
}

func TestContainsMention_EmptyUsername(t *testing.T) {
	if ContainsMention("hello @foo", "") {
		t.Error("empty username must return false")
	}
}

func TestContainsMention_WordBoundary(t *testing.T) {
	// "@aliceextra" should NOT match "alice" (word boundary).
	if ContainsMention("say hello @aliceextra", "alice") {
		t.Error("expected no match on partial word")
	}
}

// --- StripInboundMeta ---

func TestStripInboundMeta_Empty(t *testing.T) {
	if got := StripInboundMeta(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestStripInboundMeta_NoMeta(t *testing.T) {
	input := "just a regular message"
	got := StripInboundMeta(input)
	if got != input {
		t.Errorf("unexpected change: %q → %q", input, got)
	}
}

func TestStripInboundMeta_ForwardedHeader(t *testing.T) {
	input := "Forwarded from Someone:\nhello"
	got := StripInboundMeta(input)
	if strings.Contains(got, "Forwarded from") {
		t.Errorf("forwarded header not stripped: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("body should be preserved: %q", got)
	}
}

func TestStripInboundMeta_Trimmed(t *testing.T) {
	got := StripInboundMeta("  hello world  ")
	if got != "hello world" {
		t.Errorf("expected trimmed result, got %q", got)
	}
}

// --- NormalizeInlineWhitespace ---

func TestNormalizeInlineWhitespace_Collapses(t *testing.T) {
	got := NormalizeInlineWhitespace("  hello   world  ")
	if got != "hello world" {
		t.Errorf("expected collapsed whitespace, got %q", got)
	}
}

func TestNormalizeInlineWhitespace_NoChange(t *testing.T) {
	got := NormalizeInlineWhitespace("hello world")
	if got != "hello world" {
		t.Errorf("unchanged input changed: %q", got)
	}
}

func TestNormalizeInlineWhitespace_Tabs(t *testing.T) {
	got := NormalizeInlineWhitespace("a\t\tb")
	if got != "a b" {
		t.Errorf("tabs not collapsed: %q", got)
	}
}

// --- MediaPathResolver.ResolvePath ---

func TestResolvePath_AbsolutePassthrough(t *testing.T) {
	r := MediaPathResolver{BaseDir: "/base"}
	got := r.ResolvePath("/some/abs/path.mp4")
	if got != "/some/abs/path.mp4" {
		t.Errorf("absolute path should pass through: %q", got)
	}
}

func TestResolvePath_URLPassthrough(t *testing.T) {
	r := MediaPathResolver{BaseDir: "/base"}
	got := r.ResolvePath("https://example.com/file.mp4")
	if got != "https://example.com/file.mp4" {
		t.Errorf("http URL should pass through: %q", got)
	}
}

func TestResolvePath_RelativeWithBase(t *testing.T) {
	r := MediaPathResolver{BaseDir: "/base"}
	got := r.ResolvePath("file.mp4")
	if got != "/base/file.mp4" {
		t.Errorf("relative path with base expected /base/file.mp4, got %q", got)
	}
}

func TestResolvePath_RelativeNoBase(t *testing.T) {
	r := MediaPathResolver{}
	got := r.ResolvePath("file.mp4")
	if got != "file.mp4" {
		t.Errorf("relative path without base should return as-is: %q", got)
	}
}

func TestResolvePath_Empty(t *testing.T) {
	r := MediaPathResolver{BaseDir: "/base"}
	got := r.ResolvePath("")
	if got != "" {
		t.Errorf("empty path should return empty: %q", got)
	}
}

// --- ReplyInline ---

func TestReplyInline_Trims(t *testing.T) {
	got := ReplyInline("  hello  ")
	if got != "hello" {
		t.Errorf("expected trimmed, got %q", got)
	}
}

func TestReplyInline_NoChange(t *testing.T) {
	got := ReplyInline("hello")
	if got != "hello" {
		t.Errorf("unchanged input changed: %q", got)
	}
}
