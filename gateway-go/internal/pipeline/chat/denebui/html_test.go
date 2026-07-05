package denebui

import (
	"strings"
	"testing"
)

// Shared grammar vectors — docs/research/deneb-ui-html.md. The Kotlin
// (DenebUiHtmlTest.kt) and Andromeda (denebUiHtml.test.ts) suites cover the
// same scenarios; keep all three in sync when the grammar changes.

func mustParseHTML(t *testing.T, body string) map[string]any {
	t.Helper()
	root, issues := ParseHTML(body)
	if len(issues) > 0 {
		t.Fatalf("unexpected parse issues: %v", issues)
	}
	m, ok := root.(map[string]any)
	if !ok {
		t.Fatalf("root is %T, want map", root)
	}
	return m
}

func children(t *testing.T, m map[string]any) []any {
	t.Helper()
	ch, _ := m["children"].([]any)
	return ch
}

func TestParseHTML_SelectWithUnclosedOptions(t *testing.T) {
	root := mustParseHTML(t, `<select id="pick" label="선택"><option>가<option selected>나</select>`)
	if root["type"] != "select" || root["id"] != "pick" {
		t.Fatalf("bad root: %v", root)
	}
	opts, _ := root["options"].([]any)
	if len(opts) != 2 || opts[0] != "가" || opts[1] != "나" {
		t.Errorf("options = %v", opts)
	}
	if root["selected"] != "나" {
		t.Errorf("selected = %v", root["selected"])
	}
	if issues, err := Validate(`<select id="pick"><option>가</option></select>`); err != nil || len(issues) > 0 {
		t.Errorf("valid select rejected: err=%v issues=%v", err, issues)
	}
	// No selected attribute anywhere → no implicit selection.
	none := mustParseHTML(t, `<select id="p2"><option>가</option><option>나</option></select>`)
	if _, has := none["selected"]; has {
		t.Errorf("selected must be absent without a selected attr, got %v", none["selected"])
	}
}

func TestParseHTML_MultipleRootsWrapAsColumn(t *testing.T) {
	root := mustParseHTML(t, `<card><text>a</text></card><card><text>b</text></card>`)
	if root["type"] != "column" {
		t.Fatalf("want implicit column, got %v", root["type"])
	}
	if got := len(children(t, root)); got != 2 {
		t.Errorf("want 2 children, got %d", got)
	}
}

func TestParseHTML_TruncationAutoCloses(t *testing.T) {
	root := mustParseHTML(t, `<column><card><text style="body">잘림`)
	if root["type"] != "column" {
		t.Fatalf("root = %v", root["type"])
	}
	card := children(t, root)[0].(map[string]any)
	txt := children(t, card)[0].(map[string]any)
	if txt["type"] != "text" || txt["value"] != "잘림" || txt["style"] != "body" {
		t.Errorf("text node = %v", txt)
	}
	if issues, err := Validate(`<column><card><text>잘림`); err != nil || len(issues) > 0 {
		t.Errorf("truncated body must still validate: err=%v issues=%v", err, issues)
	}
}

func TestParseHTML_RawTextEntitiesAndBackticks(t *testing.T) {
	root := mustParseHTML(t, "<markdown>줄1\n&#96;&#96;&#96;go\na &lt; b &amp; c\n&#96;&#96;&#96;</markdown>")
	want := "줄1\n```go\na < b & c\n```"
	if root["type"] != "markdown" || root["value"] != want {
		t.Errorf("markdown value = %q, want %q", root["value"], want)
	}
}

func TestParseHTML_CodeRawText(t *testing.T) {
	root := mustParseHTML(t, `<code language="go">if a < b { return }</code>`)
	if root["type"] != "code" || root["language"] != "go" {
		t.Fatalf("code node = %v", root)
	}
	if root["code"] != "if a < b { return }" {
		t.Errorf("code = %q", root["code"])
	}
}

