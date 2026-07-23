package ai.deneb.deneb

import ai.deneb.data.Attachment
import ai.deneb.ui.chat.History
import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class DenebTranscriptCacheBoundaryTest {

    private val cacheOwner = "gateway#account"

    /** Most codec-boundary cases exercise the messages array; wrap those rows in
     *  the same owner envelope used by the settings-backed cache. */
    private fun decodeCachedTranscript(json: String): List<History>? {
        val payload = if (json.trimStart().startsWith("[")) {
            """{"owner":"$cacheOwner","messages":$json}"""
        } else {
            json
        }
        return ai.deneb.deneb.decodeCachedTranscript(payload, cacheOwner)
    }

    private fun encodeCachedTranscript(transcript: List<History>): String? = ai.deneb.deneb.encodeCachedTranscript(transcript, cacheOwner)

    private fun history(
        role: History.Role,
        content: String,
        timestamp: Long = 0L,
        id: String = "id-$timestamp-$content",
    ) = History(
        id = id,
        role = role,
        content = content,
        timestampMs = timestamp,
    )

    @Test
    fun malformedJsonReturnsNull() {
        for (payload in listOf("", "not-json", "{", "{}", "null", "42", "\"text\"")) {
            assertNull(decodeCachedTranscript(payload), payload)
        }
    }

    @Test
    fun ownerMismatchRejectsOtherwiseValidTranscript() {
        val encoded = encodeCachedTranscript(listOf(history(History.Role.USER, "private")))!!

        assertNull(ai.deneb.deneb.decodeCachedTranscript(encoded, "different-owner"))
    }

    @Test
    fun legacyUnscopedTranscriptIsRejected() {
        val legacy = """[{"role":"user","content":"private","ts":1}]"""

        assertNull(ai.deneb.deneb.decodeCachedTranscript(legacy, cacheOwner))
    }

    @Test
    fun emptyArrayIsTreatedAsNoCache() {
        assertNull(decodeCachedTranscript("[]"))
        assertNull(decodeCachedTranscript("  [ ]  "))
    }

    @Test
    fun unknownFieldsAreIgnoredForForwardCompatibility() {
        val decoded = decodeCachedTranscript(
            """[{"role":"user","content":"hello","ts":7,"future":{"nested":true}}]""",
        )

        assertEquals("hello", decoded?.single()?.content)
        assertEquals(7L, decoded?.single()?.timestampMs)
    }

    @Test
    fun missingRequiredRoleRejectsWholePayload() {
        assertNull(decodeCachedTranscript("""[{"content":"hello"}]"""))
    }

    @Test
    fun missingRequiredContentRejectsWholePayload() {
        assertNull(decodeCachedTranscript("""[{"role":"user"}]"""))
    }

    @Test
    fun oneMalformedRowRejectsTheWholeTranscript() {
        val payload = """[{"role":"user","content":"ok"},{"role":7,"content":"bad"}]"""

        assertNull(decodeCachedTranscript(payload))
    }

    @Test
    fun omittedTimestampDefaultsToZero() {
        val decoded = decodeCachedTranscript("""[{"role":"assistant","content":"answer"}]""")

        assertEquals(0L, decoded?.single()?.timestampMs)
    }

    @Test
    fun explicitNullTimestampIsRejectedRatherThanInventingTime() {
        assertNull(decodeCachedTranscript("""[{"role":"assistant","content":"answer","ts":null}]"""))
    }

    @Test
    fun exactUserRoleDecodesAsUser() {
        val decoded = decodeCachedTranscript("""[{"role":"user","content":"q"}]""")

        assertEquals(History.Role.USER, decoded?.single()?.role)
    }

    @Test
    fun everyOtherPersistedRoleFallsBackToAssistant() {
        for (role in listOf("assistant", "tool", "USER", "", "unknown")) {
            val decoded = decodeCachedTranscript("""[{"role":"$role","content":"x"}]""")
            assertEquals(History.Role.ASSISTANT, decoded?.single()?.role, role)
        }
    }

    @Test
    fun encodeReturnsNullForEmptyTranscript() {
        assertNull(encodeCachedTranscript(emptyList()))
    }

    @Test
    fun encodeDropsAllKindsOfBlankContent() {
        val rows = listOf(
            history(History.Role.USER, ""),
            history(History.Role.ASSISTANT, "   "),
            history(History.Role.TOOL, "\n\t"),
        )

        assertNull(encodeCachedTranscript(rows))
    }

    @Test
    fun encodeDropsBlankRowsButPreservesRelativeOrderOfTextRows() {
        val rows = listOf(
            history(History.Role.USER, "first", 1),
            history(History.Role.ASSISTANT, " ", 2),
            history(History.Role.ASSISTANT, "second", 3),
            history(History.Role.TOOL, "", 4),
            history(History.Role.USER, "third", 5),
        )

        val decoded = decodeCachedTranscript(encodeCachedTranscript(rows)!!)

        assertEquals(listOf("first", "second", "third"), decoded?.map { it.content })
        assertEquals(listOf(1L, 3L, 5L), decoded?.map { it.timestampMs })
    }

    @Test
    fun nonBlankWhitespaceAtEdgesIsNotTrimmed() {
        val content = "  keep edges \n"

        val decoded = decodeCachedTranscript(encodeCachedTranscript(listOf(history(History.Role.USER, content)))!!)

        assertEquals(content, decoded?.single()?.content)
    }

    @Test
    fun userAndAssistantRolesRoundTrip() {
        val rows = listOf(
            history(History.Role.USER, "question"),
            history(History.Role.ASSISTANT, "answer"),
        )

        val decoded = decodeCachedTranscript(encodeCachedTranscript(rows)!!)

        assertEquals(listOf(History.Role.USER, History.Role.ASSISTANT), decoded?.map { it.role })
    }

    @Test
    fun toolAndExecutingRowsArePersistedAsAssistantText() {
        val rows = listOf(
            history(History.Role.TOOL, "tool result"),
            history(History.Role.TOOL_EXECUTING, "running"),
        )

        val decoded = decodeCachedTranscript(encodeCachedTranscript(rows)!!)

        assertEquals(listOf(History.Role.ASSISTANT, History.Role.ASSISTANT), decoded?.map { it.role })
        assertEquals(listOf("tool result", "running"), decoded?.map { it.content })
    }

    @Test
    fun attachmentsAndTransientMetadataAreNeverSerialized() {
        val row = History(
            id = "original-id",
            role = History.Role.ASSISTANT,
            content = "visible",
            attachments = persistentListOf(Attachment(data = "BASE64", mimeType = "image/png", fileName = "report.png")),
            toolCallId = "call",
            toolName = "search",
            isThinking = true,
            isStatusMessage = true,
            reasoningContent = "private",
            timestampMs = 9,
        )

        val encoded = encodeCachedTranscript(listOf(row))!!
        val decoded = decodeCachedTranscript(encoded)!!.single()

        for (forbidden in listOf("BASE64", "original-id", "toolCallId", "search", "attachments")) {
            assertFalse(forbidden in encoded, forbidden)
        }
        assertEquals(emptyList(), decoded.attachments)
        assertNull(decoded.toolCallId)
        assertFalse(decoded.isThinking)
        assertFalse(decoded.isStatusMessage)
        // reasoningContent is deliberately cached now (cold-start reasoning block)
        // and round-trips — unlike the transient metadata above.
        assertEquals("private", decoded.reasoningContent)
    }

    @Test
    fun decodedRowsReceiveFreshRuntimeIds() {
        val original = history(History.Role.USER, "hello", id = "persisted-id")

        val decoded = decodeCachedTranscript(encodeCachedTranscript(listOf(original))!!)?.single()

        assertNotEquals("persisted-id", decoded?.id)
        assertTrue(decoded?.id?.isNotBlank() == true)
    }

    @Test
    fun unicodeEscapesQuotesAndNewlinesRoundTripExactly() {
        val content = "한글 🚀 \"quoted\" \\ slash\nsecond\tcolumn"

        val decoded = decodeCachedTranscript(encodeCachedTranscript(listOf(history(History.Role.ASSISTANT, content)))!!)

        assertEquals(content, decoded?.single()?.content)
    }

    @Test
    fun extremeTimestampsRoundTripWithoutNarrowing() {
        val rows = listOf(
            history(History.Role.USER, "min", Long.MIN_VALUE),
            history(History.Role.ASSISTANT, "zero", 0),
            history(History.Role.USER, "max", Long.MAX_VALUE),
        )

        val decoded = decodeCachedTranscript(encodeCachedTranscript(rows)!!)

        assertEquals(listOf(Long.MIN_VALUE, 0L, Long.MAX_VALUE), decoded?.map { it.timestampMs })
    }

    @Test
    fun payloadBelowCharacterBudgetIsCacheable() {
        val content = "x".repeat(TX_CACHE_MAX_CHARS - 100)

        val encoded = encodeCachedTranscript(listOf(history(History.Role.USER, content)))

        assertTrue(encoded != null)
        assertTrue(encoded.length <= TX_CACHE_MAX_CHARS)
        assertEquals(content.length, decodeCachedTranscript(encoded)?.single()?.content?.length)
    }

    @Test
    fun payloadAboveCharacterBudgetIsRejected() {
        val content = "x".repeat(TX_CACHE_MAX_CHARS)

        assertNull(encodeCachedTranscript(listOf(history(History.Role.USER, content))))
    }

    @Test
    fun budgetCountsSerializedCharactersIncludingEscapingOverhead() {
        val plain = "x".repeat(TX_CACHE_MAX_CHARS - 100)
        val escapeHeavy = "\"".repeat(TX_CACHE_MAX_CHARS - 100)

        assertTrue(encodeCachedTranscript(listOf(history(History.Role.USER, plain))) != null)
        assertNull(encodeCachedTranscript(listOf(history(History.Role.USER, escapeHeavy))))
    }

    @Test
    fun encodingDoesNotMutateCallerOwnedHistoryList() {
        val rows = mutableListOf(
            history(History.Role.USER, "q", 1),
            history(History.Role.ASSISTANT, "a", 2),
        )
        val before = rows.toList()

        encodeCachedTranscript(rows)

        assertEquals(before, rows)
    }
}
