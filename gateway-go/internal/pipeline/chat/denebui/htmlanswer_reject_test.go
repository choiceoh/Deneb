package denebui

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A degraded deneb-html page reaches the user as a raw code block. Its author
// heard nothing until now, while the deneb-ui path has carried a correction
// hint since #4753 — the same divergence this fixes.
func TestDegradedHTMLAnswerIsReported(t *testing.T) {
	oversize := "<div>" + strings.Repeat("가", MaxHTMLAnswerBytes) + "</div>"
	text := "설명\n\n```deneb-html\n" + oversize + "\n```"

	out, rejections := NormalizeFinalReplyWithRejections(text, "client:main", quietLogger())

	if len(rejections) != 1 || rejections[0].Reason != "html_oversize" {
		t.Fatalf("oversize page must be reported: %+v", rejections)
	}
	if !strings.Contains(out, "```html") {
		t.Fatalf("degraded page still reaches the user as a code block:\n%s", out[:120])
	}
}

func TestSecondHTMLAnswerIsReported(t *testing.T) {
	text := "```deneb-html\n<div>첫째</div>\n```\n\n```deneb-html\n<div>둘째</div>\n```"

	_, rejections := NormalizeFinalReplyWithRejections(text, "client:main", quietLogger())

	if len(rejections) != 1 || rejections[0].Reason != "html_additional_block" {
		t.Fatalf("the second page must be reported: %+v", rejections)
	}
}

func TestValidHTMLAnswerReportsNothing(t *testing.T) {
	text := "```deneb-html\n<div class=\"card\">매출</div>\n```"

	out, rejections := NormalizeFinalReplyWithRejections(text, "client:main", quietLogger())

	if len(rejections) != 0 {
		t.Fatalf("a valid page must not be reported: %+v", rejections)
	}
	if !strings.Contains(out, "deneb-html") {
		t.Fatalf("valid page must keep its fence:\n%s", out)
	}
}
