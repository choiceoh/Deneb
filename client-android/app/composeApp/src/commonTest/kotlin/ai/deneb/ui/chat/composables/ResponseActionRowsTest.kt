package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.History
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * One answer arrives as SEVERAL assistant messages — text, a tool call, more
 * text — and every fragment used to draw its own 복사/⋯ row, so a single turn
 * showed the same two icons three or four times down the thread.
 *
 * The row belongs to the response. This pins both halves of that: which message
 * carries it, and that copying from there yields the WHOLE answer rather than
 * the closing paragraph alone.
 */
class ResponseActionRowsTest {
    private fun user(id: String, text: String = "질문") = History(id = id, role = History.Role.USER, content = text)

    private fun assistant(id: String, text: String, thinking: Boolean = false) = History(id = id, role = History.Role.ASSISTANT, content = text, isThinking = thinking)

    @Test
    fun onlyTheLastFragmentOfAResponseCarriesTheRow() {
        val history = listOf(
            user("u1"),
            assistant("a1", "확인해볼게요."),
            assistant("a2", "찾았습니다."),
            assistant("a3", "정리하면 이렇습니다."),
        )
        val (rows, _) = responseActionRows(history)
        assertEquals(setOf("a3"), rows, "one row per response, on its last fragment")
    }

    @Test
    fun copyFromTheTailYieldsTheWholeResponse() {
        val history = listOf(
            user("u1"),
            assistant("a1", "확인해볼게요."),
            assistant("a2", "찾았습니다."),
        )
        val (_, text) = responseActionRows(history)
        assertEquals("확인해볼게요.\n\n찾았습니다.", text["a2"])
        assertNull(text["a1"], "an intermediate fragment has no copy payload")
    }

    @Test
    fun eachResponseGetsItsOwnRow() {
        val history = listOf(
            user("u1"),
            assistant("a1", "첫 답변 조각"),
            assistant("a2", "첫 답변 마무리"),
            user("u2"),
            assistant("a3", "둘째 답변"),
        )
        val (rows, text) = responseActionRows(history)
        assertEquals(setOf("a2", "a3"), rows)
        assertEquals("첫 답변 조각\n\n첫 답변 마무리", text["a2"])
        assertEquals("둘째 답변", text["a3"])
    }

    @Test
    fun thinkingAndEmptyTurnsAreNotAnswerText() {
        // A thinking-only turn and a bare tool-call turn carry no answer, so they
        // must neither take the row nor leak into the copied text.
        val history = listOf(
            user("u1"),
            assistant("t1", "속으로 생각 중", thinking = true),
            assistant("e1", ""),
            assistant("a1", "실제 답변"),
        )
        val (rows, text) = responseActionRows(history)
        assertEquals(setOf("a1"), rows)
        assertEquals("실제 답변", text["a1"])
    }

    @Test
    fun aResponseWithNoAnswerTextGetsNoRow() {
        // The turn is still running: thinking only, no answer yet. Showing a copy
        // row under nothing would invite acting on an empty answer.
        val history = listOf(user("u1"), assistant("t1", "생각 중", thinking = true))
        val (rows, text) = responseActionRows(history)
        assertTrue(rows.isEmpty())
        assertTrue(text.isEmpty())
    }

    @Test
    fun emptyHistoryIsEmpty() {
        val (rows, text) = responseActionRows(emptyList())
        assertTrue(rows.isEmpty())
        assertTrue(text.isEmpty())
    }
}
