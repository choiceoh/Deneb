package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * How long the event stream waits before reconnecting.
 *
 * The backoff only ever applied to exceptions, but a clean EOF — and, because
 * readSseLines turns a mid-stream read error into a normal return, a reset socket
 * too — left the loop through the success path, where it reconnected instantly and
 * re-ran its sync + reconcile kick each lap.
 */
class EventStreamReconnectGapTest {

    @Test
    fun aStreamThatLivedReconnectsPromptly() {
        assertEquals(EVENTS_MIN_RECONNECT_GAP_MS, reconnectGapMs(60_000))
        assertEquals(EVENTS_MIN_RECONNECT_GAP_MS, reconnectGapMs(EVENTS_SHORT_CONNECTION_MS))
    }

    @Test
    fun aStreamThatDiedImmediatelyIsGivenRoom() {
        // 200-then-close (a proxy, or a half-deployed gateway) is the spin case.
        assertEquals(EVENTS_SHORT_CONNECTION_GAP_MS, reconnectGapMs(0))
        assertEquals(EVENTS_SHORT_CONNECTION_GAP_MS, reconnectGapMs(EVENTS_SHORT_CONNECTION_MS - 1))
    }

    @Test
    fun everyOutcomeWaitsAtLeastSomething() {
        // The property that matters: no path reconnects with zero delay.
        for (alive in listOf(0L, 1L, 999L, 5_000L, 120_000L)) {
            assertTrue(reconnectGapMs(alive) > 0, "alive=$alive")
        }
    }
}
