package ai.deneb.ui.markdown

import kotlinx.collections.immutable.persistentListOf
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertIs
import kotlin.test.assertSame
import kotlin.test.assertTrue

class MarkdownParseCacheTest {

    private fun document(label: String): MarkdownDocument = MarkdownDocument(
        persistentListOf(
            Paragraph(persistentListOf(Text(label))),
        ),
    )

    private fun fillCache(prefix: String): List<String> = (0 until 128).map { index ->
        "$prefix/$index"
    }.also { keys ->
        keys.forEach { key -> putMarkdownParsed(key, document(key)) }
    }

    @Test
    fun parseMissStoresDocumentForLaterReads() {
        val key = "cache-test/miss/# heading"
        assertFalse(isMarkdownCached(key))

        val parsed = parseMarkdownCached(key)

        assertTrue(isMarkdownCached(key))
        assertSame(parsed, parseMarkdownCached(key))
        assertIs<Paragraph>(parsed.blocks.single())
    }

    @Test
    fun equalTextUsesTheSameCachedDocumentInstance() {
        val firstInput = buildString { append("cache-test/equal/**bold**") }
        val secondInput = buildString { append("cache-test/equal/**bold**") }

        val first = parseMarkdownCached(firstInput)
        val second = parseMarkdownCached(secondInput)

        assertEquals(firstInput, secondInput)
        assertSame(first, second)
    }

    @Test
    fun distinctTextKeepsIndependentDocuments() {
        val firstKey = "cache-test/distinct/first"
        val secondKey = "cache-test/distinct/second"

        val first = parseMarkdownCached(firstKey)
        val second = parseMarkdownCached(secondKey)

        assertTrue(first !== second)
        assertSame(first, parseMarkdownCached(firstKey))
        assertSame(second, parseMarkdownCached(secondKey))
    }

    @Test
    fun putMakesBackgroundParsedDocumentImmediatelyVisible() {
        val key = "cache-test/put/fresh"
        val supplied = document("already parsed")
        assertFalse(isMarkdownCached(key))

        putMarkdownParsed(key, supplied)

        assertTrue(isMarkdownCached(key))
        assertSame(supplied, parseMarkdownCached(key))
    }

    @Test
    fun putDoesNotOverwriteAnExistingDocument() {
        val key = "cache-test/put/preserve"
        val original = document("original")
        val replacement = document("replacement")
        putMarkdownParsed(key, original)

        putMarkdownParsed(key, replacement)

        assertSame(original, parseMarkdownCached(key))
        assertTrue(parseMarkdownCached(key) !== replacement)
    }

    @Test
    fun cacheEvictsTheOldestEntryAtItsBound() {
        val keys = fillCache("cache-test/eviction/fifo")
        keys.forEach { key -> assertTrue(isMarkdownCached(key), key) }

        val newest = "cache-test/eviction/fifo/newest"
        putMarkdownParsed(newest, document(newest))

        assertFalse(isMarkdownCached(keys.first()))
        assertTrue(isMarkdownCached(keys[1]))
        assertTrue(isMarkdownCached(keys.last()))
        assertTrue(isMarkdownCached(newest))
    }

    @Test
    fun parseHitRefreshesAnEntryBeforeEviction() {
        val keys = fillCache("cache-test/eviction/hit-refresh")
        val firstDocument = parseMarkdownCached(keys.first())

        val newest = "cache-test/eviction/hit-refresh/newest"
        putMarkdownParsed(newest, document(newest))

        assertTrue(isMarkdownCached(keys.first()))
        assertSame(firstDocument, parseMarkdownCached(keys.first()))
        assertFalse(isMarkdownCached(keys[1]))
        assertTrue(isMarkdownCached(newest))
    }

