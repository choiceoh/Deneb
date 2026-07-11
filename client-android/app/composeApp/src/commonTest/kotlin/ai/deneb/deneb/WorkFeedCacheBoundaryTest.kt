package ai.deneb.deneb

import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class WorkFeedCacheBoundaryTest {

    private fun item(
        id: String,
        title: String = "Title $id",
        priority: Int = 0,
        actions: List<WorkFeedAction> = emptyList(),
    ) = WorkFeedItem(
        id = id,
        source = "proactive",
        title = title,
        summary = "Summary $id",
        body = "Body $id",
        sessionKey = "client:main:$id",
        status = "unread",
        priority = priority,
        actions = actions,
        question = false,
        createdAtMs = 100,
        readAtMs = 0,
    )

    @Test
    fun fullItemAndActionFieldsRoundTrip() {
        val action = WorkFeedAction(
            id = "archive",
            kind = "callback",
            label = "보관",
            status = "ready",
            prompt = "archive this",
        )
        val original = item("f1", title = "한글 보고서 🚀", priority = 9, actions = listOf(action)).copy(
            question = true,
            createdAtMs = Long.MAX_VALUE,
            readAtMs = Long.MIN_VALUE,
        )

        assertEquals(listOf(original), decodeWorkFeedCache(encodeWorkFeedCache(listOf(original), "owner"), "owner"))
    }

    @Test
    fun ownerComparisonIsExact() {
        val encoded = encodeWorkFeedCache(listOf(item("f")), "Owner")

        assertEquals(1, decodeWorkFeedCache(encoded, "Owner")?.size)
        assertNull(decodeWorkFeedCache(encoded, "owner"))
        assertNull(decodeWorkFeedCache(encoded, "Owner "))
        assertNull(decodeWorkFeedCache(encoded, " Owner"))
    }

    @Test
    fun emptyOwnerMatchesOnlyEmptyExpectedOwner() {
        val encoded = encodeWorkFeedCache(listOf(item("f")), "")

        assertEquals(1, decodeWorkFeedCache(encoded, "")?.size)
        assertNull(decodeWorkFeedCache(encoded, "x"))
    }

    @Test
    fun emptyItemsRepresentNoUsableBriefing() {
        assertNull(decodeWorkFeedCache(encodeWorkFeedCache(emptyList(), "owner"), "owner"))
    }

    @Test
    fun malformedAndWrongTopLevelShapesFailClosed() {
        for (raw in listOf("", "not-json", "null", "[]", "42", "\"feed\"")) {
            assertNull(decodeWorkFeedCache(raw, "owner"), raw)
        }
    }

    @Test
    fun missingOwnerDefaultsEmptyWithoutCrossOwnerLeak() {
        val raw = """{"items":[{"id":"f"}]}"""

        assertEquals("f", decodeWorkFeedCache(raw, "")?.single()?.id)
        assertNull(decodeWorkFeedCache(raw, "owner"))
    }

    @Test
    fun missingItemsDefaultsEmptyAndReturnsNull() {
        assertNull(decodeWorkFeedCache("""{"owner":"owner"}""", "owner"))
    }

    @Test
    fun explicitNullItemsRejectEnvelope() {
        assertNull(decodeWorkFeedCache("""{"owner":"owner","items":null}""", "owner"))
    }

    @Test
    fun oneMalformedItemRejectsWholeEnvelope() {
        val raw = """{"owner":"owner","items":[{"id":"good"},{"id":{"bad":true}}]}"""

        assertNull(decodeWorkFeedCache(raw, "owner"))
    }

    @Test
    fun unknownEnvelopeItemAndActionFieldsAreIgnored() {
        val raw = """
            {
              "owner":"owner","futureEnvelope":true,
              "items":[{"id":"f","futureItem":{"x":1},"actions":[{"id":"a","futureAction":true}]}]
            }
        """.trimIndent()

        val decoded = decodeWorkFeedCache(raw, "owner")!!.single()

        assertEquals("f", decoded.id)
        assertEquals("a", decoded.actions.single().id)
    }

    @Test
    fun omittedItemAndActionFieldsUseWireDefaults() {
        val decoded = decodeWorkFeedCache(
            """{"owner":"owner","items":[{"id":"f","actions":[{}]}]}""",
            "owner",
        )!!.single()

        assertEquals("", decoded.title)
        assertEquals(0, decoded.priority)
        assertFalse(decoded.question)
        assertEquals(0L, decoded.createdAtMs)
        assertEquals(WorkFeedAction(), decoded.actions.single())
    }

    @Test
    fun explicitNullDefaultedScalarRejectsEnvelope() {
        assertNull(decodeWorkFeedCache("""{"owner":"owner","items":[{"id":"f","title":null}]}""", "owner"))
    }

    @Test
    fun encoderCapsAtFirstEightyItems() {
        val original = (0 until 100).map { item("f$it") }

        val decoded = decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")!!

        assertEquals(80, decoded.size)
        assertEquals("f0", decoded.first().id)
        assertEquals("f79", decoded.last().id)
    }

    @Test
    fun decoderCapsMaliciousOversizedPersistedEnvelope() {
        val items = (0 until 120).joinToString(",") { """{"id":"f$it"}""" }
        val raw = """{"owner":"owner","items":[$items]}"""

        val decoded = decodeWorkFeedCache(raw, "owner")!!

        assertEquals(80, decoded.size)
        assertEquals("f79", decoded.last().id)
    }

    @Test
    fun duplicateIdsArePreservedWithoutInventingServerDedupPolicy() {
        val original = listOf(item("same", title = "first"), item("same", title = "second"))

        assertEquals(
            listOf("first", "second"),
            decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")?.map { it.title },
        )
    }

    @Test
    fun cacheKeepsOriginalOrderingRatherThanSortingPriorityOrTime() {
        val original = listOf(
            item("low", priority = -1).copy(createdAtMs = 300),
            item("high", priority = 99).copy(createdAtMs = 100),
            item("middle", priority = 5).copy(createdAtMs = 200),
        )

        assertEquals(
            listOf("low", "high", "middle"),
            decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")?.map { it.id },
        )
    }

    @Test
    fun priorityExtremesRoundTripWithoutNarrowing() {
        val original = listOf(item("min", priority = Int.MIN_VALUE), item("max", priority = Int.MAX_VALUE))

        assertEquals(
            listOf(Int.MIN_VALUE, Int.MAX_VALUE),
            decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")?.map { it.priority },
        )
    }

    @Test
    fun timestampExtremesRemainDistinct() {
        val original = listOf(
            item("min").copy(createdAtMs = Long.MIN_VALUE, readAtMs = Long.MAX_VALUE),
            item("max").copy(createdAtMs = Long.MAX_VALUE, readAtMs = Long.MIN_VALUE),
        )

        val decoded = decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")!!

        assertEquals(Long.MIN_VALUE, decoded[0].createdAtMs)
        assertEquals(Long.MAX_VALUE, decoded[0].readAtMs)
        assertEquals(Long.MAX_VALUE, decoded[1].createdAtMs)
        assertEquals(Long.MIN_VALUE, decoded[1].readAtMs)
    }

    @Test
    fun questionAndReadStateRoundTripIndependently() {
        val original = listOf(
            item("question").copy(question = true, readAtMs = 0),
            item("read").copy(question = false, readAtMs = 123),
        )

        val decoded = decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")!!

        assertTrue(decoded[0].question)
        assertEquals(0L, decoded[0].readAtMs)
        assertFalse(decoded[1].question)
        assertEquals(123L, decoded[1].readAtMs)
    }

    @Test
    fun unicodeEscapesAndMultilineBodyRoundTripExactly() {
        val body = "한글 🚀 \"quote\" \\ slash\nsecond line\tend"
        val original = item("f").copy(body = body, summary = "요약")

        val decoded = decodeWorkFeedCache(encodeWorkFeedCache(listOf(original), "owner"), "owner")!!.single()

        assertEquals(body, decoded.body)
        assertEquals("요약", decoded.summary)
    }

    @Test
    fun veryLargeBodyIsPreservedBecauseRowCountIsTheCacheContract() {
        val body = "x".repeat(200_000)

        val decoded = decodeWorkFeedCache(
            encodeWorkFeedCache(listOf(item("f").copy(body = body)), "owner"),
            "owner",
        )

        assertEquals(body.length, decoded?.single()?.body?.length)
    }

    @Test
    fun maliciousOwnerTextIsEscapedAndComparedAsData() {
        val owner = "owner\"}\n{injected"
        val encoded = encodeWorkFeedCache(listOf(item("f")), owner)

        assertEquals(1, decodeWorkFeedCache(encoded, owner)?.size)
        assertNull(decodeWorkFeedCache(encoded, "owner"))
    }

    @Test
    fun encodingDoesNotMutateCallerList() {
        val original = mutableListOf(item("f1"), item("f2"))
        val before = original.toList()

        encodeWorkFeedCache(original, "owner")

        assertEquals(before, original)
    }

    @Test
    fun repeatedEncodeDecodeIsStableAtDomainLevel() {
        val original = listOf(item("f", actions = listOf(WorkFeedAction(id = "a"))))
        val once = decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")!!
        val twice = decodeWorkFeedCache(encodeWorkFeedCache(once, "owner"), "owner")!!

        assertEquals(once, twice)
    }

    @Test
    fun truncationDoesNotMutateOrReorderOriginalOversizedList() {
        val original = (0 until 90).map { item("f${89 - it}") }
        val before = original.toList()

        val decoded = decodeWorkFeedCache(encodeWorkFeedCache(original, "owner"), "owner")!!

        assertEquals(before, original)
        assertEquals("f89", decoded.first().id)
        assertEquals("f10", decoded.last().id)
    }
}
