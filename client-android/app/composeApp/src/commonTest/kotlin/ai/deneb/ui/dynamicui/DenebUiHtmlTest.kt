package ai.deneb.ui.dynamicui

import ai.deneb.ui.markdown.DenebUiBlock
import ai.deneb.ui.markdown.parseMarkdown
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Shared grammar vectors — docs/research/deneb-ui-html.md. The gateway
 * (html_test.go) and Andromeda (denebUiHtml.test.ts) suites cover the same
 * scenarios; keep all three in sync when the grammar changes.
 */
class DenebUiHtmlTest {

    private fun parseUi(body: String): DenebUiNode {
        val result = DenebUiParser.parseUiBlockBody(body)
        val ui = assertIs<DenebUiParser.UiBlockResult.Ui>(result)
        return ui.node
    }

    @Test
    fun `parses morning letter skeleton`() {
        val html = """
            <column>
              <card>
                <row><icon name="sunny" size="16"/><text style="caption">날씨 · 광주</text></row>
                <row><text style="headline">18°</text><text style="caption">체감 16°</text></row>
                <text style="caption">최고 24° · 최저 14° · 강수 30%</text>
                <text style="body">오후 소나기 가능 — 우산 챙기세요</text>
              </card>
              <card>
                <row><icon name="payments" size="16"/><text style="caption">환율 · 구리</text></row>
                <row><stat value="1,386" label="USD/KRW"/><stat value="1,498" label="EUR/KRW"/></row>
                <stat value="${'$'}9,540 /t" label="LME 구리"/>
              </card>
              <card>
                <row><icon name="calendar" size="16"/><text style="caption">오늘 일정</text></row>
                <ul><li>09:00 — 팀 스탠드업</li><li>14:00 — 거래처 미팅</li></ul>
              </card>
              <card>
                <row><icon name="alarm" size="16"/><text style="caption">임박 마감</text></row>
                <row><text style="body">부가세 신고</text><badge>D-2</badge></row>
              </card>
            </column>
        """.trimIndent()
        val col = assertIs<ColumnNode>(parseUi(html))
        assertEquals(4, col.children.size)

        val weather = assertIs<CardNode>(col.children[0])
        val headerRow = assertIs<RowNode>(weather.children[0])
        assertEquals("sunny", assertIs<IconNode>(headerRow.children[0]).name)
        val caption = assertIs<TextNode>(headerRow.children[1])
        assertEquals("날씨 · 광주", caption.value)
        assertEquals(TextNodeStyle.CAPTION, caption.style)

        val fx = assertIs<CardNode>(col.children[1])
        val statRow = assertIs<RowNode>(fx.children[1])
        val usd = assertIs<StatNode>(statRow.children[0])
        assertEquals("1,386", usd.value)
        assertEquals("USD/KRW", usd.label)

        val sched = assertIs<CardNode>(col.children[2])
        val list = assertIs<ListNode>(sched.children[1])
        assertEquals(2, list.items.size)
        assertEquals("09:00 — 팀 스탠드업", assertIs<TextNode>(list.items[0]).value)

        val deadline = assertIs<CardNode>(col.children[3])
        val badgeRow = assertIs<RowNode>(deadline.children[1])
        assertEquals("D-2", assertIs<BadgeNode>(badgeRow.children[1]).value)
    }

    @Test
    fun `select tolerates unclosed options and maps selected`() {
        val node = assertIs<SelectNode>(
            parseUi("""<select id="pick" label="선택"><option>가<option selected>나</select>"""),
        )
        assertEquals("pick", node.id)
        assertEquals(listOf("가", "나"), node.options)
        assertEquals("나", node.selected)

        val none = assertIs<SelectNode>(parseUi("""<select id="p2"><option>가</option><option>나</option></select>"""))
        assertNull(none.selected)
    }

    @Test
    fun `multiple roots wrap into an implicit column`() {
        val node = assertIs<ColumnNode>(parseUi("<card><text>a</text></card><card><text>b</text></card>"))
        assertEquals(2, node.children.size)
    }

