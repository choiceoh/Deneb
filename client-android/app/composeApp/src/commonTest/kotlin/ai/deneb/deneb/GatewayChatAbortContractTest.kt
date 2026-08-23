package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * The stop button's server half.
 *
 * The native chat stream detaches its run from the request context so a
 * backgrounded phone cannot kill a turn mid-flight. That makes closing the socket
 * useless as a stop signal — pressing ■ cancelled only the local coroutine while
 * the gateway kept generating, and the finished answer then replaced the
 * "중단됨" reply at the next reconcile.
 */
class GatewayChatAbortContractTest {

    @Test
    fun abortAsksTheGatewayToCancelThisSessionsRun() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true,"aborted":true,"sessionKey":"client:main"}""")

        val aborted = f.client.abortTurn()

        assertTrue(aborted)
        val params = f.transport.requests.single().requireRpc("miniapp.chat.abort")
        assertEquals("client:main", params["sessionKey"]?.jsonPrimitive?.content)
    }

    @Test
    fun abortReportsFalseWhenNothingWasRunning() = runTest {
        // Stopping just after the turn finished is not an error — it just did not
        // cancel anything, and the UI must not claim otherwise.
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true,"aborted":false,"sessionKey":"client:main"}""")

        assertFalse(f.client.abortTurn())
    }

    @Test
    fun abortReportsFalseWhenTheGatewayCannotBeReached() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(IllegalStateException("offline"))

        assertFalse(f.client.abortTurn())
    }

    @Test
    fun abortIsSkippedWithoutAToken() = runTest {
        val f = gatewayClientFixture(token = "")

        assertFalse(f.client.abortTurn())
        assertTrue(f.transport.requests.isEmpty())
    }
}
