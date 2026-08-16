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
}
