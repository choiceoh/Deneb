package ai.deneb.data

import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertIs
import kotlin.test.assertTrue

class StoredJsonDocumentTest {

    private class Fixture(
        initial: String = "",
        malformedPolicy: MalformedStoredJsonPolicy = MalformedStoredJsonPolicy.CLEAR,
        migrate: (Int) -> StoredJsonMigration<Int> = ::unchangedStoredJson,
    ) {
        var raw = initial
        var failWrites = false
        var reads = 0
        var malformedCalls = 0
        var repairFailures = 0
        var beforeWrite: ((String) -> Unit)? = null
        val writes = mutableListOf<String>()

        val document = StoredJsonDocument(
            readJson = {
                reads++
                raw
            },
            writeJson = { value ->
                beforeWrite?.invoke(value)
                if (failWrites) error("write unavailable")
                raw = value
                writes += value
            },
            defaultValue = { 0 },
            decode = { it.toInt() },
            encode = { it.toString() },
            malformedPolicy = malformedPolicy,
            migrate = migrate,
            onMalformed = { malformedCalls++ },
            onRepairFailure = { repairFailures++ },
        )
    }

    @Test
    fun decoderClassifiesAbsentDecodedAndMalformedPayloads() {
        assertEquals(StoredJsonDecode.Absent, decodeStoredJson("") { it.toInt() })

        val decoded = assertIs<StoredJsonDecode.Decoded<Int>>(decodeStoredJson("42") { it.toInt() })
        assertEquals(42, decoded.value)

        val malformed = assertIs<StoredJsonDecode.Malformed>(decodeStoredJson("broken") { it.toInt() })
        assertIs<NumberFormatException>(malformed.error)
    }

    @Test
    fun emptyReadReturnsDefaultWithoutDecodeOrWrite() {
        val fixture = Fixture()

        assertEquals(0, fixture.document.read())

        assertEquals(1, fixture.reads)
        assertEquals(0, fixture.malformedCalls)
        assertEquals(emptyList(), fixture.writes)
    }

    @Test
    fun validReadReturnsDecodedValueWithoutWrite() {
        val fixture = Fixture(initial = "41")

        assertEquals(41, fixture.document.read())

        assertEquals(1, fixture.reads)
        assertEquals(0, fixture.malformedCalls)
        assertEquals(emptyList(), fixture.writes)
    }

    @Test
    fun malformedReadReturnsDefaultAndClearsPayloadOnce() {
        val fixture = Fixture(initial = "broken")

        assertEquals(0, fixture.document.read())
        assertEquals("", fixture.raw)
        assertEquals(listOf(""), fixture.writes)
        assertEquals(1, fixture.malformedCalls)

        assertEquals(0, fixture.document.read())
        assertEquals(listOf(""), fixture.writes)
        assertEquals(1, fixture.malformedCalls)
    }

    @Test
    fun preservePolicyReturnsDefaultWithoutMutatingMalformedPayload() {
        val fixture = Fixture(
            initial = "broken",
            malformedPolicy = MalformedStoredJsonPolicy.PRESERVE,
        )

        assertEquals(0, fixture.document.read())

        assertEquals("broken", fixture.raw)
        assertEquals(emptyList(), fixture.writes)
        assertEquals(1, fixture.malformedCalls)
    }

    @Test
    fun malformedObserverFailureCannotReplaceSafeDefaultOrRepair() {
        var raw = "broken"
        val document = StoredJsonDocument(
            readJson = { raw },
            writeJson = { raw = it },
            defaultValue = { 7 },
            decode = { it.toInt() },
            encode = { it.toString() },
            onMalformed = { error("observer failed") },
        )

        assertEquals(7, document.read())
        assertEquals("", raw)
    }

    @Test
    fun migrationReturnsNormalizedValueAndRewritesStorage() {
        val fixture = Fixture(
            initial = "1",
            migrate = { value -> rewrittenStoredJson(value + 1) },
        )

        assertEquals(2, fixture.document.read())

        assertEquals("2", fixture.raw)
        assertEquals(listOf("2"), fixture.writes)
    }

    @Test
    fun unchangedMigrationDoesNotRewriteStorage() {
        val fixture = Fixture(initial = "5")

        assertEquals(5, fixture.document.read())

        assertEquals("5", fixture.raw)
        assertEquals(emptyList(), fixture.writes)
    }

    @Test
    fun failedMigrationRepairStillReturnsValueAndRetriesLater() {
        val fixture = Fixture(
            initial = "10",
            migrate = { rewrittenStoredJson(it + 1) },
        )
        fixture.failWrites = true

        assertEquals(11, fixture.document.read())
        assertEquals("10", fixture.raw)
        assertEquals(1, fixture.repairFailures)

        fixture.failWrites = false
        assertEquals(11, fixture.document.read())
        assertEquals("11", fixture.raw)
        assertEquals(listOf("11"), fixture.writes)
    }

    @Test
    fun repairFailureObserverIsBestEffort() {
        var raw = "1"
        val document = StoredJsonDocument(
            readJson = { raw },
            writeJson = { error("write unavailable") },
            defaultValue = { 0 },
            decode = { it.toInt() },
            encode = { it.toString() },
            migrate = { rewrittenStoredJson(it + 1) },
            onRepairFailure = { error("observer failed") },
        )

        assertEquals(2, document.read())
        assertEquals("1", raw)
    }

