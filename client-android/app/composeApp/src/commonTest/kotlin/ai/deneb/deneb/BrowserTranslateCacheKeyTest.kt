package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals

/**
 * The cache key decides how often the phone asks the gateway for a translation it
 * could have answered itself. Two properties matter and pull in opposite
 * directions: context must NOT split a sentence into separate entries, and part
 * boundaries MUST, because a grouped reply is matched by length.
 */
class BrowserTranslateCacheKeyTest {
    private val sentinel = "\uE000"
    private val prefix = "deneb_translate_segment:v1:"

    private fun envelope(body: String, withSentinel: Boolean = true) = (if (withSentinel) sentinel else "") + prefix + body

    @Test
    fun sameSentenceInDifferentBlocksSharesOneEntry() {
        // This is the whole point: the gateway drops context from its own key, so a
        // client miss here buys a round trip that returns the identical string.
        val a = browserTranslateCacheKey("ko", envelope("""{"text":"Hello there","context":"A B C","role":"body"}"""))
        val b = browserTranslateCacheKey("ko", envelope("""{"text":"Hello there","context":"X Y Z","role":"chrome"}"""))
        assertEquals(a, b)
    }

    @Test
    fun theSentinelIsOptional() {
        // Old APKs in the field send it; the parser must not care.
        val withIt = browserTranslateCacheKey("ko", envelope("""{"text":"Hello"}""", withSentinel = true))
        val without = browserTranslateCacheKey("ko", envelope("""{"text":"Hello"}""", withSentinel = false))
        assertEquals(withIt, without)
    }

    @Test
    fun differentPartBoundariesAreDifferentEntries() {
        // Same joined text, different split. Serving one for the other hands the page
        // worker an array of the wrong length, which fails the entire unit.
        val two = browserTranslateCacheKey("ko", envelope("""{"parts":["Hello ","there"]}"""))
        val three = browserTranslateCacheKey("ko", envelope("""{"parts":["Hel","lo ","there"]}"""))
        assertNotEquals(two, three)
    }

    @Test
    fun partsAndPlainTextOfTheSameSentenceDoNotCollide() {
        val parts = browserTranslateCacheKey("ko", envelope("""{"parts":["Hello ","there"]}"""))
        val text = browserTranslateCacheKey("ko", envelope("""{"text":"Hello there"}"""))
        assertNotEquals(parts, text)
    }

    @Test
    fun targetLanguageStaysInTheKey() {
        val ko = browserTranslateCacheKey("ko", envelope("""{"text":"Hello"}"""))
        val ja = browserTranslateCacheKey("ja", envelope("""{"text":"Hello"}"""))
        assertNotEquals(ko, ja)
    }

    @Test
    fun plainSegmentsPassThroughUnchanged() {
        assertEquals("ko Hello there", browserTranslateCacheKey("ko", "Hello there"))
    }

    @Test
    fun anUnreadableEnvelopeIsKeyedVerbatim() {
        // Falling back to a truncated key would let two different segments collide;
        // keying the raw payload just costs one extra entry.
        val broken = envelope("{not json")
        assertEquals("ko $broken", browserTranslateCacheKey("ko", broken))

        val empty = envelope("""{"context":"only context"}""")
        assertEquals("ko $empty", browserTranslateCacheKey("ko", empty))
    }

    @Test
    fun aTextThatLooksLikeAnEnvelopeIsNotMisread() {
        // A page can literally contain the ASCII prefix; without the sentinel it is
        // prose, and prose must not be parsed as protocol.
        val prose = prefix + "this is page text, not an envelope"
        assertEquals("ko $prose", browserTranslateCacheKey("ko", prose))
    }
}
