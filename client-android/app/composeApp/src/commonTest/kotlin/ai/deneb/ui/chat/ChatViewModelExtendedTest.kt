package ai.deneb.ui.chat

import ai.deneb.data.TaskScheduler
import ai.deneb.testutil.FakeDataRepository
import androidx.lifecycle.viewModelScope
import app.cash.turbine.test
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
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

@OptIn(ExperimentalCoroutinesApi::class)
class ChatViewModelExtendedTest {

    private val testDispatcher = StandardTestDispatcher()
    private val unconfinedDispatcher = UnconfinedTestDispatcher()
    private val viewModels = mutableListOf<ChatViewModel>()
    private lateinit var fakeRepository: FakeDataRepository

    @BeforeTest
    fun setup() {
        Dispatchers.setMain(testDispatcher)
        fakeRepository = FakeDataRepository()
    }

    @AfterTest
    fun tearDown() {
        clearViewModels()
        Dispatchers.resetMain()
    }

    private fun clearViewModels() {
        viewModels.forEach { it.viewModelScope.cancel() }
        viewModels.clear()
    }

    private fun runViewModelTest(block: suspend TestScope.() -> Unit) = runTest {
        try {
            block()
        } finally {
            // runTest checks its scheduler for unfinished work before @AfterTest
            // runs, so ViewModel scopes must be closed inside the test body. Drain
            // the cancellation continuations as well. Do not use advanceUntilIdle
            // here: stateIn's WhileSubscribed/sample pipeline deliberately keeps
            // scheduling while a Turbine subscriber is active.
            clearViewModels()
            testScheduler.runCurrent()
        }
    }

    private fun createViewModel(): ChatViewModel {
        val noOpScheduler = TaskScheduler(fakeRepository, enabled = false)
        return ChatViewModel(fakeRepository, noOpScheduler, unconfinedDispatcher)
            .also(viewModels::add)
    }

    // ---- Concurrent ask prevention ----

    @Test
    fun `concurrent ask is ignored while a previous ask is still loading`() = runViewModelTest {
        // Gate the first ask so it stays in flight
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("first")
            // First ask is now suspended at the gate, isLoading should be true
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)
            assertTrue(loadingState.isLoading)
            assertEquals(1, fakeRepository.askCalls.size)

            // Second ask should be ignored entirely while loading
            loadingState.actions.ask("second")
            testDispatcher.scheduler.runCurrent()
            // No new ask call recorded
            assertEquals(1, fakeRepository.askCalls.size)

            // Release the gate so the first ask completes
            fakeRepository.askGate = null
            gate.complete(Unit)
            cancelAndIgnoreRemainingEvents()
        }
    }

    // ---- startNewChat ----

    @Test
    fun `startNewChat clears history error and isLoading`() = runViewModelTest {
        // Pre-populate some history
        fakeRepository.chatHistory.value = listOf(
            History(role = History.Role.USER, content = "Hi"),
            History(role = History.Role.ASSISTANT, content = "Hello"),
        )
        val viewModel = createViewModel()

        viewModel.state.test {
            // Wait for initial state to surface the pre-populated history
            var stateWithHistory: ChatUiState
            do {
                stateWithHistory = awaitItem()
            } while (stateWithHistory.history.isEmpty())
            assertEquals(2, stateWithHistory.history.size)

            stateWithHistory.actions.startNewChat()
            testDispatcher.scheduler.runCurrent()

            var clearedState: ChatUiState
            do {
                clearedState = awaitItem()
            } while (clearedState.history.isNotEmpty())
            assertTrue(clearedState.history.isEmpty())
            assertNull(clearedState.error)
            assertFalse(clearedState.isLoading)
            cancelAndIgnoreRemainingEvents()
        }
    }

    // ---- regenerate ----

    @Test
    fun `regenerate drops the last exchange and re-asks the last user message`() = runViewModelTest {
        fakeRepository.chatHistory.value = listOf(
            History(role = History.Role.USER, content = "First"),
            History(role = History.Role.ASSISTANT, content = "Old answer"),
        )
        val viewModel = createViewModel()

        viewModel.state.test {
            // Wait for initial state to surface
            var initialState: ChatUiState
            do {
                initialState = awaitItem()
            } while (initialState.history.size < 2)

            initialState.actions.regenerate()
            testDispatcher.scheduler.runCurrent()

            // #1863: regenerate no longer uses the old `repository.regenerate(); ask(null)`
            // path — in gateway mode that did nothing (regenerate() wasn't overridden and
            // ask(null) was dropped at the empty-text guard). It now drops the last
            // user+assistant pair via popLastExchange() and re-asks the captured last-user
            // text through the normal ask() path. See ChatViewModel.regenerate().
            assertEquals(1, fakeRepository.askCalls.size)
            assertEquals("First", fakeRepository.askCalls.single().first)
            cancelAndIgnoreRemainingEvents()
        }
    }

    // ---- files ----

    @Test
    fun `files list defaults to empty`() = runViewModelTest {
        val viewModel = createViewModel()
        viewModel.state.test {
            val state = awaitItem()
            assertTrue(state.files.isEmpty())
            cancelAndIgnoreRemainingEvents()
        }
    }

    // ---- clearSnackbar ----

    @Test
    fun `clearSnackbar clears the snackbar message`() = runViewModelTest {
        val viewModel = createViewModel()
        viewModel.state.test {
            val initialState = awaitItem()
            assertNull(initialState.snackbarMessage)
            // Calling clearSnackbar when there's nothing to clear should be safe
            initialState.actions.clearSnackbar()
            testDispatcher.scheduler.runCurrent()
            cancelAndIgnoreRemainingEvents()
        }
    }

    // ---- cancel ----

    @Test
    fun `cancel stops an in-flight ask and resets isLoading`() = runViewModelTest {
        val gate = CompletableDeferred<Unit>()
        fakeRepository.askGate = gate
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("hello")

            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading)
            assertTrue(loadingState.isLoading)

            loadingState.actions.cancel()
            testDispatcher.scheduler.runCurrent()

            var cancelledState: ChatUiState
            do {
                cancelledState = awaitItem()
            } while (cancelledState.isLoading)
            assertFalse(cancelledState.isLoading)
            // Release gate so the cancelled coroutine can finish unwinding
            gate.complete(Unit)
            cancelAndIgnoreRemainingEvents()
        }
    }
}
