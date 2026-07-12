package ai.deneb

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SandboxContractsTest {

    @Test
    fun sentinelSessionIdsAreStableAndDistinct() {
        assertEquals("__default__", SandboxSessions.DEFAULT)
        assertEquals("__system__", SandboxSessions.SYSTEM)
        assertEquals("__terminal__", SandboxSessions.TERMINAL)
        assertEquals(3, setOf(SandboxSessions.DEFAULT, SandboxSessions.SYSTEM, SandboxSessions.TERMINAL).size)
    }

    @Test
    fun sentinelAndBlankSessionsAreNotPersistable() {
        for (id in listOf("", "   ", SandboxSessions.DEFAULT, SandboxSessions.SYSTEM, SandboxSessions.TERMINAL)) {
            assertFalse(SandboxSessions.isPersistable(id), id)
        }
    }

    @Test
    fun realConversationAndBackgroundIdsArePersistable() {
        for (id in listOf("client:main", "client:main:uuid", "chat:legacy", "cron:job:123", "system:boot")) {
            assertTrue(SandboxSessions.isPersistable(id), id)
        }
    }

    @Test
    fun sentinelMatchingIsExactAndCaseSensitive() {
        for (id in listOf("__DEFAULT__", "__default", " __default__", "__system__x", "terminal")) {
            assertTrue(SandboxSessions.isPersistable(id), id)
        }
    }

    @Test
    fun noOpHandleNeverReportsCancellation() {
        assertFalse(NoOpCommandHandle.isCancelled())
        NoOpCommandHandle.cancel()
        NoOpCommandHandle.cancel()
        assertFalse(NoOpCommandHandle.isCancelled())
    }

    @Test
    fun noOpHandleAcceptsInputAndReturnsUnavailableExitCode() = runTest {
        NoOpCommandHandle.writeInput("")
        NoOpCommandHandle.writeInput("echo test")

        assertEquals(-1, NoOpCommandHandle.awaitExit())
        assertEquals(-1, NoOpCommandHandle.awaitExit())
    }

    @Test
    fun noOpHandleCancellationLeavesTerminalStateUnavailable() = runTest {
        NoOpCommandHandle.cancel()

        assertFalse(NoOpCommandHandle.isCancelled())
        assertEquals(-1, NoOpCommandHandle.awaitExit())
    }

    @Test
    fun noOpHandleWriteAfterCancellationRemainsSafeAndStateless() = runTest {
        NoOpCommandHandle.cancel()

        NoOpCommandHandle.writeInput("input after cancel")

        assertFalse(NoOpCommandHandle.isCancelled())
        assertEquals(-1, NoOpCommandHandle.awaitExit())
    }
}
