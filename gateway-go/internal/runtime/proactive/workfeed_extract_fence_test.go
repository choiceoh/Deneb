package proactive

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
)

// A proactive body that is one big deneb-ui fence (the morning-letter shape)
// used to defeat every text heuristic: the fence-skipping scans came back
// empty (generic "업무 리포트" title) and the LLM titler, fed raw markup,
// echoed "```deneb-ui" as the summary (2026-07-12 live feed). The extraction
// pipeline now reads the card's prose projection.
func TestCardExtractionReadsDenebUIFenceProse(t *testing.T) {
	body := "```deneb-ui\n" +
		"<column>\n" +
		"<row><icon name=\"sun\" size=\"20\"/><text style=\"headline\">2026년 7월 12일 일요일 ☀️</text></row>\n" +
		"<text style=\"caption\">주말입니다. 다음 주 핵심: 강진·당진 계약 협상 마무리.</text>\n" +
		"</column>\n" +
		"```"
	src := denebui.ReplaceFences(body, denebui.PlainText)

	title, titleLine := extractCardTitle(src)
	if title != "2026년 7월 12일 일요일 ☀️" {
		t.Errorf("title = %q, want the card headline", title)
	}
	summary := extractCardSummary(src, titleLine)
	if !strings.Contains(summary, "주말입니다") {
		t.Errorf("summary = %q, want the card caption prose", summary)
	}
	if strings.Contains(summary, "deneb-ui") || strings.Contains(title, "deneb-ui") {
		t.Errorf("fence markup leaked: title=%q summary=%q", title, summary)
	}
}

func TestPushPreviewParsesCardHeadlineFromFence(t *testing.T) {
	body := "```deneb-ui\n<column><text style=\"headline\">아침 브리핑</text><text>본문 첫 줄.</text></column>\n```"
	if got := pushPreview(body); got != "아침 브리핑" {
		t.Errorf("pushPreview = %q, want the card headline", got)
	}
}
