package ai.deneb.data

import kotlinx.coroutines.sync.Mutex
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertIs
import kotlin.test.assertTrue

class StoredJsonTest {

    @Test
    fun validStorageDecodesWithoutCallingFallbacks() {
        var defaultCalls = 0
        var malformedCalls = 0

        val result = decodeStoredJsonOrDefault(
            raw = "41",
            defaultValue = {
                defaultCalls += 1
                -1
            },
            onMalformed = { malformedCalls += 1 },
            decode = { it.toInt() + 1 },
        )

        assertEquals(42, result)
        assertEquals(0, defaultCalls)
        assertEquals(0, malformedCalls)
    }

    @Test
    fun emptyStorageReturnsDefaultWithoutDecodingOrReportingMalformedData() {
        var decoded = false
        var malformedCalls = 0

        val result = decodeStoredJsonOrDefault(
            raw = "",
            defaultValue = { 7 },
            onMalformed = { malformedCalls += 1 },
            decode = {
                decoded = true
                it.toInt()
            },
        )

        assertEquals(7, result)
        assertFalse(decoded)
        assertEquals(0, malformedCalls)
    }

    @Test
    fun malformedStorageReportsTheDecodeExceptionAndReturnsDefault() {
        var observed: Exception? = null

        val result = decodeStoredJsonOrDefault(
            raw = "broken",
            defaultValue = { 9 },
            onMalformed = { observed = it },
            decode = { throw IllegalArgumentException("bad json") },
        )

        assertEquals(9, result)
        val error = assertIs<IllegalArgumentException>(observed)
        assertEquals("bad json", error.message)
    }

    @Test
    fun malformedObserverFailureDoesNotReplaceTheSafeDefault() {
        val result = decodeStoredJsonOrDefault(
            raw = "broken",
            defaultValue = { 11 },
            onMalformed = { error("observer failed") },
            decode = { throw IllegalArgumentException("bad json") },
        )

        assertEquals(11, result)
    }

    @Test
    fun mutexOwnerRepairsMalformedStorageAndReleasesTheLock() {
        val mutex = Mutex()
        var clears = 0

        val result = mutex.loadStoredJsonOrDefault(
            readJson = { "broken" },
            clearMalformed = { clears += 1 },
            defaultValue = { 13 },
            decode = { throw IllegalArgumentException("bad json") },
        )

        assertEquals(13, result)
        assertEquals(1, clears)
        assertFalse(mutex.isLocked)
    }

    @Test
    fun readerWithoutMutexOwnershipCannotClearAnIncomingWrite() {
        val mutex = Mutex()
        assertTrue(mutex.tryLock())
        var clears = 0

        try {
            val result = mutex.loadStoredJsonOrDefault(
                readJson = { "broken" },
                clearMalformed = { clears += 1 },
                defaultValue = { 15 },
                decode = { throw IllegalArgumentException("bad json") },
            )

            assertEquals(15, result)
            assertEquals(0, clears)
            assertTrue(mutex.isLocked)
        } finally {
            mutex.unlock()
        }
    }
}
