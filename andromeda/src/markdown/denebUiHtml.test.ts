// Shared grammar vectors — docs/research/deneb-ui-html.md (repo root). The
// gateway (html_test.go) and native (DenebUiHtmlTest.kt) suites cover the same
// scenarios; keep all three in sync when the grammar changes.
import { describe, expect, it } from "vitest";

import { hasInteractiveNode, parseDenebUi, splitDenebUi } from "./denebUiParse";

describe("parseDenebUi (labeled HTML)", () => {
  it("parses a letter card skeleton", () => {
    const root = parseDenebUi(`<column>
      <card>
        <row><icon name="calendar" size="16"/><text style="caption">내일 일정</text></row>
        <ul><li>10:00 — 분기 리뷰</li><li>15:00 — 거래처 콜</li></ul>
      </card>
      <card>
        <row><icon name="alarm" size="16"/><text style="caption">임박 마감</text></row>
        <row><text style="body">부가세 신고</text><badge>D-2</badge></row>
      </card>
    </column>`);
    expect(root.type).toBe("column");
    expect(root.children).toHaveLength(2);
    const sched = root.children[0];
    expect(sched.type).toBe("card");
    expect(sched.children[0].children[0]).toMatchObject({ type: "icon", name: "calendar", size: 16 });
    expect(sched.children[1]).toMatchObject({ type: "list" });
    expect(sched.children[1].items[0]).toMatchObject({ type: "text", value: "10:00 — 분기 리뷰" });
    const badge = root.children[1].children[1].children[1];
    expect(badge).toMatchObject({ type: "badge", value: "D-2" });
  });

  it("tolerates unclosed options and maps selected", () => {
    const sel = parseDenebUi(`<select id="pick" label="선택"><option>가<option selected>나</select>`);
    expect(sel).toMatchObject({ type: "select", id: "pick", options: ["가", "나"], selected: "나" });
    const none = parseDenebUi(`<select id="p2"><option>가</option><option>나</option></select>`);
    expect(none.selected).toBeUndefined();
  });

  it("when wraps multiple roots into an implicit column", () => {
    const root = parseDenebUi("<card><text>a</text></card><card><text>b</text></card>");
    expect(root.type).toBe("column");
    expect(root.children).toHaveLength(2);
  });

  it("auto-closes at EOF (stream truncation)", () => {
    const root = parseDenebUi(`<column><card><text style="body">잘림`);
    expect(root.children[0].children[0]).toMatchObject({ type: "text", value: "잘림", style: "body" });
  });

  it("decodes entities and escaped backticks in raw text", () => {
    const md = parseDenebUi("<markdown>줄1\n&#96;&#96;&#96;go\na &lt; b &amp; c\n&#96;&#96;&#96;</markdown>");
    expect(md).toMatchObject({ type: "markdown", value: "줄1\n```go\na < b & c\n```" });
  });

  it("preserves raw code content including bare angle brackets", () => {
    const code = parseDenebUi(`<code language="go">if a < b { return }</code>`);
    expect(code).toMatchObject({ type: "code", language: "go", code: "if a < b { return }" });
  });

  it("when maps th row to headers and td rows to cells", () => {
    const table = parseDenebUi("<table><tr><th>이름<th>값</tr><tr><td>구리<td>9,540</tr></table>");
    expect(table.headers).toEqual(["이름", "값"]);
    expect(table.rows).toEqual([["구리", "9,540"]]);
  });

  it("when maps action attributes by precedence", () => {
    const cb = parseDenebUi(`<button event="submit" data-kind="rsvp" collect="name,when">전송</button>`);
    expect(cb.label).toBe("전송");
    expect(cb.action).toMatchObject({
      type: "callback",
      event: "submit",
      data: { kind: "rsvp" },
      collectFrom: ["name", "when"],
    });
    const open = parseDenebUi(`<button href="https://deneb.ai">열기</button>`);
    expect(open.action).toMatchObject({ type: "open_url", url: "https://deneb.ai" });
    const tog = parseDenebUi(`<countdown seconds="10" toggle="panel1"/>`);
    expect(tog.action).toMatchObject({ type: "toggle", targetId: "panel1" });
    const cp = parseDenebUi(`<button copy="내용">복사</button>`);
    expect(cp.action).toMatchObject({ type: "copy_to_clipboard", text: "내용" });
  });

  it("unwraps unknown tags so content survives", () => {
    // Tolerance round: the unknown wrapper drops but its content hoists.
    const root = parseDenebUi("<column><wat>x</wat><text>ok</text></column>");
    expect(root.children).toHaveLength(2);
    expect(root.children[0]).toMatchObject({ type: "text", value: "x" });
    expect(root.children[1]).toMatchObject({ type: "text", value: "ok" });
  });

  it("when unwraps generic html wrappers transparently", () => {
    const root = parseDenebUi("<div><card><text>안</text></card></div>");
    expect(root.type).toBe("card");
    expect(root.children[0]).toMatchObject({ type: "text", value: "안" });
  });

  it("when unwraps table sections keeping their rows", () => {
    const root = parseDenebUi(
      "<table><thead><tr><th>이름</th><th>값</th></tr></thead><tbody><tr><td>구리</td><td>9,540</td></tr></tbody></table>",
    );
    expect(root).toMatchObject({ type: "table", headers: ["이름", "값"], rows: [["구리", "9,540"]] });
  });

  it("merges inline formatting tags into the text flow", () => {
    const card = parseDenebUi("<card>이건 <b>중요</b>한 일</card>");
    expect(card.children).toHaveLength(1);
    expect(card.children[0]).toMatchObject({ type: "text", value: "이건 **중요**한 일" });
  });

  it("preserves a separating space between adjacent inline runs", () => {
    // Dropping the whitespace-only run would glue the markers ("**A****B**")
    // and break the inline markdown pass.
    const card = parseDenebUi(`<card><b>최종 결론:</b> <b>"지금 사라"</b></card>`);
    expect(card.children).toHaveLength(1);
    expect(card.children[0]).toMatchObject({ type: "text", value: `**최종 결론:** **"지금 사라"**` });
    const text = parseDenebUi("<text><b>A</b> <b>B</b></text>");
    expect(text).toMatchObject({ type: "text", value: "**A** **B**" });
  });

  it("when merges inline anchor and code into the text value", () => {
    const text = parseDenebUi(`<text>보고서 <a href="https://x">링크</a>는 <code>make ci</code>로</text>`);
    expect(text).toMatchObject({ type: "text", value: "보고서 [링크](https://x)는 `make ci`로" });
  });

  it("when suppresses inline markers in plain-value slots", () => {
    const badge = parseDenebUi("<badge><b>긴급</b></badge>");
    expect(badge).toMatchObject({ type: "badge", value: "긴급" });
  });

  it("when auto-upgrades a markdown table inside a card to a markdown node", () => {
    const card = parseDenebUi("<card>\n| 항목 | 값 |\n|---|---|\n| a | 1 |\n</card>");
    expect(card.children).toHaveLength(1);
    expect(card.children[0].type).toBe("markdown");
    expect(card.children[0].value).toContain("| 항목 | 값 |");
  });

  it("when upgrades a text node carrying a markdown block", () => {
    expect(parseDenebUi("<text>- 하나\n- 둘</text>").type).toBe("markdown");
  });

  it("when maps heading and paragraph aliases to text nodes", () => {
    const root = parseDenebUi("<column><h2>제목</h2><p>본문</p></column>");
    expect(root.children[0]).toMatchObject({ type: "text", value: "제목", style: "title" });
    expect(root.children[1]).toMatchObject({ type: "text", value: "본문" });
  });

  it("parses lenient numbers and canonicalizes enums", () => {
    const root = parseDenebUi(
      `<column><progress value="68%"/>` +
        `<chart type="column" label="생산"><point label="A" value="1,200톤"/></chart>` +
        `<badge color="red">지연</badge><alert severity="warn">확인</alert></column>`,
    );
    expect(root.children[0]).toMatchObject({ type: "progress", value: 0.68 });
    expect(root.children[1]).toMatchObject({ type: "chart", chartType: "bar", values: [1200] });
    expect(root.children[2]).toMatchObject({ type: "badge", color: "error" });
    expect(root.children[3]).toMatchObject({ type: "alert", severity: "warning" });
  });

  it("when maps input variants to typed nodes", () => {
    expect(parseDenebUi(`<input type="checkbox" id="c1" label="확인" checked/>`)).toMatchObject({
      type: "checkbox",
      id: "c1",
      checked: true,
    });
    expect(parseDenebUi(`<input type="date" id="d1" value="2026-07-06" required/>`)).toMatchObject({
      type: "date_input",
      id: "d1",
      value: "2026-07-06",
      required: true,
    });
    expect(parseDenebUi(`<textarea id="memo">기본값</textarea>`)).toMatchObject({
      type: "text_input",
      id: "memo",
      multiline: true,
      value: "기본값",
    });
  });

  it("when turns container bare text into implicit text nodes", () => {
    const card = parseDenebUi(`<card>제목 텍스트<text style="caption">부제</text></card>`);
    expect(card.children).toHaveLength(2);
    expect(card.children[0]).toMatchObject({ type: "text", value: "제목 텍스트" });
  });

  it("parses chart points and chips from children", () => {
    const chart = parseDenebUi(
      `<chart type="line"><point label="월" value="1"/><point label="화" value="2.5"/></chart>`,
    );
    expect(chart).toMatchObject({ type: "chart", chartType: "line", labels: ["월", "화"], values: [1, 2.5] });
    const chips = parseDenebUi(`<chips id="tags" selection="multi"><chip value="a">에이</chip><chip>비</chip></chips>`);
    expect(chips.chips).toEqual([
      { label: "에이", value: "a" },
      { label: "비", value: "비" },
    ]);
  });

  it("parses the gateway's collapsed accordion shape", () => {
    const root = parseDenebUi(
      '<accordion title="📬 탑솔라 &lt;견적&gt;">\n<markdown>## 분석\n&#96;&#96;&#96;go\ncode\n&#96;&#96;&#96;</markdown>\n</accordion>',
    );
    expect(root.type).toBe("accordion");
    expect(root.title).toBe("📬 탑솔라 <견적>");
    expect(root.children[0].type).toBe("markdown");
    expect(root.children[0].value).toContain("```go");
  });

  it("still parses legacy JSON bodies strictly", () => {
    expect(parseDenebUi('{"type":"text","value":"Hi"}')).toMatchObject({ type: "text", value: "Hi" });
    expect(parseDenebUi('{"type":"a"}\n{"type":"b"}')).toMatchObject({ type: "column" });
  });

  it("survives a truncated close tag with Unicode case-fold hazards", () => {
    // Go-side fuzzer regression: "</Code" without '>' + 'İ' whose toLowerCase()
    // changes string length — the close-tag search must fold ASCII manually.
    expect(parseDenebUi("<Code>İstanbul x</Code")).toMatchObject({ type: "code", code: "İstanbul x" });
    expect(parseDenebUi("<markdown>İİİİ 열린 채 끝")).toMatchObject({ type: "markdown", value: "İİİİ 열린 채 끝" });
  });

  it("hasInteractiveNode distinguishes display trees from forms", () => {
    expect(hasInteractiveNode(parseDenebUi("<card><text>본문</text><badge>D-2</badge></card>"))).toBe(false);
    expect(hasInteractiveNode(parseDenebUi(`<card><input id="n" label="이름"/></card>`))).toBe(true);
    expect(hasInteractiveNode(parseDenebUi(`<tabs><tab label="a"><button event="e">전송</button></tab></tabs>`))).toBe(
      true,
    );
  });

  it("carries the partial body on ui-pending segments for progressive render", () => {
    const segs = splitDenebUi("머리말\n```deneb-ui\n<column><card><text>부분");
    const last = segs.at(-1);
    expect(last?.kind).toBe("ui-pending");
    if (last?.kind === "ui-pending") {
      const partial = parseDenebUi(last.body);
      expect(partial).toMatchObject({ type: "column" });
      expect(hasInteractiveNode(partial)).toBe(false);
    }
  });
  it("promotes invented tags (title/label/spacer/kv) to typed aliases", () => {
    // 2026-07-18 reject telemetry — gateway/Kotlin parity.
    const root = parseDenebUi(
      '<card><title>실사 보고</title><label>발신</label><kv label="발신">양도현</kv><spacer/><text>본문</text></card>',
    );
    expect(root).toMatchObject({ type: "card" });
    const kids = root.children as Array<Record<string, unknown>>;
    expect(kids).toHaveLength(5);
    expect(kids[0]).toMatchObject({ type: "text", style: "title", value: "실사 보고" });
    expect(kids[1]).toMatchObject({ type: "text", style: "caption", value: "발신" });
    expect(kids[2]).toMatchObject({ type: "text", value: "발신 — 양도현" });
    expect(kids[3]).toMatchObject({ type: "divider" });
  });
});

describe("longpress action (gateway/native parity)", () => {
  it("attaches a press-hold callback to a row from longpress= + data-*", () => {
    const root = parseDenebUi(
      '<row longpress="deadline_done" data-path="프로젝트/대한전선"><text>대한전선 마감</text></row>',
    );
    expect(root).toMatchObject({ type: "row" });
    expect(root.longPressAction).toMatchObject({
      type: "callback",
      event: "deadline_done",
      data: { path: "프로젝트/대한전선" },
    });
  });

  it("leaves a plain row without a longPressAction", () => {
    const root = parseDenebUi("<row><text>일반</text></row>");
    expect(root.longPressAction).toBeUndefined();
  });
});
