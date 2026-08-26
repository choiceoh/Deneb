package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
)

// The detail string is read by the MODEL on its next turn, so an enum like
// "additional_block" (shipped verbatim until 2026-08-26) tells the author
// nothing about what to change.
func TestCardRejectionDetailIsActionableKorean(t *testing.T) {
	if got := cardRejectionDetail(denebui.Rejection{
		Reason: "schema_issues",
		Issue:  `$.children[0]: node "text_input" requires a non-empty "id"`,
	}); !strings.Contains(got, "text_input") {
		t.Fatalf("a schema issue must pass through verbatim: %q", got)
	}

	for _, reason := range []string{
		"additional_block", "unparseable", "schema_issues",
		"html_additional_block", "html_oversize", "html_not_markup",
	} {
		got := cardRejectionDetail(denebui.Rejection{Reason: reason})
		if strings.Contains(got, reason) {
			t.Fatalf("%s: raw enum leaked into the model-facing text: %q", reason, got)
		}
		if strings.TrimSpace(got) == "" {
			t.Fatalf("%s: empty detail", reason)
		}
	}

	if got := cardRejectionDetail(denebui.Rejection{Reason: "additional_block"}); !strings.Contains(got, "하나만") {
		t.Fatalf("additional_block must state the one-fence rule: %q", got)
	}
}
