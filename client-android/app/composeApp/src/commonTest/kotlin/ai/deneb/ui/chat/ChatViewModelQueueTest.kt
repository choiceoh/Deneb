package ai.deneb.ui.chat

import ai.deneb.data.TaskScheduler
import ai.deneb.testutil.FakeDataRepository
import app.cash.turbine.test
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The 응답 중 전송 대기열 (#3026) defect family: messages queued while a reply is
 * streaming must (a) never auto-fire into a different conversation after a session
 * switch / new chat, (b) never auto-fire after a turn that FAILED — including
 * failures the gateway client surfaces as an ⚠️ bubble while returning normally,
 * and (c) never restore programmatic (work-feed / UI-callback) prompt text into
 * the input box.
 *
 * Harness note: do NOT call `scheduler.advanceUntilIdle()` while subscribed to
 * `viewModel.state` — the state combine samples chatHistory on a 64ms ticker
 * (STREAM_HISTORY_SAMPLE_MS) that perpetually reschedules itself, so the scheduler
 * never goes idle and the call hangs until runTest's 60s timeout. All ViewModel
 * actions here run synchronously (unconfined background dispatcher); awaitItem()
 * loops advance virtual time on their own.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ChatViewModelQueueTest {

    private val testDispatcher = StandardTestDispatcher()
    private val unconfinedDispatcher = UnconfinedTestDispatcher()
    private lateinit var fakeRepository: FakeDataRepository

    @BeforeTest
    fun setup() {
        Dispatchers.setMain(testDispatcher)
        fakeRepository = FakeDataRepository()
    }

    @AfterTest
    fun tearDown() {
        Dispatchers.resetMain()
    }

    private fun createViewModel(): ChatViewModel {
        val noOpScheduler = TaskScheduler(fakeRepository, enabled = false)
        return ChatViewModel(fakeRepository, noOpScheduler, unconfinedDispatcher)
    }

    @Test
    fun `message queued during streaming auto-sends after a successful turn`() = runTest {
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("first")
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)

            // Typed send while streaming: queued client-side, not sent to the repo.
            loadingState.actions.ask("queued follow-up")
            assertEquals(1, fakeRepository.askCalls.size)

            // Turn completes successfully → the queue drains automatically.
            fakeRepository.askGate = null
            gate.complete(Unit)

            var doneState: ChatUiState
            do {
                doneState = awaitItem()
            } while (doneState.isLoading || doneState.pendingQuestions.isNotEmpty() || fakeRepository.askCalls.size < 2)

            assertEquals(listOf("first", "queued follow-up"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `session switch clears the queue and never fires it into the next conversation`() = runTest {
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("first")
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)

            loadingState.actions.ask("queued follow-up")
            var queuedState: ChatUiState
            do {
                queuedState = awaitItem()
            } while (queuedState.pendingQuestions.isEmpty())

            // Switching conversation cancels the running turn (a rethrown
            // CancellationException skips the catch-side queue fold) — the switch
            // itself must clear the queue and hand the text back via failedInput.
            queuedState.actions.loadConversation("some-other-conversation")

            var switchedState: ChatUiState
            do {
                switchedState = awaitItem()
            } while (switchedState.pendingQuestions.isNotEmpty() || switchedState.isLoading)
            assertEquals("queued follow-up", switchedState.failedInput)
            assertEquals(1, fakeRepository.askCalls.size)

            // A later successful turn in the new conversation must NOT auto-send
            // the stale queue.
            fakeRepository.askGate = null
            gate.complete(Unit)
            switchedState.actions.ask("new conversation message")

            var doneState: ChatUiState
            do {
                doneState = awaitItem()
            } while (doneState.isLoading)

            assertEquals(
                listOf("first", "new conversation message"),
                fakeRepository.askCalls.map { it.first },
            )
            assertTrue(doneState.pendingQuestions.isEmpty())
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `startNewChat clears the queue and restores user-typed text`() = runTest {
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("first")
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)

            loadingState.actions.ask("queued follow-up")
            var queuedState: ChatUiState
            do {
                queuedState = awaitItem()
            } while (queuedState.pendingQuestions.isEmpty())

            queuedState.actions.startNewChat()

            var switchedState: ChatUiState
            do {
                switchedState = awaitItem()
            } while (switchedState.pendingQuestions.isNotEmpty() || switchedState.isLoading)
            assertEquals("queued follow-up", switchedState.failedInput)
            assertEquals(1, fakeRepository.askCalls.size)

            gate.complete(Unit)
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `queue folds into failedInput instead of auto-sending after a failed-bubble turn`() = runTest {
        // askResult=false mimics DenebGatewayClient surfacing a gateway/stream
        // failure as an ⚠️ reply bubble while returning normally (no exception).
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        fakeRepository.askResult = false
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("first")
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)

            loadingState.actions.ask("queued follow-up")
            var queuedState: ChatUiState
            do {
                queuedState = awaitItem()
            } while (queuedState.pendingQuestions.isEmpty())

            fakeRepository.askGate = null
            gate.complete(Unit)

            var doneState: ChatUiState
            do {
                doneState = awaitItem()
            } while (doneState.isLoading || doneState.pendingQuestions.isNotEmpty())

            // 실패 턴 뒤 자동전송 금지: the queued message went back to the input.
            assertEquals("queued follow-up", doneState.failedInput)
            assertEquals(listOf("first"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `programmatic prompt queued during streaming is dropped instead of restored`() = runTest {
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("first")
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)

            // A UI-callback with no source submission goes through the programmatic
            // askInternal path (restoreText = null) — same shape as the work-feed
            // card prompts. It queues, but tagged non-restorable.
            loadingState.actions.submitUiCallback("card_action", emptyMap())
            var queuedState: ChatUiState
            do {
                queuedState = awaitItem()
            } while (queuedState.pendingQuestions.isEmpty())
            assertFalse(queuedState.pendingQuestions.single().restoreToInput)

            // Stopping the turn folds the queue back — programmatic text must NOT
            // land in the input box.
            queuedState.actions.cancel()

            var stoppedState: ChatUiState
            do {
                stoppedState = awaitItem()
            } while (stoppedState.pendingQuestions.isNotEmpty() || stoppedState.isLoading)
            assertNull(stoppedState.failedInput)

            gate.complete(Unit)
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `text follow-up during streaming steers instead of queueing`() = runTest {
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        fakeRepository.steerResult = true
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("first")
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)

            loadingState.actions.ask("내일 말고 모레")
            var steeredState: ChatUiState
            do {
                steeredState = awaitItem()
            } while (steeredState.lastSteerNote == null)

            assertEquals("내일 말고 모레", steeredState.lastSteerNote)
            assertTrue(steeredState.pendingQuestions.isEmpty())
            assertEquals(listOf("내일 말고 모레"), fakeRepository.steerCalls)
            assertEquals(1, fakeRepository.askCalls.size)
            assertTrue(
                fakeRepository.chatHistory.value.any {
                    it.role == History.Role.USER && it.content == "내일 말고 모레"
                },
            )

            fakeRepository.askGate = null
            gate.complete(Unit)
            var doneState: ChatUiState
            do {
                doneState = awaitItem()
            } while (doneState.isLoading)
            assertNull(doneState.lastSteerNote)
            assertEquals(1, fakeRepository.askCalls.size)
            cancelAndIgnoreRemainingEvents()
        }
    }
}