    @Test
    fun `truncated stream auto-closes at EOF`() {
        val node = assertIs<ColumnNode>(parseUi("""<column><card><text style="body">잘림"""))
        val card = assertIs<CardNode>(node.children[0])
        val text = assertIs<TextNode>(card.children[0])
        assertEquals("잘림", text.value)
        assertEquals(TextNodeStyle.BODY, text.style)
    }

    @Test
    fun `raw text decodes entities and escaped backticks`() {
        val node = assertIs<MarkdownNode>(
            parseUi("<markdown>줄1\n&#96;&#96;&#96;go\na &lt; b &amp; c\n&#96;&#96;&#96;</markdown>"),
        )
        assertEquals("줄1\n```go\na < b & c\n```", node.value)
    }

    @Test
    fun `code keeps raw content including bare angle brackets`() {
        val node = assertIs<CodeNode>(parseUi("""<code language="go">if a < b { return }</code>"""))
        assertEquals("go", node.language)
        assertEquals("if a < b { return }", node.code)
    }

    @Test
    fun `table maps th row to headers and td rows to cells`() {
        val node = assertIs<TableNode>(
            parseUi("<table><tr><th>이름<th>값</tr><tr><td>구리<td>9,540</tr><tr><td>환율<td>1,386</table>"),
        )
        assertEquals(listOf("이름", "값"), node.headers)
        assertEquals(2, node.rows.size)
        assertEquals(listOf("구리", "9,540"), node.rows[0])
    }

    @Test
    fun `action attributes map by precedence`() {
        val cb = assertIs<ButtonNode>(parseUi("""<button event="submit" data-kind="rsvp" collect="name,when">전송</button>"""))
        assertEquals("전송", cb.label)
        val callback = assertIs<CallbackAction>(cb.action)
        assertEquals("submit", callback.event)
        assertEquals("rsvp", callback.dataAsStrings?.get("kind"))
        assertEquals(listOf("name", "when"), callback.collectFrom)

        val open = assertIs<ButtonNode>(parseUi("""<button href="https://deneb.ai">열기</button>"""))
        assertEquals("https://deneb.ai", assertIs<OpenUrlAction>(open.action).url)

        val tog = assertIs<CountdownNode>(parseUi("""<countdown seconds="10" toggle="panel1" label="숨기기"/>"""))
        assertEquals(10, tog.seconds)
        assertEquals("panel1", assertIs<ToggleAction>(tog.action).targetId)

        val cp = assertIs<ButtonNode>(parseUi("""<button copy="복사할 내용">복사</button>"""))
        assertEquals("복사할 내용", assertIs<CopyToClipboardAction>(cp.action).text)
    }

    @Test
    fun `unknown tags are skipped with the rest rendered`() {
        val node = assertIs<ColumnNode>(parseUi("<column><wat>x</wat><text>ok</text></column>"))
        assertEquals(1, node.children.size)
        assertEquals("ok", assertIs<TextNode>(node.children[0]).value)
    }

    @Test
    fun `input variants map to typed nodes`() {
        val cb = assertIs<CheckboxNode>(parseUi("""<input type="checkbox" id="c1" label="확인" checked/>"""))
        assertEquals(true, cb.checked)
        val dt = assertIs<DateInputNode>(parseUi("""<input type="date" id="d1" label="날짜" value="2026-07-06" required/>"""))
        assertEquals("2026-07-06", dt.value)
        assertEquals(true, dt.required)
        val ti = assertIs<TextInputNode>(parseUi("""<input id="name" label="이름" placeholder="홍길동" keyboard="text"/>"""))
        assertEquals("name", ti.id)
        assertEquals("홍길동", ti.placeholder)
        val ta = assertIs<TextInputNode>(parseUi("""<textarea id="memo" label="메모">기본값</textarea>"""))
        assertEquals(true, ta.multiline)
        assertEquals("기본값", ta.value)
    }

    @Test
    fun `container bare text becomes implicit text node`() {
        val card = assertIs<CardNode>(parseUi("<card>제목 텍스트<text style=\"caption\">부제</text></card>"))
        assertEquals(2, card.children.size)
        assertEquals("제목 텍스트", assertIs<TextNode>(card.children[0]).value)
    }

