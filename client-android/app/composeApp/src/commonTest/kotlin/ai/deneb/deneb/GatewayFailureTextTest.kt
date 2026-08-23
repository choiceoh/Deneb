package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * What a failed send says in the transcript. The bubble is the only place the user
 * learns why nothing came back, so it has to name the cause in their language and
 * must not carry the gateway URL (ktor's timeout message embeds it).
 */
class GatewayFailureTextTest {

    @Test
    fun rejectedTokenPointsAtSettings() {
        val text = gatewayFailureText(IllegalStateException("chat HTTP 401"))

        assertTrue(text.contains("토큰"), text)
        assertTrue(text.contains("설정"), text)
    }

    @Test
    fun serverSideFailureInvitesARetry() {
        val text = gatewayFailureText(IllegalStateException("stream HTTP 503"))

        assertTrue(text.contains("503"), text)
        assertTrue(text.contains("다시"), text)
    }

    @Test
    fun connectFailureNamesTheConnectionAndLeaksNoUrl() {
        // ktor's own text is "Connect timeout has expired [url=http://100.x.x.x:18789/...]".
        val raw = "Connect timeout has expired [url=http://100.64.0.7:18789/api/v1/miniapp/chat/stream]"

        val text = gatewayFailureText(RuntimeException(raw))

        assertTrue(text.contains("연결하지 못했습니다"), text)
        assertTrue(!text.contains("http"), text)
        assertTrue(!text.contains("100.64"), text)
    }

    @Test
    fun statusIsReadOnlyFromTheTransportsOwnMarker() {
        assertEquals(401, gatewayHttpStatus("chat HTTP 401"))
        assertEquals(503, gatewayHttpStatus("stream HTTP 503"))
        // No response at all → no status, which is the branch that says "연결하지 못했습니다".
        assertNull(gatewayHttpStatus("Connection refused"))
        assertNull(gatewayHttpStatus(null))
        assertNull(gatewayHttpStatus("HTTP but no code"))
    }
}
