package ai.deneb.tools

import kotlin.test.Test
import kotlin.test.assertEquals

class HtmlUtilsTest {

    @Test
    fun decodesEverySupportedNamedAndNumericEntity() {
        val encoded = "a&nbsp;b &amp; c &lt;tag&gt; &quot;quoted&quot; &#39;single&#39;"

        assertEquals("a b & c <tag> \"quoted\" 'single'", encoded.decodeHtmlEntities())
    }

    @Test
    fun repeatedEntitiesDecodeAtEveryOccurrence() {
        assertEquals("&&&", "&amp;&amp;&amp;".decodeHtmlEntities())
        assertEquals("   ", "&nbsp;&nbsp;&nbsp;".decodeHtmlEntities())
        assertEquals("<<>>", "&lt;&lt;&gt;&gt;".decodeHtmlEntities())
    }

    @Test
    fun nestedEscapesDecodeExactlyOneHtmlLayer() {
        assertEquals("&lt;script&gt;", "&amp;lt;script&amp;gt;".decodeHtmlEntities())
        assertEquals("&amp;", "&amp;amp;".decodeHtmlEntities())
        assertEquals("&nbsp;", "&amp;nbsp;".decodeHtmlEntities())
    }

    @Test
    fun unknownAndDifferentlyCasedEntitiesStayVerbatim() {
        val source = "&copy; &apos; &#34; &AMP; &NbSp; &unknown;"

        assertEquals(source, source.decodeHtmlEntities())
    }

    @Test
    fun incompleteEntityFragmentsStayVerbatim() {
        for (source in listOf("&", "&amp", "amp;", "&&amp", "&lt tag")) {
            assertEquals(source, source.decodeHtmlEntities(), source)
        }
        assertEquals("&lt tag>", "&lt tag&gt;".decodeHtmlEntities())
    }

    @Test
    fun plainEmptyAndUnicodeTextAreUnchanged() {
        for (source in listOf("", "plain text", "한글과 emoji 🚀", "1 < 2 && 3 > 2")) {
            assertEquals(source, source.decodeHtmlEntities(), source)
        }
    }
}