    @Test
    fun membershipCheckDoesNotChangeRecency() {
        val keys = fillCache("cache-test/eviction/plain-check")

        assertTrue(isMarkdownCached(keys.first()))
        val newest = "cache-test/eviction/plain-check/newest"
        putMarkdownParsed(newest, document(newest))

        assertFalse(isMarkdownCached(keys.first()))
        assertTrue(isMarkdownCached(keys[1]))
        assertTrue(isMarkdownCached(newest))
    }

    @Test
    fun duplicatePutDoesNotChangeRecencyOrIdentity() {
        val keys = fillCache("cache-test/eviction/duplicate-put")
        putMarkdownParsed(keys.first(), document("replacement"))
        val newest = "cache-test/eviction/duplicate-put/newest"
        putMarkdownParsed(newest, document(newest))

        assertFalse(isMarkdownCached(keys.first()))
        assertTrue(isMarkdownCached(keys[1]))
        assertTrue(isMarkdownCached(newest))
    }

    @Test
    fun directParsingCachesBlankContentAsAStableDocument() {
        val blank = " ".repeat(73)
        assertFalse(isMarkdownCached(blank))

        val parsed = parseMarkdownCached(blank)

        assertTrue(isMarkdownCached(blank))
        assertTrue(parsed.blocks.isEmpty())
        assertSame(parsed, parseMarkdownCached(blank))
    }

    @Test
    fun precomputeSkipsBlankInputs() = runTest {
        val blank = " \t".repeat(97)
        assertFalse(isMarkdownCached(blank))

        precomputeMarkdownAsync(listOf("", blank, "\n\n"))

        assertFalse(isMarkdownCached(""))
        assertFalse(isMarkdownCached(blank))
        assertFalse(isMarkdownCached("\n\n"))
    }

    @Test
    fun precomputeWarmsDistinctMarkdownBodies() = runTest {
        val heading = "cache-test/precompute/# Heading"
        val list = "cache-test/precompute/list\n\n- one\n- two"
        val code = "cache-test/precompute/code\n\n```kotlin\nval n = 1\n```"

        precomputeMarkdownAsync(listOf(heading, list, heading, code, list))

        assertTrue(isMarkdownCached(heading))
        assertTrue(isMarkdownCached(list))
        assertTrue(isMarkdownCached(code))
        assertSame(parseMarkdownCached(heading), parseMarkdownCached(heading))
        assertEquals(2, parseMarkdownCached(list).blocks.size)
        assertIs<CodeFence>(parseMarkdownCached(code).blocks.last())
    }

    @Test
    fun precomputePreservesAnAlreadyCachedDocument() = runTest {
        val existingKey = "cache-test/precompute/existing"
        val newKey = "cache-test/precompute/new"
        val supplied = document("not derived from key")
        putMarkdownParsed(existingKey, supplied)

        precomputeMarkdownAsync(listOf(existingKey, newKey, existingKey))

        assertSame(supplied, parseMarkdownCached(existingKey))
        assertTrue(isMarkdownCached(newKey))
    }

    @Test
    fun precomputedDocumentIsReusedByForegroundParsing() = runTest {
        val key = "cache-test/precompute/reuse\n\n**bold** and `code`"

        precomputeMarkdownAsync(listOf(key))
        val firstForegroundRead = parseMarkdownCached(key)
        val secondForegroundRead = parseMarkdownCached(key)

        assertSame(firstForegroundRead, secondForegroundRead)
        val paragraph = assertIs<Paragraph>(firstForegroundRead.blocks.last())
        assertTrue(paragraph.inlines.any { it is Strong })
        assertTrue(paragraph.inlines.any { it is InlineCode })
    }

    @Test
    fun precomputeCanWarmMoreThanTheCacheBoundWithoutGrowingUnbounded() = runTest {
        val keys = (0 until 130).map { "cache-test/precompute/bound/$it" }

        precomputeMarkdownAsync(keys)

        assertFalse(isMarkdownCached(keys[0]))
        assertFalse(isMarkdownCached(keys[1]))
        keys.drop(2).forEach { key -> assertTrue(isMarkdownCached(key), key) }
    }
}