    @Test
    fun `chart points and chips parse from children`() {
        val chart = assertIs<ChartNode>(
            parseUi("""<chart type="line" label="주간"><point label="월" value="1"/><point label="화" value="2.5"/></chart>"""),
        )
        assertEquals("line", chart.chartType)
        assertEquals(listOf("월", "화"), chart.labels)
        assertEquals(listOf(1f, 2.5f), chart.values)

        val chips = assertIs<ChipGroupNode>(
            parseUi("""<chips id="tags" selection="multi"><chip value="a">에이</chip><chip>비</chip></chips>"""),
        )
        assertEquals("multi", chips.selection)
        assertEquals("에이", chips.chips[0].label)
        assertEquals("a", chips.chips[0].value)
        assertEquals("비", chips.chips[1].value)
    }

    @Test
    fun `collapsed accordion html from the gateway renders`() {
        // Shape denebui.CollapsedReportFence emits (backticks escaped as &#96;).
        val body = "<accordion title=\"📬 탑솔라 &lt;견적&gt;\">\n<markdown>## 분석\n- **중요도**: 높음\n&#96;&#96;&#96;go\ncode\n&#96;&#96;&#96;</markdown>\n</accordion>"
        val node = assertIs<AccordionNode>(parseUi(body))
        assertEquals("📬 탑솔라 <견적>", node.title)
        assertNull(node.expanded)
        val md = assertIs<MarkdownNode>(node.children[0])
        assertTrue(md.value.startsWith("## 분석"))
        assertTrue("```go" in md.value)
    }

    @Test
    fun `bare html without fence is wrapped into a ui block`() {
        val bare = "<column><text>안녕</text></column>"
        val wrapped = DenebUiParser.wrapBareDenebUiContent(bare)
        assertTrue(wrapped.startsWith("```deneb-ui"))
        val blocks = parseMarkdown(wrapped).blocks
        assertEquals(1, blocks.filterIsInstance<DenebUiBlock>().size)
    }

    @Test
    fun `html block inside a chat message parses end to end`() {
        val message = "머리말 한 줄\n\n```deneb-ui\n<column><card><text style=\"body\">본문</text></card></column>\n```\n"
        val uiBlocks = parseMarkdown(message).blocks.filterIsInstance<DenebUiBlock>()
        assertEquals(1, uiBlocks.size)
        assertIs<ColumnNode>(uiBlocks[0].node)
    }

    @Test
    fun `comments and doctype are ignored`() {
        val node = assertIs<ColumnNode>(parseUi("<!DOCTYPE html><!-- 주석 --><column><text>ok</text></column><!-- 끝 -->"))
        assertEquals("ok", assertIs<TextNode>(node.children[0]).value)
    }

    @Test
    fun `literal angle bracket in plain text stays text`() {
        val node = assertIs<TextNode>(parseUi("<text>a < b 그리고 c</text>"))
        assertEquals("a < b 그리고 c", node.value)
    }

    @Test
    fun `raw text with truncated close tag and unicode does not crash`() {
        // Go-side fuzzer regression: "</Code" without '>' + case-fold length hazard.
        // Whole-string lowercasing skews indexes when Unicode case mapping changes
        // length ('İ' → "i̇") — the close-tag search must fold ASCII manually.
        val truncated = assertIs<CodeNode>(parseUi("<Code>İstanbul x</Code"))
        assertEquals("İstanbul x", truncated.code)
        val eof = assertIs<MarkdownNode>(parseUi("<markdown>İİİİ 열린 채 끝"))
        assertEquals("İİİİ 열린 채 끝", eof.value)
    }

    @Test
    fun `hasInteractiveNode distinguishes display trees from forms`() {
        val display = parseUi("<column><card><text>본문</text><badge>D-2</badge></card></column>")
        assertEquals(false, display.hasInteractiveNode())
        val form = parseUi("""<column><card><input id="n" label="이름"/></card></column>""")
        assertEquals(true, form.hasInteractiveNode())
        val nested = parseUi("""<tabs><tab label="a"><button event="e">전송</button></tab></tabs>""")
        assertEquals(true, nested.hasInteractiveNode())
    }
}
