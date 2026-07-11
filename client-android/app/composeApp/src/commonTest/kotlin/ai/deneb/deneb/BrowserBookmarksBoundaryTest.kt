package ai.deneb.deneb

import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserBookmarksBoundaryTest {

    private fun bookmark(
        url: String,
        title: String = "Title",
        at: Long = 1,
    ) = BrowserBookmark(url = url, title = title, addedAtMs = at)

    @Test
    fun acceptsHttpAndHttpsSchemesCaseInsensitively() {
        for (url in listOf("http://example.com", "https://example.com", "HTTP://example.com", "HtTpS://example.com")) {
            assertTrue(canBookmarkUrl(url), url)
        }
    }

    @Test
    fun trimsOuterWhitespaceBeforeValidationAndStorage() {
        val result = toggleBrowserBookmark(emptyList(), "  https://example.com/path  ", "Title", nowMs = 5)

        assertEquals("https://example.com/path", result.single().url)
        assertTrue(canBookmarkUrl("  https://example.com/path  "))
    }

    @Test
    fun rejectsNonWebSchemesAndRelativeUrls() {
        for (url in listOf("", "/relative", "example.com", "ftp://example.com", "mailto:a@example.com", "javascript:alert(1)", "about:blank")) {
            assertFalse(canBookmarkUrl(url), url)
        }
    }

    @Test
    fun rejectsSchemeWithoutAuthority() {
        for (url in listOf("http://", "https://", "https:///path", "https://?q=x", "https://#fragment")) {
            assertFalse(canBookmarkUrl(url), url)
        }
    }

    @Test
    fun rejectsWhitespaceAndControlCharactersAnywhereInUrl() {
        for (url in listOf(
            "https://exa mple.com",
            "https://example.com/a path",
            "https://example.com\njavascript:alert(1)",
            "https://example.com\rheader",
            "https://example.com\tpath",
            "https://example.com/\u0000",
        )) {
            assertFalse(canBookmarkUrl(url), url)
        }
    }

    @Test
    fun acceptsPortsCredentialsQueriesFragmentsAndUnicodePaths() {
        for (url in listOf(
            "https://localhost:8443/path",
            "http://user:pass@example.com/private",
            "https://example.com?q=one",
            "https://example.com#section",
            "https://example.com/한글/경로",
        )) {
            assertTrue(canBookmarkUrl(url), url)
        }
    }

    @Test
    fun invalidToggleDoesNotAddRowButSanitizesExistingList() {
        val existing = listOf(
            bookmark("mailto:a@example.com"),
            bookmark(" https://valid.example/path ", title = " Valid "),
        )

        val result = toggleBrowserBookmark(existing, "javascript:bad", "Bad", nowMs = 9)

        assertEquals(listOf("https://valid.example/path"), result.map { it.url })
        assertEquals("Valid", result.single().title)
    }

    @Test
    fun addingBookmarkPrependsItAndPreservesTimestamp() {
        val existing = listOf(bookmark("https://old.example", at = 1))

        val result = toggleBrowserBookmark(existing, "https://new.example", "New", nowMs = Long.MAX_VALUE)

        assertEquals(listOf("https://new.example", "https://old.example"), result.map { it.url })
        assertEquals(Long.MAX_VALUE, result.first().addedAtMs)
    }

    @Test
    fun togglingExistingCanonicalUrlRemovesEveryDuplicate() {
        val existing = listOf(
            bookmark("https://example.com", title = "one"),
            bookmark(" https://example.com ", title = "two"),
            bookmark("https://keep.example"),
        )

        val result = toggleBrowserBookmark(existing, " https://example.com ", "ignored", nowMs = 9)

        assertEquals(listOf("https://keep.example"), result.map { it.url })
    }

    @Test
    fun togglingExistingUrlDoesNotReplaceItsTitle() {
        val existing = listOf(bookmark("https://example.com", title = "Original"))

        val removed = toggleBrowserBookmark(existing, "https://example.com", "Replacement", nowMs = 2)

        assertEquals(emptyList(), removed)
    }

    @Test
    fun removeUsesTrimmedCanonicalKey() {
        val rows = listOf(bookmark("https://a.example"), bookmark("https://b.example"))

        assertEquals(listOf("https://b.example"), removeBrowserBookmark(rows, "  https://a.example  ").map { it.url })
    }

    @Test
    fun removingMissingUrlStillReturnsSanitizedCopy() {
        val rows = listOf(bookmark(" https://a.example ", title = "  A   title "))

        val result = removeBrowserBookmark(rows, "https://missing.example")

        assertEquals(listOf(bookmark("https://a.example", title = "A title")), result)
    }

    @Test
    fun bookmarkLookupUsesCanonicalUrlAndRejectsInvalidTarget() {
        val rows = listOf(bookmark(" https://example.com "))

        assertTrue(isBrowserBookmarked(rows, "https://example.com"))
        assertTrue(isBrowserBookmarked(rows, " https://example.com "))
        assertFalse(isBrowserBookmarked(rows, "javascript:bad"))
    }

    @Test
    fun urlPathQueryAndFragmentRemainDistinctBookmarkKeys() {
        val rows = listOf(
            bookmark("https://example.com/a"),
            bookmark("https://example.com/a?q=1"),
            bookmark("https://example.com/a#part"),
        )

        assertTrue(isBrowserBookmarked(rows, "https://example.com/a"))
        assertTrue(isBrowserBookmarked(rows, "https://example.com/a?q=1"))
        assertTrue(isBrowserBookmarked(rows, "https://example.com/a#part"))
        assertEquals(3, decodeBrowserBookmarks(encodeBrowserBookmarks(rows)).size)
    }

    @Test
    fun sanitizationKeepsFirstDuplicateAndOriginalOrder() {
        val rows = listOf(
            bookmark("https://a.example", title = "first"),
            bookmark("https://b.example", title = "second"),
            bookmark(" https://a.example ", title = "duplicate"),
            bookmark("https://c.example", title = "third"),
        )

        val decoded = decodeBrowserBookmarks(encodeBrowserBookmarks(rows))

        assertEquals(listOf("first", "second", "third"), decoded.map { browserBookmarkDisplayTitle(it) })
        assertEquals("first", decoded.first().title)
    }

    @Test
    fun encoderDropsInvalidRowsBeforeWriting() {
        val encoded = encodeBrowserBookmarks(
            listOf(bookmark("javascript:bad"), bookmark("https://valid.example")),
        )

        assertFalse("javascript" in encoded)
        assertTrue("https://valid.example" in encoded)
    }

    @Test
    fun decoderRejectsMalformedNonArrayAndWrongElementShapes() {
        for (raw in listOf("", "null", "{}", "42", "\"text\"", "[1]", "[{\"url\":{}}]")) {
            assertEquals(emptyList(), decodeBrowserBookmarks(raw), raw)
        }
    }

    @Test
    fun decoderIgnoresUnknownFields() {
        val raw = """[{"url":"https://example.com","title":"Title","addedAtMs":7,"future":{"x":true}}]"""

        assertEquals(listOf(bookmark("https://example.com", at = 7)), decodeBrowserBookmarks(raw))
    }

    @Test
    fun decoderAppliesDefaultsForMissingOptionalFields() {
        val decoded = decodeBrowserBookmarks("""[{"url":"https://example.com"}]""").single()

        assertEquals("example.com", decoded.title)
        assertEquals(0L, decoded.addedAtMs)
    }

    @Test
    fun decoderDropsExplicitNullRowsRatherThanPartiallyTrustingThem() {
        val raw = """[{"url":null,"title":"bad"},{"url":"https://good.example"}]"""

        assertEquals(emptyList(), decodeBrowserBookmarks(raw))
    }

    @Test
    fun titleWhitespaceIsTrimmedAndCollapsed() {
        val result = toggleBrowserBookmark(emptyList(), "https://example.com", "  Alpha\n\t Beta   Gamma  ", nowMs = 1)

        assertEquals("Alpha Beta Gamma", result.single().title)
    }

    @Test
    fun blankTitleFallsBackToHostWithoutWwwPrefix() {
        val lower = toggleBrowserBookmark(emptyList(), "https://www.example.com/path", "   ", nowMs = 1).single()
        val upper = toggleBrowserBookmark(emptyList(), "https://WWW.EXAMPLE.COM/path", "", nowMs = 1).single()

        assertEquals("example.com", lower.title)
        assertEquals("EXAMPLE.COM", upper.title)
    }

    @Test
    fun displayTitlePrefersStoredNonBlankTitle() {
        val row = bookmark("https://example.com", title = "Friendly")

        assertEquals("Friendly", browserBookmarkDisplayTitle(row))
    }

    @Test
    fun displayTitleFallsBackToHostForBlankStoredTitle() {
        val row = bookmark("https://www.example.com/path", title = "")

        assertEquals("example.com", browserBookmarkDisplayTitle(row))
    }

    @Test
    fun titleIsCappedAtNinetySixCharacters() {
        val result = toggleBrowserBookmark(emptyList(), "https://example.com", "x".repeat(200), nowMs = 1)

        assertEquals(96, result.single().title.length)
    }

    @Test
    fun titleLimitNeverLeavesDanglingHighSurrogate() {
        val title = "x".repeat(95) + "🚀" + "tail"

        val stored = toggleBrowserBookmark(emptyList(), "https://example.com", title, nowMs = 1).single().title

        assertEquals(95, stored.length)
        assertFalse(stored.last().isHighSurrogate())
        assertEquals("x".repeat(95), stored)
    }

    @Test
    fun encoderCapsCollectionAtEightyNewestInputRows() {
        val rows = (0 until 100).map { index -> bookmark("https://e$index.example", at = index.toLong()) }

        val decoded = decodeBrowserBookmarks(encodeBrowserBookmarks(rows))

        assertEquals(80, decoded.size)
        assertEquals("https://e0.example", decoded.first().url)
        assertEquals("https://e79.example", decoded.last().url)
    }

    @Test
    fun decoderCapsMaliciousOversizedPersistedArrayAtEightyRows() {
        val raw = Json.encodeToString(
            (0 until 150).map { index -> bookmark("https://e$index.example") },
        )

        val decoded = decodeBrowserBookmarks(raw)

        assertEquals(80, decoded.size)
        assertEquals("https://e79.example", decoded.last().url)
    }

    @Test
    fun toggleAtCapacityPrependsNewAndEvictsLast() {
        val rows = (0 until 80).map { index -> bookmark("https://e$index.example") }

        val result = toggleBrowserBookmark(rows, "https://new.example", "New", nowMs = 9)

        assertEquals(80, result.size)
        assertEquals("https://new.example", result.first().url)
        assertFalse(result.any { it.url == "https://e79.example" })
    }

    @Test
    fun sanitizationDoesNotMutateCallerOwnedListOrRows() {
        val row = bookmark(" https://example.com ", title = "  Title  ")
        val rows = mutableListOf(row)

        val encoded = encodeBrowserBookmarks(rows)

        assertEquals(listOf(row), rows)
        assertEquals(" https://example.com ", row.url)
        assertTrue("https://example.com" in encoded)
    }

    @Test
    fun invalidToggleReturnsSanitizedListNotOriginalIdentity() {
        val rows = listOf(bookmark("https://example.com"))

        val result = toggleBrowserBookmark(rows, "bad", "bad", nowMs = 1)

        assertEquals(rows, result)
        assertFalse(result === rows)
    }

    @Test
    fun decodeEncodeRoundTripPreservesExtremeTimestamps() {
        val rows = listOf(
            bookmark("https://min.example", at = Long.MIN_VALUE),
            bookmark("https://max.example", at = Long.MAX_VALUE),
        )

        assertEquals(rows, decodeBrowserBookmarks(encodeBrowserBookmarks(rows)))
    }
}
