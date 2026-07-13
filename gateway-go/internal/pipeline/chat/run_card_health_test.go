package chat

import (
	"context"
	"log/slog"
	"testing"
)

// recordingHandler captures emitted log messages so a test can assert which
// adoption signal a turn produced.
type recordingHandler struct{ msgs *[]string }

func (h recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordingHandler) Handle(_ context.Context, r slog.Record) error {
	*h.msgs = append(*h.msgs, r.Message)
	return nil
}
func (h recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordingHandler) WithGroup(string) slog.Handler      { return h }

func captureLogger() (*[]string, *slog.Logger) {
	msgs := &[]string{}
	return msgs, slog.New(recordingHandler{msgs: msgs})
}

func contains(msgs []string, want string) bool {
	for _, m := range msgs {
		if m == want {
			return true
		}
	}
	return false
}

const (
	msgCardAuthored = "deneb-ui card authored"
	msgAdoptionMiss = "deneb-ui adoption miss — structured answer without a card (heuristic)"
)

// A card turn must emit the card-authored denominator signal (not a miss), and a
// structured no-card answer must emit the miss (not authored) — so grepping the
// journal for the two yields a real adoption rate.
func TestReportDenebUICardHealth_AdoptionSignals(t *testing.T) {
	authored, log1 := captureLogger()
	reportDenebUICardHealth("```deneb-ui\n<card><text>보고</text></card>\n```", "client:main", log1)
	if !contains(*authored, msgCardAuthored) {
		t.Fatal("a card turn must emit the card-authored adoption signal")
	}
	if contains(*authored, msgAdoptionMiss) {
		t.Fatal("a card turn must not also count as an adoption miss")
	}

	miss, log2 := captureLogger()
	reportDenebUICardHealth("| 항목 | 상태 |\n|---|---|\n| a | b |", "client:main", log2)
	if !contains(*miss, msgAdoptionMiss) {
		t.Fatal("a structured no-card answer must emit an adoption miss")
	}
	if contains(*miss, msgCardAuthored) {
		t.Fatal("a no-card answer must not emit the card-authored signal")
	}

	// Plain short prose is neither — no adoption signal at all.
	neither, log3 := captureLogger()
	reportDenebUICardHealth("짧은 답변입니다.", "client:main", log3)
	if contains(*neither, msgCardAuthored) || contains(*neither, msgAdoptionMiss) {
		t.Fatal("plain prose must emit no adoption signal")
	}
}

// Adoption-miss heuristic: markdown tables and long bullet runs are the shapes
// the authoring contract says should be cards; short prose and short lists are not.
func TestLooksStructuredWithoutCard(t *testing.T) {
	if !looksStructuredWithoutCard("| 항목 | 상태 |\n|---|---|\n| a | b |") {
		t.Fatal("markdown table must count as structured")
	}
	if !looksStructuredWithoutCard("| 항목 | 상태 |\n|:---|---:|\n| a | b |") {
		t.Fatal("table separator with alignment colons must count as structured")
	}
	if !looksStructuredWithoutCard("| 항목 | 상태 |\n| :--- | ---: |\n| a | b |") {
		t.Fatal("spaced alignment separator must count as structured")
	}
	if looksStructuredWithoutCard("diff 헤더 예시\n|--- a/file.go 처럼 본문에 섞인 대시") {
		t.Fatal("dashes mixed into prose must not count as a table separator")
	}
	if looksStructuredWithoutCard("|-|\n짧은 답변입니다.") {
		t.Fatal("separator without a 3-dash cell must not count")
	}
	if looksStructuredWithoutCard("|--:--|---|\n짧은 답변입니다.") {
		t.Fatal("colon inside a dash run is not a valid separator cell")
	}
	if looksStructuredWithoutCard("| |---|\n짧은 답변입니다.") {
		t.Fatal("an empty cell disqualifies the row as a separator")
	}
	long := "서두 설명 문장입니다. "
	for i := 0; i < 70; i++ {
		long += "채움 문장. "
	}
	long += "\n- 하나\n- 둘\n- 셋\n- 넷\n- 다섯\n"
	if !looksStructuredWithoutCard(long) {
		t.Fatal("5+ bullets in a long answer must count as structured")
	}
	if looksStructuredWithoutCard("짧은 답변입니다.\n- 하나\n- 둘") {
		t.Fatal("short prose with two bullets must not count")
	}
}
