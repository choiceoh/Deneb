package rules

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func TestParseInlineDirectivesFacade(t *testing.T) {
	parsed := ParseInlineDirectives("/think high /verbose off hello", nil)
	if !parsed.HasThinkDirective || parsed.ThinkLevel != types.ThinkHigh {
		t.Fatalf("think directive not parsed: %#v", parsed)
	}
	if parsed.Cleaned != "hello" {
		t.Fatalf("expected cleaned body to be hello, got %q", parsed.Cleaned)
	}
}

func TestResolveTypingModeFacade(t *testing.T) {
	mode := ResolveTypingMode(TypingModeContext{IsGroupChat: true})
	if mode != TypingModeMessage {
		t.Fatalf("expected group default typing mode, got %q", mode)
	}
}