func TestParseHTML_Table(t *testing.T) {
	root := mustParseHTML(t, `<table><tr><th>이름<th>값</tr><tr><td>구리<td>9,540</tr><tr><td>환율<td>1,386</table>`)
	headers, _ := root["headers"].([]any)
	rows, _ := root["rows"].([]any)
	if len(headers) != 2 || headers[0] != "이름" {
		t.Errorf("headers = %v", headers)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	r0 := rows[0].([]any)
	if r0[0] != "구리" || r0[1] != "9,540" {
		t.Errorf("row0 = %v", r0)
	}
}

func TestParseHTML_Actions(t *testing.T) {
	root := mustParseHTML(t, `<button event="submit" data-kind="rsvp" collect="name,when">전송</button>`)
	act, _ := root["action"].(map[string]any)
	if act["type"] != "callback" || act["event"] != "submit" {
		t.Fatalf("action = %v", act)
	}
	if d, _ := act["data"].(map[string]any); d["kind"] != "rsvp" {
		t.Errorf("data = %v", act["data"])
	}
	if c, _ := act["collectFrom"].([]any); len(c) != 2 || c[0] != "name" {
		t.Errorf("collectFrom = %v", act["collectFrom"])
	}
	if root["label"] != "전송" {
		t.Errorf("label = %v", root["label"])
	}

	href := mustParseHTML(t, `<button href="https://deneb.ai">열기</button>`)
	if a := href["action"].(map[string]any); a["type"] != "open_url" || a["url"] != "https://deneb.ai" {
		t.Errorf("href action = %v", a)
	}
	tog := mustParseHTML(t, `<countdown seconds="10" toggle="panel1" label="숨기기"/>`)
	if a := tog["action"].(map[string]any); a["type"] != "toggle" || a["targetId"] != "panel1" {
		t.Errorf("toggle action = %v", a)
	}
	cp := mustParseHTML(t, `<button copy="복사할 내용">복사</button>`)
	if a := cp["action"].(map[string]any); a["type"] != "copy_to_clipboard" || a["text"] != "복사할 내용" {
		t.Errorf("copy action = %v", a)
	}
}

func TestParseHTML_UnknownTagSkippedWithIssue(t *testing.T) {
	root, issues := ParseHTML(`<column><wat>x</wat><text>ok</text></column>`)
	if len(issues) != 1 || !strings.Contains(issues[0].Msg, "unknown tag") {
		t.Fatalf("issues = %v", issues)
	}
	m := root.(map[string]any)
	ch, _ := m["children"].([]any)
	if len(ch) != 1 || ch[0].(map[string]any)["value"] != "ok" {
		t.Errorf("children = %v", ch)
	}
}

func TestValidate_HTMLInteractiveRequiresID(t *testing.T) {
	issues, err := Validate(`<input label="이름"/>`)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Msg, "requires a non-empty") {
		t.Errorf("issues = %v", issues)
	}
}

func TestParseHTML_InputVariants(t *testing.T) {
	cb := mustParseHTML(t, `<input type="checkbox" id="c1" label="확인" checked/>`)
	if cb["type"] != "checkbox" || cb["checked"] != true {
		t.Errorf("checkbox = %v", cb)
	}
	dt := mustParseHTML(t, `<input type="date" id="d1" label="날짜" value="2026-07-06" required/>`)
	if dt["type"] != "date_input" || dt["required"] != true || dt["value"] != "2026-07-06" {
		t.Errorf("date = %v", dt)
	}
	ta := mustParseHTML(t, `<textarea id="memo" label="메모">기본값</textarea>`)
	if ta["type"] != "text_input" || ta["multiline"] != true || ta["value"] != "기본값" {
		t.Errorf("textarea = %v", ta)
	}
}

func TestParseHTML_ContainerImplicitText(t *testing.T) {
	root := mustParseHTML(t, `<card>제목 텍스트<text style="caption">부제</text></card>`)
	ch := children(t, root)
	if len(ch) != 2 {
		t.Fatalf("children = %v", ch)
	}
	if ch[0].(map[string]any)["value"] != "제목 텍스트" {
		t.Errorf("implicit text = %v", ch[0])
	}
}

func TestParseHTML_ChartAndChips(t *testing.T) {
	chart := mustParseHTML(t, `<chart type="line" label="주간"><point label="월" value="1"/><point label="화" value="2.5"/></chart>`)
	if chart["chartType"] != "line" {
		t.Errorf("chartType = %v", chart["chartType"])
	}
	if v := chart["values"].([]any); len(v) != 2 || v[1] != 2.5 {
		t.Errorf("values = %v", v)
	}
	chips := mustParseHTML(t, `<chips id="tags" selection="multi"><chip value="a">에이</chip><chip>비</chip></chips>`)
	cs := chips["chips"].([]any)
	c0 := cs[0].(map[string]any)
	c1 := cs[1].(map[string]any)
	if c0["label"] != "에이" || c0["value"] != "a" || c1["value"] != "비" {
		t.Errorf("chips = %v", cs)
	}
}

func TestValidate_HTMLEnumViolation(t *testing.T) {
	issues, err := Validate(`<alert severity="fatal">큰일</alert>`)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Msg, "invalid severity") {
		t.Errorf("issues = %v", issues)
	}
}
