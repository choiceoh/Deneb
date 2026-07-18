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

func TestParseHTML_TableSectionsUnwrapKeepingRows(t *testing.T) {
	// <thead>/<tbody> are HTML fluency: the wrappers unwrap and their row
	// structurals must hoist to the table (not vanish with the wrapper).
	root := mustParseHTML(t, `<table><thead><tr><th>이름</th><th>값</th></tr></thead><tbody><tr><td>구리</td><td>9,540</td></tr></tbody></table>`)
	headers, _ := root["headers"].([]any)
	rows, _ := root["rows"].([]any)
	if len(headers) != 2 || headers[0] != "이름" {
		t.Errorf("headers = %v", headers)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	if r0 := rows[0].([]any); r0[0] != "구리" || r0[1] != "9,540" {
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

func TestParseHTML_UnknownTagUnwrapsWithIssue(t *testing.T) {
	root, issues := ParseHTML(`<column><wat>x</wat><text>ok</text></column>`)
	if len(issues) != 1 || !strings.Contains(issues[0].Msg, "unknown tag") {
		t.Fatalf("issues = %v", issues)
	}
	// Tolerance round: the unknown wrapper drops but its content hoists.
	m := root.(map[string]any)
	ch, _ := m["children"].([]any)
	if len(ch) != 2 || ch[0].(map[string]any)["value"] != "x" || ch[1].(map[string]any)["value"] != "ok" {
		t.Errorf("children = %v", ch)
	}
}

func TestParseHTML_GenericWrapperUnwraps(t *testing.T) {
	root, issues := ParseHTML(`<div><card><text>안</text></card></div>`)
	if len(issues) != 0 {
		t.Fatalf("div soup must not raise issues, got %v", issues)
	}
	if got := root.(map[string]any)["type"]; got != "card" {
		t.Errorf("root = %v, want unwrapped card", got)
	}
}

func TestParseHTML_InlineTagsMergeIntoTextFlow(t *testing.T) {
	root := mustParseHTML(t, `<card>이건 <b>중요</b>한 일</card>`)
	ch := children(t, root)
	if len(ch) != 1 {
		t.Fatalf("children = %v, want single merged text", ch)
	}
	if got := ch[0].(map[string]any)["value"]; got != "이건 **중요**한 일" {
		t.Errorf("value = %q", got)
	}
}

func TestParseHTML_InlineRunsKeepSeparatingSpace(t *testing.T) {
	// A whitespace-only run between two inline elements must survive as one
	// space — dropping it glues the markers ("**A****B**") and breaks the
	// renderers' inline markdown pass.
	root := mustParseHTML(t, `<card><b>최종 결론:</b> <b>"지금 사라"</b></card>`)
	ch := children(t, root)
	if len(ch) != 1 {
		t.Fatalf("children = %v, want single merged text", ch)
	}
	if got := ch[0].(map[string]any)["value"]; got != `**최종 결론:** **"지금 사라"**` {
		t.Errorf("value = %q", got)
	}
	// Same flow inside a value slot.
	txt := mustParseHTML(t, `<text><b>A</b> <b>B</b></text>`)
	if got := txt["value"]; got != "**A** **B**" {
		t.Errorf("text value = %q", got)
	}
}

func TestParseHTML_ListOrderedOnlyWhenAsked(t *testing.T) {
	ul := mustParseHTML(t, `<ul><li>하나</li><li>둘</li></ul>`)
	if _, has := ul["ordered"]; has {
		t.Errorf("<ul> must not be ordered, got %v", ul["ordered"])
	}
	ol := mustParseHTML(t, `<ol><li>하나</li></ol>`)
	if ol["ordered"] != true {
		t.Errorf("<ol> ordered = %v, want true", ol["ordered"])
	}
	attr := mustParseHTML(t, `<list ordered><li>하나</li></list>`)
	if attr["ordered"] != true {
		t.Errorf("<list ordered> ordered = %v, want true", attr["ordered"])
	}
}

func TestParseHTML_InlineAnchorAndCode(t *testing.T) {
	root := mustParseHTML(t, `<text>보고서 <a href="https://x">링크</a>는 <code>make ci</code>로</text>`)
	if got := root["value"]; got != "보고서 [링크](https://x)는 `make ci`로" {
		t.Errorf("value = %q", got)
	}
}

func TestParseHTML_InlineMarkerSuppressedInPlainValueSlots(t *testing.T) {
	root := mustParseHTML(t, `<badge><b>긴급</b></badge>`)
	if got := root["value"]; got != "긴급" {
		t.Errorf("badge value = %q, want bare text without markers", got)
	}
}

func TestParseHTML_MarkdownTableAutoUpgrades(t *testing.T) {
	root := mustParseHTML(t, "<card>\n| 항목 | 값 |\n|---|---|\n| a | 1 |\n</card>")
	ch := children(t, root)
	if len(ch) != 1 || ch[0].(map[string]any)["type"] != "markdown" {
		t.Fatalf("children = %v, want one markdown node", ch)
	}
	if v := ch[0].(map[string]any)["value"].(string); !strings.Contains(v, "| 항목 | 값 |") {
		t.Errorf("markdown value = %q", v)
	}
}

func TestParseHTML_TextNodeWithMarkdownBlockUpgrades(t *testing.T) {
	root := mustParseHTML(t, "<text>- 하나\n- 둘</text>")
	if got := root["type"]; got != "markdown" {
		t.Errorf("type = %v, want markdown", got)
	}
}

func TestParseHTML_HeadingAndParagraphAliases(t *testing.T) {
	root := mustParseHTML(t, `<column><h2>제목</h2><p>본문</p></column>`)
	ch := children(t, root)
	if len(ch) != 2 {
		t.Fatalf("children = %v", ch)
	}
	h := ch[0].(map[string]any)
	if h["type"] != "text" || h["style"] != "title" || h["value"] != "제목" {
		t.Errorf("h2 = %v", h)
	}
	pn := ch[1].(map[string]any)
	if pn["type"] != "text" || pn["value"] != "본문" {
		t.Errorf("p = %v", pn)
	}
}

func TestParseHTML_LenientNumbersAndEnumCanon(t *testing.T) {
	root := mustParseHTML(t, `<column>`+
		`<progress value="68%"/>`+
		`<chart type="column" label="생산"><point label="A" value="1,200톤"/></chart>`+
		`<badge color="red">지연</badge>`+
		`<alert severity="warn">확인</alert>`+
		`</column>`)
	ch := children(t, root)
	if got := ch[0].(map[string]any)["value"]; got != 0.68 {
		t.Errorf("progress value = %v, want 0.68", got)
	}
	chart := ch[1].(map[string]any)
	if chart["chartType"] != "bar" {
		t.Errorf("chartType = %v, want bar", chart["chartType"])
	}
	if vals := chart["values"].([]any); len(vals) != 1 || vals[0] != 1200.0 {
		t.Errorf("values = %v, want [1200]", vals)
	}
	if got := ch[2].(map[string]any)["color"]; got != "error" {
		t.Errorf("badge color = %v, want error", got)
	}
	if got := ch[3].(map[string]any)["severity"]; got != "warning" {
		t.Errorf("alert severity = %v, want warning", got)
	}
}

func TestValidateReturnsIssueWhenIDMissing(t *testing.T) {
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

func TestValidateReturnsIssueForInvalidEnumValue(t *testing.T) {
	// "fatal" now canonicalizes to error; use a word with no canonical fold.
	issues, err := Validate(`<alert severity="catastrophic">큰일</alert>`)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Msg, "invalid severity") {
		t.Errorf("issues = %v", issues)
	}
}

func TestParseHTML_InventedTagAliases(t *testing.T) {
	// title/label/spacer/kv — the invented tags production models actually
	// emit (2026-07-18 reject telemetry), promoted to real aliases so they
	// render with proper typography instead of unwrapping to bare text.
	root := mustParseHTML(t,
		`<card><title>실사 보고</title><label>발신</label><kv label="발신">양도현</kv><spacer/><text>본문</text></card>`)
	kids := children(t, root)
	if len(kids) != 5 {
		t.Fatalf("want 5 children, got %d: %v", len(kids), kids)
	}
	titleNode := kids[0].(map[string]any)
	if titleNode["type"] != "text" || titleNode["style"] != "title" || titleNode["value"] != "실사 보고" {
		t.Errorf("title alias = %v", titleNode)
	}
	labelNode := kids[1].(map[string]any)
	if labelNode["type"] != "text" || labelNode["style"] != "caption" || labelNode["value"] != "발신" {
		t.Errorf("label alias = %v", labelNode)
	}
	kvNode := kids[2].(map[string]any)
	if kvNode["type"] != "text" || kvNode["value"] != "발신 — 양도현" {
		t.Errorf("kv alias = %v", kvNode)
	}
	if kids[3].(map[string]any)["type"] != "divider" {
		t.Errorf("spacer alias = %v", kids[3])
	}
	if issues, err := Validate(`<card><title>제목</title><spacer/></card>`); err != nil || len(issues) > 0 {
		t.Errorf("aliased card must validate clean: err=%v issues=%v", err, issues)
	}
}
