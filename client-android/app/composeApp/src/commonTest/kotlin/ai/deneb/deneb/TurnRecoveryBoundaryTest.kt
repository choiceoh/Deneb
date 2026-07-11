package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs

class TurnRecoveryBoundaryTest {

    private fun row(
        role: History.Role,
        content: String,
        thinking: Boolean = false,
        status: Boolean = false,
    ) = History(
        role = role,
        content = content,
        isThinking = thinking,
        isStatusMessage = status,
    )

    private fun user(content: String) = row(History.Role.USER, content)
    private fun assistant(content: String, thinking: Boolean = false, status: Boolean = false) = row(History.Role.ASSISTANT, content, thinking, status)

    @Test
    fun emptyTranscriptMeansMessageDidNotArrive() {
        assertIs<TurnProbe.NotArrived>(probeTranscriptForTurn(emptyList(), "question"))
    }

    @Test
    fun blankSentTextNeverAnchorsToBlankTranscriptRows() {
        val transcript = listOf(user("   "), assistant("unrelated answer"))

        assertIs<TurnProbe.NotArrived>(probeTranscriptForTurn(transcript, ""))
        assertIs<TurnProbe.NotArrived>(probeTranscriptForTurn(transcript, " \n\t "))
    }

    @Test
    fun exactUserMessageWithoutReplyIsStillRunning() {
        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(listOf(user("question")), "question"))
    }

    @Test
    fun surroundingWhitespaceIsIgnoredOnBothSentAndStoredText() {
        val transcript = listOf(user(" \n question \t"), assistant("answer"))

        assertEquals(TurnProbe.Answered("answer"), probeTranscriptForTurn(transcript, "  question\n"))
    }

    @Test
    fun internalWhitespaceDifferencesDoNotMatch() {
        val transcript = listOf(user("two  spaces"), assistant("answer"))

        assertIs<TurnProbe.NotArrived>(probeTranscriptForTurn(transcript, "two spaces"))
    }

    @Test
    fun matchingIsCaseSensitiveAndUnicodeExact() {
        val transcript = listOf(user("한글 Question"), assistant("answer"))

        assertIs<TurnProbe.NotArrived>(probeTranscriptForTurn(transcript, "한글 question"))
        assertEquals(TurnProbe.Answered("answer"), probeTranscriptForTurn(transcript, "한글 Question"))
    }

    @Test
    fun assistantBeforeSentMessageNeverCountsAsReply() {
        val transcript = listOf(assistant("old answer"), user("question"))

        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun laterUnrelatedUserDoesNotHideAnswerToAnchoredTurn() {
        val transcript = listOf(user("question"), assistant("answer"), user("follow-up"))

        assertEquals(TurnProbe.Answered("answer"), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun newestDuplicateUserMessageIsTheOnlyAnchor() {
        val transcript = listOf(
            user("same"),
            assistant("old answer"),
            user("same"),
        )

        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(transcript, "same"))
    }

    @Test
    fun newestDuplicateWithReplyReturnsOnlyNewReply() {
        val transcript = listOf(
            user("same"),
            assistant("old answer"),
            user("same"),
            assistant("new answer"),
        )

        assertEquals(TurnProbe.Answered("new answer"), probeTranscriptForTurn(transcript, "same"))
    }

    @Test
    fun finalNonBlankAssistantWinsAcrossToolLoop() {
        val transcript = listOf(
            user("question"),
            assistant("planning"),
            row(History.Role.TOOL_EXECUTING, "searching"),
            row(History.Role.TOOL, "tool result"),
            assistant("intermediate"),
            row(History.Role.TOOL, "second result"),
            assistant("final answer"),
        )

        assertEquals(TurnProbe.Answered("final answer"), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun toolRowsNeverCountAsAssistantReply() {
        val transcript = listOf(
            user("question"),
            row(History.Role.TOOL_EXECUTING, "running"),
            row(History.Role.TOOL, "result"),
        )

        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun thinkingAssistantRowsNeverResolveRecovery() {
        val transcript = listOf(user("question"), assistant("private reasoning", thinking = true))

        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun statusAssistantRowsNeverResolveRecovery() {
        val transcript = listOf(user("question"), assistant("result review in progress", status = true))

        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun visibleAssistantAfterThinkingAndStatusRowsResolvesNormally() {
        val transcript = listOf(
            user("question"),
            assistant("thinking", thinking = true),
            assistant("reviewing", status = true),
            assistant("visible answer"),
        )

        assertEquals(TurnProbe.Answered("visible answer"), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun trailingThinkingRowDoesNotReplaceVisibleAnswer() {
        val transcript = listOf(
            user("question"),
            assistant("visible answer"),
            assistant("late thought", thinking = true),
        )

        assertEquals(TurnProbe.Answered("visible answer"), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun trailingStatusRowDoesNotReplaceVisibleAnswer() {
        val transcript = listOf(
            user("question"),
            assistant("visible answer"),
            assistant("continuity", status = true),
        )

        assertEquals(TurnProbe.Answered("visible answer"), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun whitespaceOnlyAssistantRowsAreIgnored() {
        val transcript = listOf(user("question"), assistant(""), assistant(" \n\t "))

        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun returnedAnswerPreservesWhitespaceAndFormatting() {
        val answer = "  # Heading\n\nbody  \n"
        val transcript = listOf(user("question"), assistant(answer))

        assertEquals(TurnProbe.Answered(answer), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun emptyEarlierAnswerDoesNotBlockLaterVisibleAnswer() {
        val transcript = listOf(user("question"), assistant(""), assistant("answer"))

        assertEquals(TurnProbe.Answered("answer"), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun matchingUserRoleMustBeExact() {
        val transcript = listOf(
            assistant("question"),
            row(History.Role.TOOL, "question"),
            row(History.Role.TOOL_EXECUTING, "question"),
            assistant("answer"),
        )

        assertIs<TurnProbe.NotArrived>(probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun userRowsAfterAnchorAreNotMistakenForAnswers() {
        val transcript = listOf(user("question"), user("another"), user("third"))

        assertIs<TurnProbe.StillRunning>(probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun assistantReplyAfterOtherUserRowsStillCountsForOriginalAnchor() {
        val transcript = listOf(user("question"), user("another"), assistant("answer"))

        assertEquals(TurnProbe.Answered("answer"), probeTranscriptForTurn(transcript, "question"))
    }

    @Test
    fun nullCharactersAndEmojiAreComparedAsOrdinaryContent() {
        val sent = "question\u0000🚀"
        val transcript = listOf(user(sent), assistant("answer\u0000✅"))

        assertEquals(TurnProbe.Answered("answer\u0000✅"), probeTranscriptForTurn(transcript, sent))
    }

    @Test
    fun veryLargeSentTextMatchesWithoutTruncation() {
        val sent = "한글🚀".repeat(50_000)
        val transcript = listOf(user(sent), assistant("done"))

        assertEquals(TurnProbe.Answered("done"), probeTranscriptForTurn(transcript, sent))
    }

    @Test
    fun veryLargeTranscriptUsesNewestMatchingTurn() {
        val rows = buildList {
            repeat(2_000) { index ->
                add(user("question-$index"))
                add(assistant("answer-$index"))
            }
            add(user("target"))
            add(assistant("target answer"))
        }

        assertEquals(TurnProbe.Answered("target answer"), probeTranscriptForTurn(rows, "target"))
    }

    @Test
    fun answeredProbeDataClassHasValueEquality() {
        assertEquals(TurnProbe.Answered("x"), TurnProbe.Answered("x"))
    }

    @Test
    fun singletonProbeOutcomesRemainStable() {
        assertEquals(TurnProbe.NotArrived, probeTranscriptForTurn(emptyList(), "x"))
        assertEquals(TurnProbe.StillRunning, probeTranscriptForTurn(listOf(user("x")), "x"))
    }
}
