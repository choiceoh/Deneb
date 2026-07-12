package ai.deneb.ui.markdown

import ai.deneb.ui.dynamicui.AlertNode
import ai.deneb.ui.dynamicui.ColumnNode
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class DenebUiBlockIntegrationTest {

    @Test
    fun `deneb-ui fence produces DenebUiBlock`() {
        val md = """
            ```deneb-ui
            {"type":"alert","title":"Heads up","message":"Hello"}
            ```
        """.trimIndent()
        val block = parseMarkdown(md).blocks.single()
        assertTrue(block is DenebUiBlock)
        val alert = block.node as AlertNode
        assertEquals("Heads up", alert.title)
        assertEquals("Hello", alert.message)
    }

    @Test
    fun `malformed deneb-ui fence produces DenebUiError`() {
        val md = """
            ```deneb-ui
            not json at all
            ```
        """.trimIndent()
        val block = parseMarkdown(md).blocks.single()
        assertTrue(block is DenebUiError)
    }

    @Test
    fun `ndjson multi-line deneb-ui wraps children in a column`() {
        val md = """
            ```deneb-ui
            {"type":"text","value":"a"}
            {"type":"text","value":"b"}
            ```
        """.trimIndent()
        val block = parseMarkdown(md).blocks.single()
        assertTrue(block is DenebUiBlock)
        val col = block.node as ColumnNode
        assertEquals(2, col.children.size)
    }

    @Test
    fun `deneb-ui block surrounded by markdown produces three blocks`() {
        val md = """
            Before

            ```deneb-ui
            {"type":"alert","message":"hi"}
            ```

            After
        """.trimIndent()
        val blocks = parseMarkdown(md).blocks
        assertEquals(3, blocks.size)
        assertTrue(blocks[0] is Paragraph)
        assertTrue(blocks[1] is DenebUiBlock)
        assertTrue(blocks[2] is Paragraph)
    }

    @Test
    fun `opener glued to a prose tail still opens the fence and keeps the prose`() {
        // Models sometimes emit the fence without a newline after the sentence
        // ("…가져올게요.```deneb-ui") — the card must render, not dump raw HTML.
        val md = "소스들을 다시 가져올게요.```deneb-ui\n{\"type\":\"alert\",\"message\":\"hi\"}\n```\n끝."
        val blocks = parseMarkdown(md).blocks
        assertEquals(3, blocks.size)
        assertTrue(blocks[0] is Paragraph)
        assertTrue(blocks[1] is DenebUiBlock)
        assertTrue(blocks[2] is Paragraph)
    }

    @Test
    fun `closer glued to an HTML body tail closes the fence and keeps trailing prose`() {
        // HTML bodies escape backticks (&#96;), so a raw ``` run can only close.
        val md = "카드.```deneb-ui\n<text>hi</text>```\n뒤 프로즈는 카드 밖."
        val blocks = parseMarkdown(md).blocks
        assertEquals(3, blocks.size)
        assertTrue(blocks[0] is Paragraph)
        assertTrue(blocks[1] is DenebUiBlock)
        assertTrue(blocks[2] is Paragraph)
    }

    @Test
    fun `one-liner fence renders prose on both sides`() {
        val blocks = parseMarkdown("프로즈.```deneb-ui<text>hi</text>``` 끝.").blocks
        assertEquals(3, blocks.size)
        assertTrue(blocks[0] is Paragraph)
        assertTrue(blocks[1] is DenebUiBlock)
        assertTrue(blocks[2] is Paragraph)
    }

    @Test
    fun `start-of-line one-liner fence is a card, not a code fence`() {
        // Also matches FENCE_REGEX with the tag glued into the info string —
        // the glued-opener split must win over the plain fence match.
        val block = parseMarkdown("```deneb-ui<text>hi</text>```").blocks.single()
        assertTrue(block is DenebUiBlock)
    }

    @Test
    fun `legacy JSON body keeps the strict own-line close`() {
        // JSON string values may legitimately contain ``` (markdown values).
        val md = "```deneb-ui\n{\"type\":\"markdown\",\"value\":\"a```b\"}\n```"
        val block = parseMarkdown(md).blocks.single()
        assertTrue(block is DenebUiBlock)
    }

    @Test
    fun `mid-sentence fence mention stays prose`() {
        val blocks = parseMarkdown("```deneb-ui 펜스는 이렇게 씁니다").blocks
        assertTrue(blocks.none { it is DenebUiBlock || it is DenebUiError || it is DenebUiPending })
    }

    @Test
    fun `split-block pattern with json fence is treated as deneb-ui`() {
        val md = """
            deneb-ui
            ```json
            {"type":"alert","message":"hi"}
            ```
        """.trimIndent()
        val block = parseMarkdown(md).blocks.single()
        assertTrue(block is DenebUiBlock)
    }

    @Test
    fun `deneb-ui block speakable text walks the node tree`() {
        val md = """
            Intro.

            ```deneb-ui
            {"type":"alert","title":"Heads up","message":"Take care"}
            ```

            Outro.
        """.trimIndent()
        val spoken = parseMarkdown(md).toSpeakableText()
        assertTrue(spoken.contains("Intro"))
        assertTrue(spoken.contains("Heads up"))
        assertTrue(spoken.contains("Take care"))
        assertTrue(spoken.contains("Outro"))
    }
}