    @Test
    fun persistedMutationWritesExactlyOnceAndReturnsIndependentResult() = runTest {
        val fixture = Fixture(initial = "4")

        val result = fixture.document.mutate { current ->
            persistStoredJson(current + 3, "saved")
        }

        assertEquals("saved", result)
        assertEquals("7", fixture.raw)
        assertEquals(listOf("7"), fixture.writes)
        assertEquals(7, fixture.document.read())
    }

    @Test
    fun keptMutationDoesNotWriteHealthyStorage() = runTest {
        val fixture = Fixture(initial = "8")

        val result = fixture.document.mutate { keepStoredJson("unchanged") }

        assertEquals("unchanged", result)
        assertEquals("8", fixture.raw)
        assertEquals(emptyList(), fixture.writes)
    }

    @Test
    fun keptMutationRepairsMalformedStorageWhenPolicyAllowsIt() = runTest {
        val fixture = Fixture(initial = "broken")

        val result = fixture.document.mutate { keepStoredJson("missing") }

        assertEquals("missing", result)
        assertEquals("", fixture.raw)
        assertEquals(listOf(""), fixture.writes)
        assertEquals(1, fixture.malformedCalls)
    }

    @Test
    fun keptMutationPersistsPendingMigration() = runTest {
        val fixture = Fixture(
            initial = "20",
            migrate = { rewrittenStoredJson(it + 1) },
        )

        assertFalse(fixture.document.mutate { keepStoredJson(false) })

        assertEquals("21", fixture.raw)
        assertEquals(listOf("21"), fixture.writes)
    }

    @Test
    fun persistedMutationSupersedesMigrationWithoutAnIntermediateWrite() = runTest {
        val fixture = Fixture(
            initial = "30",
            migrate = { rewrittenStoredJson(it + 1) },
        )

        fixture.document.mutate { current -> persistStoredJson(current + 1, Unit) }

        assertEquals("32", fixture.raw)
        assertEquals(listOf("32"), fixture.writes)
    }

    @Test
    fun persistedMutationHealsMalformedStorageWithNewValue() = runTest {
        val fixture = Fixture(initial = "broken")

        fixture.document.mutate { current -> persistStoredJson(current + 5, Unit) }

        assertEquals("5", fixture.raw)
        assertEquals(listOf("5"), fixture.writes)
        assertEquals(1, fixture.malformedCalls)
    }

    @Test
    fun mutationWriteFailurePropagatesWithoutChangingStoredValue() = runTest {
        val fixture = Fixture(initial = "12")
        fixture.failWrites = true

        assertFailsWith<IllegalStateException> {
            fixture.document.mutate { current -> persistStoredJson(current + 1, Unit) }
        }

        assertEquals("12", fixture.raw)
        assertEquals(emptyList(), fixture.writes)
    }

    @Test
    fun readerDuringMutationCannotClearIncomingWrite() = runTest {
        val fixture = Fixture(initial = "broken")
        var readDuringWrite: Int? = null
        fixture.beforeWrite = { readDuringWrite = fixture.document.read() }

        fixture.document.mutate { persistStoredJson(99, Unit) }

        assertEquals(0, readDuringWrite)
        assertEquals("99", fixture.raw)
        assertEquals(listOf("99"), fixture.writes)
    }

    @Test
    fun readerDuringMigrationRewriteCannotPerformASecondRepair() {
        val fixture = Fixture(
            initial = "1",
            migrate = { rewrittenStoredJson(it + 1) },
        )
        var nestedRead: Int? = null
        fixture.beforeWrite = { nestedRead = fixture.document.read() }

        assertEquals(2, fixture.document.read())

        assertEquals(2, nestedRead)
        assertEquals(listOf("2"), fixture.writes)
        assertEquals("2", fixture.raw)
    }

    @Test
    fun concurrentMutationsDoNotLoseUpdates() = runTest {
        val fixture = Fixture(initial = "0")

        coroutineScope {
            repeat(50) {
                launch {
                    fixture.document.mutate { current ->
                        persistStoredJson(current + 1, Unit)
                    }
                }
            }
        }

        assertEquals(50, fixture.document.read())
        assertEquals(50, fixture.writes.size)
    }

    @Test
    fun directWriteReplacesMalformedStorageWithoutReadingIt() = runTest {
        val fixture = Fixture(initial = "broken")

        fixture.document.write(77)

        assertEquals("77", fixture.raw)
        assertEquals(0, fixture.reads)
        assertEquals(0, fixture.malformedCalls)
        assertEquals(listOf("77"), fixture.writes)
    }

    @Test
    fun directWriteFailurePropagates() = runTest {
        val fixture = Fixture(initial = "3")
        fixture.failWrites = true

        assertFailsWith<IllegalStateException> { fixture.document.write(4) }

        assertEquals("3", fixture.raw)
        assertEquals(emptyList(), fixture.writes)
    }

    @Test
    fun clearDropsPayloadWithoutDecodingIt() = runTest {
        val fixture = Fixture(initial = "broken")

        fixture.document.clear()

        assertEquals("", fixture.raw)
        assertEquals(0, fixture.reads)
        assertEquals(0, fixture.malformedCalls)
        assertEquals(listOf(""), fixture.writes)
        assertTrue(fixture.document.read() == 0)
    }
}
