package ai.deneb.ui.chat

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ChatEmptyReplyTest {

    private fun msg(
        role: History.Role,
        content: String,
        thinking: Boolean = false,
    ) = History(role = role, content = content, isThinking = thinking)

    @Test
    fun unansweredUserTurnIsDetected() {
        assertTrue(listOf(msg(History.Role.USER, "안녕")).hasUnansweredUserTurn())
    }

    @Test
    fun answeredTurnIsNotUnanswered() {
        assertFalse(
            listOf(
                msg(History.Role.USER, "안녕"),
                msg(History.Role.ASSISTANT, "네"),
            ).hasUnansweredUserTurn(),
        )
    }

    @Test
    fun thinkingOnlyDoesNotCountAsAnswer() {
        assertTrue(
            listOf(
                msg(History.Role.USER, "안녕"),
                msg(History.Role.ASSISTANT, "생각", thinking = true),
            ).hasUnansweredUserTurn(),
        )
    }

    @Test
    fun emptyAssistantBodyDoesNotCountAsAnswer() {
        assertTrue(
            listOf(
                msg(History.Role.USER, "안녕"),
                msg(History.Role.ASSISTANT, ""),
            ).hasUnansweredUserTurn(),
        )
    }

    @Test
    fun previousAnswerDoesNotCoverANewUserTurn() {
        assertTrue(
            listOf(
                msg(History.Role.USER, "하나"),
                msg(History.Role.ASSISTANT, "답"),
                msg(History.Role.USER, "둘"),
            ).hasUnansweredUserTurn(),
        )
    }

    @Test
    fun executingStatusRowSuppressesTheUnansweredAffordance() {
        // A foreign-turn watch row narrates a LIVE run — "응답이 비었습니다 /
        // 다시 생성" beside it contradicts the row and would double-send.
        val watching = listOf(
            msg(History.Role.USER, "보고서 정리해줘"),
            msg(History.Role.TOOL_EXECUTING, "foreign-turn"),
        )
        assertFalse(watching.silentlyUnanswered())
        // Same shape without the narration row IS the silent-death case.
        assertTrue(listOf(msg(History.Role.USER, "보고서 정리해줘")).silentlyUnanswered())
    }
}
