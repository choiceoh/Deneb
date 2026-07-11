package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class MailCacheBoundaryTest {

    private fun row(
        id: String,
        unread: Boolean = false,
        attachmentCount: Int = 0,
        workState: MailWorkState = MailWorkState(),
    ) = MailMessage(
        id = id,
        from = "sender-$id@example.com",
        subject = "Subject $id",
        snippet = "Snippet $id",
        date = "2026-07-11T09:00:00Z",
        unread = unread,
        priority = "attention",
        priorityHint = "deadline",
        mailbox = "INBOX",
        hasAttachment = attachmentCount > 0,
        attachmentCount = attachmentCount,
        workState = workState,
    )

    @Test
    fun completeMailRowAndWorkStateRoundTrip() {
        val state = MailWorkState(
            analysisStatus = "done",
            analysisQuality = "urgent",
            feedStatus = "created",
            calendarProposalCount = 2,
            todoCount = 3,
            hint = "한글 힌트 🚀",
        )
        val original = row("m1", unread = true, attachmentCount = 4, workState = state)

        assertEquals(listOf(original), decodeMailCache(encodeMailCache(listOf(original), "owner"), "owner"))
    }

    @Test
    fun ownerNormalizesOuterWhitespaceAndAllTrailingSlashes() {
        val first = mailCacheOwner("  https://gateway.example///  ", "token")
        val second = mailCacheOwner("https://gateway.example", "token")

        assertEquals(second, first)
        assertTrue(first.startsWith("https://gateway.example#"))
    }

    @Test
    fun ownerPreservesUrlCaseAndPathSemantics() {
        assertFalse(mailCacheOwner("HTTPS://GW/Path", "t") == mailCacheOwner("https://gw/Path", "t"))
        assertFalse(mailCacheOwner("https://gw/a", "t") == mailCacheOwner("https://gw/b", "t"))
    }

    @Test
    fun differentTokensProduceDifferentOwnerFingerprints() {
        assertFalse(mailCacheOwner("https://gw", "token-a") == mailCacheOwner("https://gw", "token-b"))
    }

    @Test
    fun sameLengthTokensStillProduceDifferentFingerprints() {
        val first = mailCacheOwner("https://gw", "aaaa")
        val second = mailCacheOwner("https://gw", "aaab")

        assertFalse(first == second)
        assertTrue(first.startsWith("https://gw#4:"))
        assertTrue(second.startsWith("https://gw#4:"))
    }

    @Test
    fun unicodeTokenFingerprintIsDeterministicAndDoesNotLeakToken() {
        val token = "비밀🚀token"
        val first = mailCacheOwner("https://gw", token)
        val second = mailCacheOwner("https://gw", token)

        assertEquals(first, second)
        assertFalse(token in first)
    }

    @Test
    fun ownerFingerprintDoesNotContainRawToken() {
        val token = "super-secret-client-token"
        val owner = mailCacheOwner("https://gw", token)

        assertFalse(token in owner)
        assertTrue(owner.startsWith("https://gw#${token.length}:"))
    }

    @Test
    fun emptyUrlAndTokenStillProduceDeterministicOwner() {
        val first = mailCacheOwner("", "")
        val second = mailCacheOwner("   ", "")

        assertEquals(first, second)
        assertTrue(first.startsWith("#0:"))
    }

    @Test
    fun ownerFingerprintIsDeterministicAcrossRepeatedCalls() {
        val values = (0 until 100).map { mailCacheOwner("https://gw", "token") }.toSet()

        assertEquals(1, values.size)
    }

    @Test
    fun ownerComparisonDuringDecodeIsExact() {
        val encoded = encodeMailCache(listOf(row("m")), "Owner")

        assertEquals(1, decodeMailCache(encoded, "Owner")?.size)
        assertNull(decodeMailCache(encoded, "owner"))
        assertNull(decodeMailCache(encoded, "Owner "))
    }

    @Test
    fun emptyRowsRepresentNoUsableCache() {
        assertNull(decodeMailCache(encodeMailCache(emptyList(), "owner"), "owner"))
    }

    @Test
    fun malformedAndWrongTopLevelShapesFailClosed() {
        for (raw in listOf("", "not-json", "null", "[]", "42", "\"mail\"")) {
            assertNull(decodeMailCache(raw, "owner"), raw)
        }
    }

    @Test
    fun legacyUnscopedRowArrayCannotCrossOwnerBoundary() {
        val legacy = """[{"id":"m","from":"a","subject":"s","snippet":"x","date":"d","unread":true}]"""

        assertNull(decodeMailCache(legacy, "owner"))
    }

    @Test
    fun missingOwnerDefaultsEmptyOnlyForEmptyExpectedOwner() {
        val raw = """{"rows":[{"id":"m","from":"a","subject":"s","snippet":"x","date":"d","unread":true}]}"""

        assertEquals("m", decodeMailCache(raw, "")?.single()?.id)
        assertNull(decodeMailCache(raw, "owner"))
    }

    @Test
    fun missingRowsDefaultsEmptyAndReturnsNull() {
        assertNull(decodeMailCache("""{"owner":"owner"}""", "owner"))
    }

    @Test
    fun explicitNullRowsRejectEnvelope() {
        assertNull(decodeMailCache("""{"owner":"owner","rows":null}""", "owner"))
    }

    @Test
    fun oneMalformedRowRejectsWholeEnvelope() {
        val raw = """
            {"owner":"owner","rows":[
              {"id":"good","from":"a","subject":"s","snippet":"x","date":"d","unread":true},
              {"id":{"bad":true},"from":"a","subject":"s","snippet":"x","date":"d","unread":true}
            ]}
        """.trimIndent()

        assertNull(decodeMailCache(raw, "owner"))
    }

    @Test
    fun unknownEnvelopeRowAndWorkStateFieldsAreIgnored() {
        val raw = """
            {
              "owner":"owner","futureEnvelope":true,
              "rows":[{
                "id":"m","from":"a","subject":"s","snippet":"x","date":"d","unread":true,
                "futureRow":{"x":1},"workState":{"analysisStatus":"done","futureState":true}
              }]
            }
        """.trimIndent()

        val decoded = decodeMailCache(raw, "owner")!!.single()

        assertEquals("m", decoded.id)
        assertEquals("done", decoded.workState.analysisStatus)
    }

    @Test
    fun omittedOptionalFieldsUseDomainDefaults() {
        val raw = """{"owner":"owner","rows":[{"id":"m","from":"a","subject":"s","snippet":"x","date":"d","unread":false}]}"""

        val decoded = decodeMailCache(raw, "owner")!!.single()

        assertEquals("", decoded.priority)
        assertEquals("", decoded.mailbox)
        assertFalse(decoded.hasAttachment)
        assertEquals(0, decoded.attachmentCount)
        assertEquals(MailWorkState(), decoded.workState)
    }

    @Test
    fun explicitNullDefaultedFieldRejectsEnvelope() {
        val raw = """{"owner":"owner","rows":[{"id":"m","from":"a","subject":"s","snippet":"x","date":"d","unread":false,"priority":null}]}"""

        assertNull(decodeMailCache(raw, "owner"))
    }

    @Test
    fun encoderCapsDefaultInboxAtSixtyRows() {
        val original = (0 until 90).map { row("m$it") }

        val decoded = decodeMailCache(encodeMailCache(original, "owner"), "owner")!!

        assertEquals(60, decoded.size)
        assertEquals("m0", decoded.first().id)
        assertEquals("m59", decoded.last().id)
    }

    @Test
    fun decoderCapsMaliciousOversizedPersistedEnvelope() {
        val rows = (0 until 100).joinToString(",") {
            """{"id":"m$it","from":"a","subject":"s","snippet":"x","date":"d","unread":false}"""
        }
        val raw = """{"owner":"owner","rows":[$rows]}"""

        val decoded = decodeMailCache(raw, "owner")!!

        assertEquals(60, decoded.size)
        assertEquals("m59", decoded.last().id)
    }

    @Test
    fun duplicateIdsAndOriginalOrderingArePreserved() {
        val original = listOf(row("same").copy(subject = "first"), row("same").copy(subject = "second"), row("other"))

        val decoded = decodeMailCache(encodeMailCache(original, "owner"), "owner")!!

        assertEquals(listOf("first", "second", "Subject other"), decoded.map { it.subject })
    }

    @Test
    fun integerExtremesInCountsRoundTrip() {
        val state = MailWorkState(calendarProposalCount = Int.MIN_VALUE, todoCount = Int.MAX_VALUE)
        val original = row("m", attachmentCount = Int.MAX_VALUE, workState = state)

        val decoded = decodeMailCache(encodeMailCache(listOf(original), "owner"), "owner")!!.single()

        assertEquals(Int.MAX_VALUE, decoded.attachmentCount)
        assertEquals(Int.MIN_VALUE, decoded.workState.calendarProposalCount)
        assertEquals(Int.MAX_VALUE, decoded.workState.todoCount)
    }

    @Test
    fun attachmentFlagAndCountRemainIndependentWireFacts() {
        val contradictory = row("m", attachmentCount = 0).copy(
            hasAttachment = false,
            attachmentCount = 7,
        )

        val decoded = decodeMailCache(encodeMailCache(listOf(contradictory), "owner"), "owner")!!.single()

        assertFalse(decoded.hasAttachment)
        assertEquals(7, decoded.attachmentCount)
    }

    @Test
    fun blankRequiredDisplayFieldsRoundTripWithoutInventedContent() {
        val blank = MailMessage(
            id = "",
            from = "",
            subject = "",
            snippet = "",
            date = "",
            unread = false,
        )

        assertEquals(blank, decodeMailCache(encodeMailCache(listOf(blank), "owner"), "owner")?.single())
    }

    @Test
    fun unreadAndReadRowsRemainDistinct() {
        val original = listOf(row("unread", unread = true), row("read", unread = false))

        val decoded = decodeMailCache(encodeMailCache(original, "owner"), "owner")!!

        assertTrue(decoded.first().unread)
        assertFalse(decoded.last().unread)
    }

    @Test
    fun unicodeEscapesAndMultilineTextRoundTripExactly() {
        val original = row("m").copy(
            from = "김 담당 <kim@example.com>",
            subject = "한글 🚀 \"quote\"",
            snippet = "line1\nline2\tend",
        )

        assertEquals(original, decodeMailCache(encodeMailCache(listOf(original), "owner"), "owner")?.single())
    }

    @Test
    fun maliciousOwnerTextIsEscapedAndComparedAsData() {
        val owner = "owner\"}\n{injected"
        val encoded = encodeMailCache(listOf(row("m")), owner)

        assertEquals(1, decodeMailCache(encoded, owner)?.size)
        assertNull(decodeMailCache(encoded, "owner"))
    }

    @Test
    fun encodingDoesNotMutateCallerOwnedList() {
        val original = mutableListOf(row("m1"), row("m2"))
        val before = original.toList()

        encodeMailCache(original, "owner")

        assertEquals(before, original)
    }

    @Test
    fun repeatedEncodeDecodeIsStableAtDomainLevel() {
        val original = listOf(row("m", unread = true, workState = MailWorkState(analysisStatus = "done")))
        val once = decodeMailCache(encodeMailCache(original, "owner"), "owner")!!
        val twice = decodeMailCache(encodeMailCache(once, "owner"), "owner")!!

        assertEquals(once, twice)
    }

    @Test
    fun truncationDoesNotSortOrMutateOversizedInput() {
        val original = (0 until 70).map { row("m${69 - it}") }
        val before = original.toList()

        val decoded = decodeMailCache(encodeMailCache(original, "owner"), "owner")!!

        assertEquals(before, original)
        assertEquals("m69", decoded.first().id)
        assertEquals("m10", decoded.last().id)
    }
}
