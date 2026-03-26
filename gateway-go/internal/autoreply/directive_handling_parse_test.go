package autoreply

import "testing"

func TestParseFullInlineDirectives_WithExec(t *testing.T) {
	result := ParseFullInlineDirectives("/think high /exec host=sandbox hello", nil)
	if !result.HasThinkDirective {
		t.Fatal("expected think directive")
	}
	if result.ThinkLevel != ThinkHigh {
		t.Fatalf("expected ThinkHigh, got %q", result.ThinkLevel)
	}
	if !result.HasExecDirective {
		t.Fatal("expected exec directive")
	}
	if result.ExecHost != ExecHostSandbox {
		t.Fatalf("expected host=sandbox, got %q", result.ExecHost)
	}
	if result.Cleaned != "hello" {
		t.Fatalf("unexpected cleaned: %q", result.Cleaned)
	}
}

func TestParseFullInlineDirectives_NoDirectives(t *testing.T) {
	result := ParseFullInlineDirectives("just a regular message", nil)
	if result.HasThinkDirective || result.HasExecDirective || result.HasModelDirective {
		t.Fatal("expected no directives")
	}
	if result.Cleaned != "just a regular message" {
		t.Fatalf("unexpected cleaned: %q", result.Cleaned)
	}
}

func TestIsFullDirectiveOnly(t *testing.T) {
	directives := ParseFullInlineDirectives("/think high /fast on", nil)
	if !IsFullDirectiveOnly(directives, directives.Cleaned, false) {
		t.Fatal("expected directive-only")
	}

	directives2 := ParseFullInlineDirectives("/think high hello world", nil)
	if IsFullDirectiveOnly(directives2, directives2.Cleaned, false) {
		t.Fatal("expected not directive-only")
	}
}

func TestStripAllMentions(t *testing.T) {
	got := stripAllMentions("Hello @user1 and @user2")
	want := "Hello  and "
	if got != want {
		t.Errorf("stripAllMentions = %q, want %q", got, want)
	}
}
