package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/**
 * Card interactions survive a reload as interactions.
 *
 * The client sends a tap as `[deneb-ui] event=… values={…}` — text meant for the
 * agent — and the gateway stores it verbatim. Without this parse, reopening the
 * conversation showed that raw line in the transcript as if the user had typed it,
 * and the card above never returned to its answered state.
 */
class UiCallbackWireTest {

    @Test
    fun readsABarePress() {
        val submission = parseUiCallback("[deneb-ui] event=confirm", "카드 본문")

        assertEquals("confirm", submission?.pressedEvent)
        assertEquals(emptyMap(), submission?.values)
        assertEquals("카드 본문", submission?.sourceContent)
    }

    @Test
    fun readsSubmittedValues() {
        val submission = parseUiCallback("[deneb-ui] event=submit values={qty=3, note=급함}", "카드 본문")

        assertEquals("submit", submission?.pressedEvent)
        assertEquals(mapOf("qty" to "3", "note" to "급함"), submission?.values)
    }

    @Test
    fun keepsAValueThatContainsItsOwnEquals() {
        val submission = parseUiCallback("[deneb-ui] event=submit values={url=a=b}", "")

        assertEquals(mapOf("url" to "a=b"), submission?.values)
    }

    @Test
    fun leavesOrdinaryMessagesAlone() {
        assertNull(parseUiCallback("오늘 일정 알려줘", "카드 본문"))
        // Only a line that STARTS with the marker is a callback — a message quoting
        // it (asking about the format, say) is still a message.
        assertNull(parseUiCallback("이건 [deneb-ui] event=confirm 형식이 뭐야?", "카드 본문"))
        assertNull(parseUiCallback("[deneb-ui] event=", "카드 본문"))
    }
}
