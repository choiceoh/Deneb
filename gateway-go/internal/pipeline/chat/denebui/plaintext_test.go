package denebui

import (
	"strings"
	"testing"
)

func TestPlainTextProjectsCardProseInOrder(t *testing.T) {
	body := `<column>
		<row><icon name="sun" size="20"/><text style="headline">7월 12일 일요일 ☀️</text></row>
		<text style="caption">주말입니다. 다음 주 핵심: 계약 협상 마무리.</text>
		<row><stat value="1,386" label="USD/KRW"/></row>
		<ul><li>10:00 — 회의</li></ul>
		<alert severity="warning" title="주의">기한 임박</alert>
		<table><tr><th>현장</th><th>납기</th></tr><tr><td>화성</td><td>7/10</td></tr></table>
	</column>`
	got := PlainText(body)
	wantInOrder := []string{
		"7월 12일 일요일 ☀️",
		"주말입니다. 다음 주 핵심: 계약 협상 마무리.",
		"USD/KRW 1,386",
		"10:00 — 회의",
		"주의",
		"기한 임박",
		"현장 · 납기",
		"화성 · 7/10",
	}
	last := -1
	for _, w := range wantInOrder {
		idx := strings.Index(got, w)
		if idx < 0 {
			t.Fatalf("PlainText missing %q in:\n%s", w, got)
		}
		if idx < last {
			t.Fatalf("PlainText order violated at %q in:\n%s", w, got)
		}
		last = idx
	}
	if strings.Contains(got, "<") {
		t.Errorf("PlainText leaked markup: %q", got)
	}
}

func TestPlainTextOutOfScopeInputs(t *testing.T) {
	if got := PlainText(`{"type":"text","value":"레거시"}`); got != "" {
		t.Errorf("legacy JSON should project to empty, got %q", got)
	}
	if got := PlainText(""); got != "" {
		t.Errorf("empty body should project to empty, got %q", got)
	}
}

func TestReplaceFencesProjectsAndPreservesProse(t *testing.T) {
	text := "앞 문장.\n```deneb-ui\n<column><text style=\"headline\">브리핑</text></column>\n```\n뒤 문장."
	got := ReplaceFences(text, PlainText)
	want := "앞 문장.\n브리핑\n뒤 문장."
	if got != want {
		t.Errorf("ReplaceFences = %q, want %q", got, want)
	}
}

func TestReplaceFencesKeepsGluedProsePrefix(t *testing.T) {
	// #3499 leniency: an opener glued to the tail of a prose sentence.
	text := "정리했어요.```deneb-ui\n<column><text>본문</text></column>\n```"
	got := ReplaceFences(text, PlainText)
	if !strings.Contains(got, "정리했어요.") || !strings.Contains(got, "본문") {
		t.Errorf("glued prefix or body lost: %q", got)
	}
	if strings.Contains(got, "```") {
		t.Errorf("fence markup leaked: %q", got)
	}
}

func TestReplaceFencesDropsUnprojectableBlock(t *testing.T) {
	text := "앞.\n```deneb-ui\n{\"type\":\"card\"}\n```\n뒤."
	got := ReplaceFences(text, PlainText)
	if strings.Contains(got, "deneb-ui") || strings.Contains(got, "card") {
		t.Errorf("unprojectable block should drop, got %q", got)
	}
	if !strings.Contains(got, "앞.") || !strings.Contains(got, "뒤.") {
		t.Errorf("surrounding prose lost: %q", got)
	}
}

func TestReplaceFencesNoFencePassthrough(t *testing.T) {
	text := "펜스 없는 본문\n그대로."
	if got := ReplaceFences(text, PlainText); got != text {
		t.Errorf("no-fence text must pass through unchanged, got %q", got)
	}
}
