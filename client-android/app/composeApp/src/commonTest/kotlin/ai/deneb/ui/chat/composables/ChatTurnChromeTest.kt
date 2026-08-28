package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ChatTurnChromeTest {

    @Test
    fun waitingRowBeforeTokensAndDuringTools() {
        assertTrue(
            chatShowWaitingRow(
                isLoading = true,
                isResponseStreaming = false,
                hasExecutingTools = false,
                hasPendingUiSubmission = false,
            ),
        )
        assertTrue(
            chatShowWaitingRow(
                isLoading = true,
                isResponseStreaming = true,
                hasExecutingTools = true,
                hasPendingUiSubmission = false,
            ),
        )
    }

    @Test
    fun executingRowsShowEvenWithoutAnOwnSendInFlight() {
        // The foreign-turn watch adds a status row while isLoading is false (the
        // turn was started on another surface) — it must still render.
        assertTrue(
            chatShowWaitingRow(
                isLoading = false,
                isResponseStreaming = false,
                hasExecutingTools = true,
                hasPendingUiSubmission = false,
            ),
        )
    }

    @Test
    fun waitingRowHidesOnceTheAnswerIsOnScreen() {
        assertFalse(
            chatShowWaitingRow(
                isLoading = true,
                isResponseStreaming = true,
                hasExecutingTools = false,
                hasPendingUiSubmission = false,
            ),
        )
    }

    @Test
    fun waitingRowHidesForPendingUiAndWhenIdle() {
        assertFalse(
            chatShowWaitingRow(
                isLoading = true,
                isResponseStreaming = false,
                hasExecutingTools = false,
                hasPendingUiSubmission = true,
            ),
        )
        assertFalse(
            chatShowWaitingRow(
                isLoading = false,
                isResponseStreaming = false,
                hasExecutingTools = false,
                hasPendingUiSubmission = false,
            ),
        )
    }

    @Test
    fun speakOnlyTheFinishedAnswerOfThisTurn() {
        assertTrue(
            chatShouldSpeakFinishedReply(
                speechEnabled = true,
                loadingUserId = "u1",
                lastUserId = "u1",
                lastAssistantId = "a1",
                lastAssistantHasText = true,
                stoppedMessageId = null,
                hasError = false,
                unanswered = false,
            ),
        )
    }

    @Test
    fun speakSkipsCancelEmptyErrorAndOtherSession() {
        assertFalse(
            chatShouldSpeakFinishedReply(
                speechEnabled = true,
                loadingUserId = "u1",
                lastUserId = "u1",
                lastAssistantId = "a1",
                lastAssistantHasText = true,
                stoppedMessageId = "a1",
                hasError = false,
                unanswered = false,
            ),
        )
        assertFalse(
            chatShouldSpeakFinishedReply(
                speechEnabled = true,
                loadingUserId = "u1",
                lastUserId = "u1",
                lastAssistantId = "a1",
                lastAssistantHasText = false,
                stoppedMessageId = null,
                hasError = false,
                unanswered = true,
            ),
        )
        assertFalse(
            chatShouldSpeakFinishedReply(
                speechEnabled = true,
                loadingUserId = "u1",
                lastUserId = "u1",
                lastAssistantId = "a1",
                lastAssistantHasText = true,
                stoppedMessageId = null,
                hasError = true,
                unanswered = false,
            ),
        )
        assertFalse(
            chatShouldSpeakFinishedReply(
                speechEnabled = true,
                loadingUserId = "u-old",
                lastUserId = "u-new",
                lastAssistantId = "a1",
                lastAssistantHasText = true,
                stoppedMessageId = null,
                hasError = false,
                unanswered = false,
            ),
        )
        assertFalse(
            chatShouldSpeakFinishedReply(
                speechEnabled = false,
                loadingUserId = "u1",
                lastUserId = "u1",
                lastAssistantId = "a1",
                lastAssistantHasText = true,
                stoppedMessageId = null,
                hasError = false,
                unanswered = false,
            ),
        )
    }
}
