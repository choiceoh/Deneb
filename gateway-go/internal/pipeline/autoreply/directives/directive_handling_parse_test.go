package directives

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

func TestParseFullInlineDirectives_WithExec(t *testing.T) {
	result := ParseFullInlineDirectives("/think high /exec host=sandbox hello", nil)
	if !result.HasThinkDirective {
		t.Fatal("expected think directive")
	}
	if result.ThinkLevel != types.ThinkHigh {
		t.Fatalf("got %q, want types.ThinkHigh", result.ThinkLevel)
	}
	if !result.HasExecDirective {
		t.Fatal("expected exec directive")
	}
	if result.ExecHost != ExecHostSandbox {
		t.Fatalf("got %q, want host=sandbox", result.ExecHost)
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

func TestParseFullInlineDirectives_AllDirectives(t *testing.T) {
	result := ParseFullInlineDirectives("/think high /verbose on /fast on /reasoning on /exec host=gateway", nil)
	if !result.HasThinkDirective {
		t.Fatal("expected think directive")
	}
	if !result.HasVerboseDirective {
		t.Fatal("expected verbose directive")
	}
	if !result.HasFastDirective {
		t.Fatal("expected fast directive")
	}
	if !result.HasReasoningDirective {
		t.Fatal("expected reasoning directive")
	}
	if !result.HasExecDirective {
		t.Fatal("expected exec directive")
	}
}

func TestParseFullInlineDirectives_ExecWithMultipleOptions(t *testing.T) {
	result := ParseFullInlineDirectives("/exec host=sandbox security=full ask=always", nil)
	if !result.HasExecDirective {
		t.Fatal("expected exec directive")
	}
	if result.ExecHost != ExecHostSandbox {
		t.Fatalf("got %q, want host=sandbox", result.ExecHost)
	}
	if result.ExecSecurity != ExecSecurityFull {
		t.Fatalf("got %q, want security=full", result.ExecSecurity)
	}
	if result.ExecAsk != ExecAskAlways {
		t.Fatalf("got %q, want ask=always", result.ExecAsk)
	}
	if !result.HasExecOptions {
		t.Fatal("expected HasExecOptions")
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

func TestIsFullDirectiveOnly_WithExec(t *testing.T) {
	directives := ParseFullInlineDirectives("/exec host=sandbox", nil)
	if !IsFullDirectiveOnly(directives, directives.Cleaned, false) {
		t.Fatal("expected directive-only with /exec")
	}
}

func TestIsFullDirectiveOnly_Group(t *testing.T) {
	// In group context, @mentions are stripped before checking.
	directives := ParseFullInlineDirectives("/think high", nil)
	cleaned := "@botname " + directives.Cleaned
	if !IsFullDirectiveOnly(directives, cleaned, true) {
		t.Fatal("expected directive-only after mention stripping")
	}
}

func TestStripAllMentions(t *testing.T) {
	got := stripAllMentions("Hello @user1 and @user2")
	want := "Hello  and "
	if got != want {
		t.Errorf("stripAllMentions = %q, want %q", got, want)
	}
}
