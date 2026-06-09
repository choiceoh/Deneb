package ai.deneb.ui.markdown

import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class InlineParsingTest {

    private fun inlines(text: String): List<InlineNode> {
        val para = parseMarkdown(text).blocks.single() as Paragraph
        return para.inlines
    }

    @Test
    fun `plain text is a single Text node`() {
        assertEquals(listOf(Text("hello world")), inlines("hello world"))
    }

    @Test
    fun `strong with double asterisks`() {
        assertEquals(listOf(Strong(persistentListOf(Text("bold")))), inlines("**bold**"))
    }

    @Test
    fun `strong with double underscores`() {
        assertEquals(listOf(Strong(persistentListOf(Text("bold")))), inlines("__bold__"))
    }

    @Test
    fun `emphasis with single asterisk`() {
        assertEquals(listOf(Emphasis(persistentListOf(Text("italic")))), inlines("*italic*"))
    }

    @Test
    fun `emphasis with single underscore`() {
        assertEquals(listOf(Emphasis(persistentListOf(Text("italic")))), inlines("_italic_"))
    }

    @Test
    fun `strike with tildes`() {
        assertEquals(listOf(Strike(persistentListOf(Text("gone")))), inlines("~~gone~~"))
    }

    @Test
    fun `inline code`() {
        assertEquals(listOf(InlineCode("x = 1")), inlines("`x = 1`"))
    }

    @Test
    fun `link produces Link with href and children`() {
        val result = inlines("[click here](https://example.com)")
        val link = result.single() as Link
        assertEquals("https://example.com", link.href)
        assertEquals(persistentListOf(Text("click here")), link.children)
    }

    @Test
    fun `image produces Image with src and alt`() {
        val result = inlines("![photo](https://example.com/x.png)")
        val image = result.single() as Image
        assertEquals("https://example.com/x.png", image.src)
        assertEquals("photo", image.alt)
    }

    @Test
    fun `intraword underscores do not form emphasis`() {
        assertEquals(listOf(Text("foo_bar_baz")), inlines("foo_bar_baz"))
    }

    @Test
    fun `intraword asterisks do form emphasis`() {
        // Asterisks are permissive — matches CommonMark behavior.
        val result = inlines("foo*bar*baz")
        assertTrue(result.any { it is Emphasis })
    }

    @Test
    fun `backslash escape of asterisk`() {
        assertEquals(listOf(Text("*literal*")), inlines("\\*literal\\*"))
    }

    @Test
    fun `backslash escape of backtick`() {
        assertEquals(listOf(Text("`not code`")), inlines("\\`not code\\`"))
    }

    @Test
    fun `mixed bold and italic`() {
        val result = inlines("**bold** and *italic*")
        assertEquals(3, result.size)
        assertTrue(result[0] is Strong)
        assertTrue(result[1] is Text)
        assertTrue(result[2] is Emphasis)
    }

    @Test
    fun `nested emphasis inside strong`() {
        val result = inlines("**bold _and italic_ text**")
        val strong = result.single() as Strong
        assertTrue(strong.children.any { it is Emphasis })
    }

    @Test
    fun `hard line break from trailing double space`() {
        val result = inlines("line one  \nline two")
        assertTrue(result.any { it is LineBreak })
    }

    @Test
    fun `unclosed emphasis degrades to literal`() {
        val result = inlines("*unclosed text")
        assertEquals(listOf(Text("*unclosed text")), result)
    }

    @Test
    fun `unclosed strong degrades to literal`() {
        val result = inlines("**unclosed")
        assertEquals(listOf(Text("**unclosed")), result)
    }

    @Test
    fun `inline code with special chars inside`() {
        assertEquals(listOf(InlineCode("a*b*c")), inlines("`a*b*c`"))
    }

    @Test
    fun `strong span wraps inline code`() {
        val result = inlines("**before `code` after**")
        val strong = result.single() as Strong
        assertEquals(Text("before "), strong.children[0])
        assertEquals(InlineCode("code"), strong.children[1])
        assertEquals(Text(" after"), strong.children[2])
    }

    @Test
    fun `strong span wraps multiple inline code spans`() {
        val result = inlines("**a `b` c `d` e**")
        val strong = result.single() as Strong
        assertEquals(
            listOf(Text("a "), InlineCode("b"), Text(" c "), InlineCode("d"), Text(" e")),
            strong.children,
        )
    }

    @Test
    fun `emphasis asterisks inside inline code are not delimiters`() {
        // The bold pair bridges the code spans; `*` inside code is ignored.
        val result = inlines("**x `*not*` y**")
        val strong = result.single() as Strong
        assertEquals(
            listOf(Text("x "), InlineCode("*not*"), Text(" y")),
            strong.children,
        )
    }

    @Test
    fun `pathological backslash run does not hang the parser`() {
        // Regression: the previous LINK_REGEX inner group `(?:\\.|[^\[\]])*` allowed `\X` to
        // match either as one `\\.` or as two `[^…]` iterations. On Android's ICU regex,
        // this produced exponential backtracking when the input had many `\X` pairs and no
        // closing `](url)`. The test runner's timeout catches a hang.
        val pathological = "[start " + "\\X".repeat(60) + " end]not-a-paren"
        val result = parseMarkdown(pathological)
        assertTrue(result.blocks.isNotEmpty())
    }

    @Test
    fun `bare url becomes an autolink`() {
        val link = inlines("자세히는 https://github.com/deneb 참고").filterIsInstance<Link>().single()
        assertEquals("https://github.com/deneb", link.href)
        assertEquals(persistentListOf(Text("https://github.com/deneb")), link.children)
    }

    @Test
    fun `bare url trailing punctuation stays out of the link`() {
        val link = inlines("(see https://example.com).").filterIsInstance<Link>().single()
        assertEquals("https://example.com", link.href)
    }

    @Test
    fun `www url gains an https scheme`() {
        val link = inlines("www.example.com 방문").filterIsInstance<Link>().single()
        assertEquals("https://www.example.com", link.href)
    }

    @Test
    fun `markdown link is not double-linked by autolink`() {
        val link = inlines("[here](https://example.com)").single() as Link
        assertEquals("https://example.com", link.href)
        assertEquals(persistentListOf(Text("here")), link.children)
    }

    @Test
    fun `space-flanked asterisks are not emphasis`() {
        // "단가는 3 * 4 * 5" — multiplication must not turn "* 4 *" into italic.
        assertEquals(listOf(Text("3 * 4 * 5")), inlines("3 * 4 * 5"))
    }

    @Test
    fun `emphasis still works without flanking spaces`() {
        assertEquals(listOf(Emphasis(persistentListOf(Text("italic")))), inlines("*italic*"))
        assertTrue(inlines("a *b* c").any { it is Emphasis })
    }

    @Test
    fun `html br becomes a line break`() {
        assertTrue(inlines("line one<br>line two").any { it is LineBreak })
        assertTrue(inlines("a<br/>b").any { it is LineBreak })
    }

    @Test
    fun `triple asterisk is bold italic`() {
        val em = inlines("***둘 다***").single() as Emphasis
        val strong = em.children.single() as Strong
        assertEquals(persistentListOf(Text("둘 다")), strong.children)
    }

    @Test
    fun `triple underscore is bold italic`() {
        val result = inlines("___both___").single()
        // Underscore nesting already resolved via __…__ then _…_; either order is fine
        // as long as both styles survive.
        assertTrue(result is Strong || result is Emphasis)
    }

    @Test
    fun `angle bracket url autolink hides the brackets`() {
        val link = inlines("자세히는 <https://example.com/x> 참고").filterIsInstance<Link>().single()
        assertEquals("https://example.com/x", link.href)
        assertEquals(persistentListOf(Text("https://example.com/x")), link.children)
    }

    @Test
    fun `bare email becomes a mailto link`() {
        val link = inlines("문의는 kim@topsolar.kr 으로").filterIsInstance<Link>().single()
        assertEquals("mailto:kim@topsolar.kr", link.href)
        assertEquals(persistentListOf(Text("kim@topsolar.kr")), link.children)
    }

    @Test
    fun `angle bracket email becomes a mailto link without brackets`() {
        val link = inlines("회신: <lee@example.co.kr>").filterIsInstance<Link>().single()
        assertEquals("mailto:lee@example.co.kr", link.href)
        assertEquals(persistentListOf(Text("lee@example.co.kr")), link.children)
    }

    @Test
    fun `package version is not an email`() {
        assertTrue(inlines("node@18.0.0 으로 빌드").none { it is Link })
    }

    @Test
    fun `email inside a markdown link is not double-linked`() {
        val link = inlines("[메일 보내기](mailto:kim@topsolar.kr)").single() as Link
        assertEquals("mailto:kim@topsolar.kr", link.href)
        assertEquals(persistentListOf(Text("메일 보내기")), link.children)
    }

    @Test
    fun `named html entities decode in text`() {
        assertEquals(listOf(Text("AT&T <tag> 5\u00A0kg")), inlines("AT&amp;T &lt;tag&gt; 5&nbsp;kg"))
    }

    @Test
    fun `numeric html entities decode in text`() {
        assertEquals(listOf(Text("A 😀")), inlines("&#65; &#x1F600;"))
    }

    @Test
    fun `unknown entity stays literal`() {
        assertEquals(listOf(Text("&notanentity; 그대로")), inlines("&notanentity; 그대로"))
    }

    @Test
    fun `bare ampersand stays literal`() {
        assertEquals(listOf(Text("R&D 부서 & 영업")), inlines("R&D 부서 & 영업"))
    }

    @Test
    fun `entities inside inline code stay raw`() {
        assertEquals(listOf(InlineCode("a &amp; b")), inlines("`a &amp; b`"))
    }

    @Test
    fun `link title is stripped from href`() {
        val link = inlines("[참고](https://example.com \"공식 문서\")").single() as Link
        assertEquals("https://example.com", link.href)
    }

    @Test
    fun `link target in angle brackets is unwrapped`() {
        val link = inlines("[참고](<https://example.com/a b>)").single() as Link
        assertEquals("https://example.com/a b", link.href)
    }

    @Test
    fun `html bold and italic tags map to style nodes`() {
        assertEquals(listOf(Strong(persistentListOf(Text("굵게")))), inlines("<b>굵게</b>"))
        assertEquals(listOf(Strong(persistentListOf(Text("강조")))), inlines("<strong>강조</strong>"))
        assertEquals(listOf(Emphasis(persistentListOf(Text("기울임")))), inlines("<em>기울임</em>"))
    }

    @Test
    fun `html del tag maps to strike`() {
        assertEquals(listOf(Strike(persistentListOf(Text("지움")))), inlines("<del>지움</del>"))
    }

    @Test
    fun `html sub and sup convert to unicode scripts`() {
        assertEquals(listOf(Text("H₂O와 m²")), inlines("H<sub>2</sub>O와 m<sup>2</sup>"))
    }

    @Test
    fun `html sup with unmappable chars keeps plain content`() {
        assertEquals(listOf(Text("5th")), inlines("5<sup>th</sup>"))
    }

    @Test
    fun `html mark and u are stripped to their content`() {
        assertEquals(listOf(Text("표시 밑줄")), inlines("<mark>표시</mark> <u>밑줄</u>"))
    }

    @Test
    fun `html code tag becomes inline code`() {
        assertEquals(listOf(InlineCode("a < b")), inlines("<code>a < b</code>"))
    }

    @Test
    fun `html bold tag parses nested markdown`() {
        val strong = inlines("<b>a *i*</b>").single() as Strong
        assertTrue(strong.children.any { it is Emphasis })
    }

    @Test
    fun `unpaired html tag stays literal`() {
        assertEquals(listOf(Text("List<b> 타입")), inlines("List<b> 타입"))
    }

    @Test
    fun `html tag inside inline code stays raw`() {
        assertEquals(listOf(InlineCode("<b>x</b>")), inlines("`<b>x</b>`"))
    }
}
