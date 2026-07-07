package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs

class TurnRecoveryTest {

    private fun user(text: String) = History(role = History.Role.USER, content = text)
    private fun assistant(text: String) = History(role = History.Role.ASSISTANT, content = text)

    @Test
    fun answeredWhenAssistantFollowsSentMessage() {
        val probe = probeTranscriptForTurn(
            listOf(user("영상 요약해줘"), assistant("요약입니다.")),
            "영상 요약해줘",
        )
        assertEquals(TurnProbe.Answered("요약입니다."), probe)
    }

    @Test
    fun matchesLastDuplicateAndFinalAssistantText() {
        // The 2026-07-07 incident shape: an unanswered send re-sent verbatim.
        // The probe must anchor on the NEWEST copy and, for a multi-part turn,
        // resolve to the final assistant text after it.
        val probe = probeTranscriptForTurn(
            listOf(
                user("도구를 써서 왜 내용 안 알려주고"),
                assistant("첫 답변"),
                user("도구를 써서 왜 내용 안 알려주고"),
                assistant("중간 진행 텍스트"),
                assistant("최종 답변"),
            ),
            "도구를 써서 왜 내용 안 알려주고",
        )
        assertEquals(TurnProbe.Answered("최종 답변"), probe)
    }

    @Test
    fun stillRunningWhenSentMessageIsUnanswered() {
        val probe = probeTranscriptForTurn(
            listOf(assistant("이전 턴 답"), user("새 질문")),
            "새 질문",
        )
        assertIs<TurnProbe.StillRunning>(probe)
    }

    @Test
    fun blankAssistantRowsAreNotAnAnswer() {
        val probe = probeTranscriptForTurn(
            listOf(user("질문"), assistant("  ")),
            "질문",
        )
        assertIs<TurnProbe.StillRunning>(probe)
    }

    @Test
    fun notArrivedWhenTranscriptLacksTheMessage() {
        val probe = probeTranscriptForTurn(
            listOf(user("다른 메시지"), assistant("다른 답")),
            "전송한 메시지",
        )
        assertIs<TurnProbe.NotArrived>(probe)
    }

    @Test
    fun trimsWhitespaceWhenMatching() {
        val probe = probeTranscriptForTurn(
            listOf(user("질문입니다 ")),
            " 질문입니다",
        )
        assertIs<TurnProbe.StillRunning>(probe)
    }
}
