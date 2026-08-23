package ai.deneb.ui.chat

import ai.deneb.data.DataRepository
import ai.deneb.data.TaskScheduler
import ai.deneb.testutil.FakeDataRepository
import app.cash.turbine.test
import io.github.vinceglb.filekit.PlatformFile
import io.github.vinceglb.filekit.delete
import io.github.vinceglb.filekit.writeString
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
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

    private fun createViewModel(repository: DataRepository = fakeRepository): ChatViewModel {
        val noOpScheduler = TaskScheduler(repository, enabled = false)
        return ChatViewModel(repository, noOpScheduler, unconfinedDispatcher)
    }

    private fun blockingSteerRepository(
        started: CompletableDeferred<String>,
        result: CompletableDeferred<Boolean>,
        calls: MutableList<String>,
    ): DataRepository = object : DataRepository by fakeRepository {
        override suspend fun steer(note: String): Boolean {
            calls += note
            started.complete(note)
            return result.await()
        }
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
    fun `completion winning declined-steer enqueue cannot strand the follow-up`() = runTest {
        val viewModel = createViewModel()
        val pending = PendingQuestion(
            text = "기억은 짧게 정정해줘",
            restoreToInput = true,
        )
        val queueState = MutableStateFlow(
            viewModel.state.value.copy(
                isLoading = true,
                pendingQuestions = persistentListOf(),
            ),
        )
        var completionInjected = false

        val queued = queueState.compareAndUpdateIf(
            condition = { it.isLoading },
            transform = { running ->
                if (!completionInjected) {
                    completionInjected = true
                    // Exact lost-wakeup interleaving: completion observes an empty
                    // queue and clears loading after the declined-steer path took
                    // its snapshot but before that path commits the append.
                    queueState.value = running.copy(
                        isLoading = false,
                        pendingQuestions = persistentListOf(),
                    )
                }
                running.copy(
                    pendingQuestions = (running.pendingQuestions + pending).toImmutableList(),
                )
            },
        )

        assertFalse(queued)
        assertFalse(queueState.value.isLoading)
        assertTrue(queueState.value.pendingQuestions.isEmpty())
    }

    @Test
    fun `steer RPCs preserve typed submit order when responses would finish out of order`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val secondStarted = CompletableDeferred<Unit>()
        val thirdStarted = CompletableDeferred<Unit>()
        val secondResult = CompletableDeferred<Boolean>()
        val thirdResult = CompletableDeferred<Boolean>()
        fakeRepository.askGate = turnGate
        val repository = object : DataRepository by fakeRepository {
            override suspend fun steer(note: String): Boolean = when (note) {
                "B" -> {
                    secondStarted.complete(Unit)
                    secondResult.await()
                }

                "C" -> {
                    thirdStarted.complete(Unit)
                    thirdResult.await()
                }

                else -> false
            }
        }
        val viewModel = createViewModel(repository)

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("A")
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)

            loadingState.actions.ask("B")
            loadingState.actions.ask("C")
            secondStarted.await()

            // C is configured to decline immediately, but its RPC must remain
            // behind B's accepted response.
            thirdResult.complete(false)
            assertFalse(thirdStarted.isCompleted)
            secondResult.complete(true)
            thirdStarted.await()

            var queuedState: ChatUiState
            do {
                queuedState = awaitItem()
            } while (queuedState.pendingQuestions.map { it.text } != listOf("C"))

            assertEquals("B", queuedState.lastSteerNote)
            queuedState.actions.cancel()
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `later accepted steer stays queued behind an earlier declined fallback`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val secondStarted = CompletableDeferred<Unit>()
        val thirdStarted = CompletableDeferred<Unit>()
        val secondResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val repository = object : DataRepository by fakeRepository {
            override suspend fun steer(note: String): Boolean {
                steerCalls += note
                return when (note) {
                    "B" -> {
                        secondStarted.complete(Unit)
                        secondResult.await()
                    }

                    "C" -> {
                        thirdStarted.complete(Unit)
                        true
                    }

                    else -> false
                }
            }
        }
        val viewModel = createViewModel(repository)

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            loading.actions.ask("C")
            secondStarted.await()
            secondResult.complete(false)

            var queued: ChatUiState
            do {
                queued = awaitItem()
            } while (queued.pendingQuestions.map { it.text } != listOf("B", "C"))

            assertFalse(thirdStarted.isCompleted)
            assertEquals(listOf("B"), steerCalls)
            assertNull(queued.lastSteerNote)
            queued.actions.cancel()
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `previous completion during declined steers starts only the first fallback and queues the next`() = runTest {
        val firstAskGate = CompletableDeferred<Unit>()
        val secondAskGate = CompletableDeferred<Unit>()
        val secondStarted = CompletableDeferred<Unit>()
        val thirdStarted = CompletableDeferred<Unit>()
        val secondResult = CompletableDeferred<Boolean>()
        val thirdResult = CompletableDeferred<Boolean>()
        fakeRepository.askGate = firstAskGate
        val repository = object : DataRepository by fakeRepository {
            override suspend fun steer(note: String): Boolean = when (note) {
                "B" -> {
                    secondStarted.complete(Unit)
                    secondResult.await()
                }

                "C" -> {
                    thirdStarted.complete(Unit)
                    thirdResult.await()
                }

                else -> false
            }
        }
        val viewModel = createViewModel(repository)

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            loading.actions.ask("C")
            secondStarted.await()

            // A completes before B's steer is declined. B must claim loading
            // synchronously, so C cannot start a second fallback ask beside it.
            fakeRepository.askGate = secondAskGate
            firstAskGate.complete(Unit)
            assertTrue(viewModel.state.value.isLoading)

            secondResult.complete(false)

            var queued: ChatUiState
            do {
                queued = awaitItem()
            } while (queued.pendingQuestions.map { it.text } != listOf("C"))

            assertFalse(thirdStarted.isCompleted)
            assertEquals(listOf("A", "B"), fakeRepository.askCalls.map { it.first })
            secondAskGate.complete(Unit)
            var finished: ChatUiState
            do {
                finished = awaitItem()
            } while (finished.isLoading || finished.pendingQuestions.isNotEmpty() || fakeRepository.askCalls.size < 3)
            assertEquals(listOf("A", "B", "C"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `successful turn atomically hands the loading claim to queued work before a later submit`() = runTest {
        val firstGate = CompletableDeferred<Unit>()
        val secondGate = CompletableDeferred<Unit>()
        fakeRepository.askGate = firstGate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            // B declines steer and occupies the first after-turn queue slot.
            loading.actions.ask("B")
            var queued: ChatUiState
            do {
                queued = awaitItem()
            } while (queued.pendingQuestions.map { it.text } != listOf("B"))

            fakeRepository.askGate = secondGate
            viewModel.beforeQueuedTurnStart = { claimed ->
                viewModel.beforeQueuedTurnStart = null
                // Inject D at the old idle-publish/drain boundary. B must already
                // own loading, so D cannot start a turn ahead of it.
                assertTrue(claimed.isLoading)
                assertTrue(claimed.pendingQuestions.none { it.text == "B" })
                claimed.actions.ask("D")
            }
            firstGate.complete(Unit)

            var secondRunning: ChatUiState
            do {
                secondRunning = awaitItem()
            } while (
                fakeRepository.askCalls.map { it.first } != listOf("A", "B") ||
                secondRunning.pendingQuestions.map { it.text } != listOf("D")
            )
            assertTrue(secondRunning.isLoading)

            fakeRepository.askGate = null
            secondGate.complete(Unit)
            var done: ChatUiState
            do {
                done = awaitItem()
            } while (done.isLoading || fakeRepository.askCalls.size < 3)

            assertEquals(listOf("A", "B", "D"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `cancel at the claimed queue handoff restores the unsent follow-up`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            fakeRepository.askGate = null
            turnGate.complete(Unit)
            assertTrue(viewModel.state.value.isLoading)

            viewModel.beforeQueuedTurnStart = {
                viewModel.beforeQueuedTurnStart = null
                viewModel.state.value.actions.cancel()
            }
            steerResult.complete(false)

            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            assertEquals("B", restored.failedInput)
            assertEquals(listOf("B"), steerCalls)
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            assertFalse(restored.isLoading)
            assertTrue(restored.pendingQuestions.isEmpty())
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `steer admission before mutex ownership keeps success handoff busy`() = runTest {
        val firstGate = CompletableDeferred<Unit>()
        val secondGate = CompletableDeferred<Unit>()
        fakeRepository.askGate = firstGate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            fakeRepository.askGate = secondGate
            viewModel.beforeSteerLaunch = {
                viewModel.beforeSteerLaunch = null
                // A completes after B owns its ledger id but before B owns a
                // steerMutex ticket. The handoff must wait on B's head phase.
                firstGate.complete(Unit)
            }
            loading.actions.ask("B")

            var secondRunning: ChatUiState
            do {
                secondRunning = awaitItem()
            } while (fakeRepository.askCalls.map { it.first } != listOf("A", "B"))
            assertTrue(secondRunning.isLoading)
            assertTrue(secondRunning.pendingQuestions.isEmpty())

            fakeRepository.askGate = null
            secondGate.complete(Unit)
            var done: ChatUiState
            do {
                done = awaitItem()
            } while (done.isLoading)

            assertEquals(listOf("A", "B"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `attachment submit after successful idle claim starts directly without stranding`() = runTest {
        val firstGate = CompletableDeferred<Unit>()
        val secondGate = CompletableDeferred<Unit>()
        val attachment = PlatformFile("/tmp/deneb-success-seam-P.txt")
        attachment.writeString("P")
        fakeRepository.askGate = firstGate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.addFile(attachment)
            testScheduler.runCurrent()
            var staged: ChatUiState
            do {
                staged = awaitItem()
            } while (staged.files != persistentListOf(attachment))

            fakeRepository.askGate = secondGate
            viewModel.afterSuccessfulHandoffClaim = {
                viewModel.afterSuccessfulHandoffClaim = null
                staged.actions.ask("P")
            }
            firstGate.complete(Unit)

            var secondRunning: ChatUiState
            do {
                secondRunning = awaitItem()
            } while (fakeRepository.askCalls.map { it.first } != listOf("A", "P"))
            assertTrue(secondRunning.isLoading)
            assertTrue(secondRunning.pendingQuestions.isEmpty())
            assertEquals(listOf(attachment), fakeRepository.askCalls[1].second)

            fakeRepository.askGate = null
            secondGate.complete(Unit)
            var done: ChatUiState
            do {
                done = awaitItem()
            } while (done.isLoading)

            attachment.delete()
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
            // itself must clear the queue without leaking it into the next composer.
            queuedState.actions.loadConversation("some-other-conversation")

            var switchedState: ChatUiState
            do {
                switchedState = awaitItem()
            } while (switchedState.pendingQuestions.isNotEmpty() || switchedState.isLoading)
            assertNull(switchedState.failedInput)
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
    fun `session switch discards a successful steer response from the previous generation`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            // The fake deliberately leaves the nullable session id unchanged for
            // an unknown row. The generation still has to invalidate the RPC.
            loading.actions.loadConversation("missing-conversation")
            var switched: ChatUiState
            do {
                switched = awaitItem()
            } while (switched.isLoading)

            fakeRepository.askGate = null
            steerResult.complete(true)
            testScheduler.runCurrent()

            assertEquals(listOf("B"), steerCalls)
            assertNull(viewModel.state.value.lastSteerNote)
            assertNull(viewModel.state.value.failedInput)
            assertTrue(viewModel.state.value.pendingQuestions.isEmpty())
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `startNewChat clears the queue without leaking it into the new conversation`() = runTest {
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
            assertNull(switchedState.failedInput)
            assertEquals(1, fakeRepository.askCalls.size)

            gate.complete(Unit)
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `new chat discards a declined steer response from the previous generation`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            loading.actions.startNewChat()
            var fresh: ChatUiState
            do {
                fresh = awaitItem()
            } while (fresh.isLoading)

            fakeRepository.askGate = null
            steerResult.complete(false)
            testScheduler.runCurrent()

            assertEquals(listOf("B"), steerCalls)
            assertNull(viewModel.state.value.lastSteerNote)
            assertNull(viewModel.state.value.failedInput)
            assertTrue(viewModel.state.value.pendingQuestions.isEmpty())
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `cancel discards a successful in-flight steer response`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            loading.actions.cancel()
            var stopped: ChatUiState
            do {
                stopped = awaitItem()
            } while (stopped.isLoading)

            fakeRepository.askGate = null
            steerResult.complete(true)
            testScheduler.runCurrent()

            assertEquals(listOf("B"), steerCalls)
            assertNull(viewModel.state.value.lastSteerNote)
            assertNull(viewModel.state.value.failedInput)
            assertTrue(viewModel.state.value.pendingQuestions.isEmpty())
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `cancel after a successful steer completion clears its acknowledgement atomically`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            steerResult.complete(true)
            var accepted: ChatUiState
            do {
                accepted = awaitItem()
            } while (accepted.lastSteerNote != "B")

            accepted.actions.cancel()
            var stopped: ChatUiState
            do {
                stopped = awaitItem()
            } while (stopped.isLoading || stopped.lastSteerNote != null)
            testScheduler.runCurrent()

            assertNull(viewModel.state.value.lastSteerNote)
            assertNull(viewModel.state.value.failedInput)
            assertEquals(listOf("B"), steerCalls)
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `cancel restores a declined in-flight steer without starting a new turn`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            loading.actions.cancel()
            var stopped: ChatUiState
            do {
                stopped = awaitItem()
            } while (stopped.isLoading)

            fakeRepository.askGate = null
            steerResult.complete(false)
            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            assertEquals("B", restored.failedInput)
            assertEquals(listOf("B"), steerCalls)
            assertNull(restored.lastSteerNote)
            assertTrue(restored.pendingQuestions.isEmpty())
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `cancel restores in-flight and mutex-waiting steers once in submit order`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val secondStarted = CompletableDeferred<Unit>()
        val secondResult = CompletableDeferred<Boolean>()
        val thirdStarted = CompletableDeferred<Unit>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val repository = object : DataRepository by fakeRepository {
            override suspend fun steer(note: String): Boolean {
                steerCalls += note
                return when (note) {
                    "B" -> {
                        secondStarted.complete(Unit)
                        secondResult.await()
                    }

                    "C" -> {
                        thirdStarted.complete(Unit)
                        true
                    }

                    else -> false
                }
            }
        }
        val viewModel = createViewModel(repository)

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            loading.actions.ask("C")
            secondStarted.await()
            assertFalse(thirdStarted.isCompleted)
            loading.actions.cancel()
            var stopped: ChatUiState
            do {
                stopped = awaitItem()
            } while (stopped.isLoading)

            fakeRepository.askGate = null
            secondResult.complete(false)
            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            // The first non-null restore is already complete. Publishing B first
            // and then B+C would make ChatModeScreen append B twice.
            assertEquals("B\n\nC", restored.failedInput)
            assertFalse(thirdStarted.isCompleted)
            assertEquals(listOf("B"), steerCalls)
            assertNull(restored.lastSteerNote)
            assertTrue(restored.pendingQuestions.isEmpty())
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `two cancels merge unresolved steer generations once without losing waiters`() = runTest {
        val firstTurnGate = CompletableDeferred<Unit>()
        val secondTurnGate = CompletableDeferred<Unit>()
        val secondStarted = CompletableDeferred<Unit>()
        val secondResult = CompletableDeferred<Boolean>()
        val thirdStarted = CompletableDeferred<Unit>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = firstTurnGate
        val repository = object : DataRepository by fakeRepository {
            override suspend fun steer(note: String): Boolean {
                steerCalls += note
                return when (note) {
                    "B" -> {
                        secondStarted.complete(Unit)
                        secondResult.await()
                    }

                    "E" -> {
                        thirdStarted.complete(Unit)
                        true
                    }

                    else -> false
                }
            }
        }
        val viewModel = createViewModel(repository)

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var firstLoading: ChatUiState
            do {
                firstLoading = awaitItem()
            } while (!firstLoading.isLoading)
            firstLoading.actions.ask("B")
            secondStarted.await()

            firstLoading.actions.cancel()
            var firstStopped: ChatUiState
            do {
                firstStopped = awaitItem()
            } while (firstStopped.isLoading)
            assertNull(firstStopped.failedInput)

            fakeRepository.askGate = secondTurnGate
            firstStopped.actions.ask("D")
            var secondLoading: ChatUiState
            do {
                secondLoading = awaitItem()
            } while (!secondLoading.isLoading || fakeRepository.askCalls.size < 2)
            secondLoading.actions.ask("E")
            assertFalse(thirdStarted.isCompleted)

            secondLoading.actions.cancel()
            var secondStopped: ChatUiState
            do {
                secondStopped = awaitItem()
            } while (secondStopped.isLoading)
            assertNull(secondStopped.failedInput)

            secondResult.complete(false)
            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            // The first non-null publication is the complete, ingress-ordered
            // restore. ChatModeScreen must never observe B and then B+E.
            assertEquals("B\n\nE", restored.failedInput)
            assertFalse(thirdStarted.isCompleted)
            assertEquals(listOf("B"), steerCalls)
            assertEquals(listOf("A", "D"), fakeRepository.askCalls.map { it.first })
            assertTrue(restored.pendingQuestions.isEmpty())
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `cancel resolves a CancellationException steer and restores it without starting a turn`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val releaseSteer = CompletableDeferred<Unit>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        val repository = object : DataRepository by fakeRepository {
            override suspend fun steer(note: String): Boolean {
                steerCalls += note
                steerStarted.complete(note)
                releaseSteer.await()
                throw CancellationException("steer transport cancelled")
            }
        }
        val viewModel = createViewModel(repository)

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            loading.actions.cancel()
            var stopped: ChatUiState
            do {
                stopped = awaitItem()
            } while (stopped.isLoading)

            fakeRepository.askGate = null
            releaseSteer.complete(Unit)
            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            assertEquals("B", restored.failedInput)
            assertEquals(listOf("B"), steerCalls)
            assertNull(restored.lastSteerNote)
            assertTrue(restored.pendingQuestions.isEmpty())
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `declined text and later attachment preserve one FIFO in queue and cancel restore`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        val attachment = PlatformFile("/tmp/deneb-queue-P.txt")
        attachment.writeString("P")
        fakeRepository.askGate = turnGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)

            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())
            loading.actions.addFile(attachment)
            testScheduler.runCurrent()
            val staged = expectMostRecentItem()
            assertEquals(persistentListOf(attachment), staged.files)

            staged.actions.ask("P")
            var attachmentQueued: ChatUiState
            do {
                attachmentQueued = awaitItem()
            } while (attachmentQueued.pendingQuestions.map { it.text } != listOf("P"))
            assertEquals(listOf(attachment), attachmentQueued.pendingQuestions.single().files)

            steerResult.complete(false)
            var orderedQueue: ChatUiState
            do {
                orderedQueue = awaitItem()
            } while (orderedQueue.pendingQuestions.map { it.text } != listOf("B", "P"))

            orderedQueue.actions.cancel()
            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            assertEquals("B\n\nP", restored.failedInput)
            assertEquals(listOf("B"), steerCalls)
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            assertTrue(restored.pendingQuestions.isEmpty())
            attachment.delete()
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `successful A waits for earlier declined B before starting later attachment P`() = runTest {
        val firstGate = CompletableDeferred<Unit>()
        val secondGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        val attachment = PlatformFile("/tmp/deneb-hol-false-P.txt")
        attachment.writeString("P")
        fakeRepository.askGate = firstGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)
            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())

            loading.actions.addFile(attachment)
            testScheduler.runCurrent()
            var staged: ChatUiState
            do {
                staged = awaitItem()
            } while (staged.files != persistentListOf(attachment))
            staged.actions.ask("P")
            var attachmentQueued: ChatUiState
            do {
                attachmentQueued = awaitItem()
            } while (attachmentQueued.pendingQuestions.map { it.text } != listOf("P"))

            fakeRepository.askGate = secondGate
            firstGate.complete(Unit)
            testScheduler.runCurrent()
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            assertTrue(viewModel.state.value.isLoading)

            steerResult.complete(false)
            var secondRunning: ChatUiState
            do {
                secondRunning = awaitItem()
            } while (
                fakeRepository.askCalls.map { it.first } != listOf("A", "B") ||
                secondRunning.pendingQuestions.map { it.text } != listOf("P")
            )

            fakeRepository.askGate = null
            secondGate.complete(Unit)
            var done: ChatUiState
            do {
                done = awaitItem()
            } while (done.isLoading || fakeRepository.askCalls.size < 3)

            assertEquals(listOf("A", "B", "P"), fakeRepository.askCalls.map { it.first })
            assertEquals(listOf(attachment), fakeRepository.askCalls[2].second)
            assertEquals(listOf("B"), steerCalls)
            attachment.delete()
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `successful A waits for earlier accepted B before starting later attachment P`() = runTest {
        val firstGate = CompletableDeferred<Unit>()
        val secondGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        val attachment = PlatformFile("/tmp/deneb-hol-true-P.txt")
        attachment.writeString("P")
        fakeRepository.askGate = firstGate
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)
            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())

            loading.actions.addFile(attachment)
            testScheduler.runCurrent()
            var staged: ChatUiState
            do {
                staged = awaitItem()
            } while (staged.files != persistentListOf(attachment))
            staged.actions.ask("P")
            var attachmentQueued: ChatUiState
            do {
                attachmentQueued = awaitItem()
            } while (attachmentQueued.pendingQuestions.map { it.text } != listOf("P"))

            fakeRepository.askGate = secondGate
            firstGate.complete(Unit)
            testScheduler.runCurrent()
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })

            steerResult.complete(true)
            var attachmentRunning: ChatUiState
            do {
                attachmentRunning = awaitItem()
            } while (fakeRepository.askCalls.map { it.first } != listOf("A", "P"))
            assertTrue(attachmentRunning.isLoading)
            assertEquals(listOf(attachment), fakeRepository.askCalls[1].second)

            fakeRepository.askGate = null
            secondGate.complete(Unit)
            var done: ChatUiState
            do {
                done = awaitItem()
            } while (done.isLoading)

            assertEquals(listOf("B"), steerCalls)
            attachment.delete()
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `declined steer completing after a failed turn restores instead of auto-sending`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val steerResult = CompletableDeferred<Boolean>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        fakeRepository.askResult = false
        val viewModel = createViewModel(blockingSteerRepository(steerStarted, steerResult, steerCalls))

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)
            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())

            fakeRepository.askGate = null
            turnGate.complete(Unit)
            var failed: ChatUiState
            do {
                failed = awaitItem()
            } while (failed.isLoading)
            assertNull(failed.failedInput)

            steerResult.complete(false)
            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            assertEquals("B", restored.failedInput)
            assertEquals(listOf("B"), steerCalls)
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            assertTrue(restored.pendingQuestions.isEmpty())
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `exceptional steer completing after a failed turn restores instead of auto-sending`() = runTest {
        val turnGate = CompletableDeferred<Unit>()
        val steerStarted = CompletableDeferred<String>()
        val releaseSteer = CompletableDeferred<Unit>()
        val steerCalls = mutableListOf<String>()
        fakeRepository.askGate = turnGate
        fakeRepository.askResult = false
        val repository = object : DataRepository by fakeRepository {
            override suspend fun steer(note: String): Boolean {
                steerCalls += note
                steerStarted.complete(note)
                releaseSteer.await()
                throw CancellationException("late steer cancelled")
            }
        }
        val viewModel = createViewModel(repository)

        viewModel.state.test {
            val initial = awaitItem()
            initial.actions.ask("A")
            var loading: ChatUiState
            do {
                loading = awaitItem()
            } while (!loading.isLoading)
            loading.actions.ask("B")
            assertEquals("B", steerStarted.await())

            fakeRepository.askGate = null
            turnGate.complete(Unit)
            var failed: ChatUiState
            do {
                failed = awaitItem()
            } while (failed.isLoading)
            assertNull(failed.failedInput)

            releaseSteer.complete(Unit)
            var restored: ChatUiState
            do {
                restored = awaitItem()
            } while (restored.failedInput == null)

            assertEquals("B", restored.failedInput)
            assertEquals(listOf("B"), steerCalls)
            assertEquals(listOf("A"), fakeRepository.askCalls.map { it.first })
            assertTrue(restored.pendingQuestions.isEmpty())
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
