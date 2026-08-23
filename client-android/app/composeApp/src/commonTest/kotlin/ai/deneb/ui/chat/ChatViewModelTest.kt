package ai.deneb.ui.chat

import ai.deneb.data.TaskScheduler
import ai.deneb.network.AnthropicInsufficientCreditsException
import ai.deneb.network.AnthropicInvalidApiKeyException
import ai.deneb.network.AnthropicOverloadedException
import ai.deneb.network.AnthropicRateLimitExceededException
import ai.deneb.network.GeminiInvalidApiKeyException
import ai.deneb.network.GeminiRateLimitExceededException
import ai.deneb.network.GenericNetworkException
import ai.deneb.network.OpenAICompatibleInvalidApiKeyException
import ai.deneb.network.OpenAICompatibleRateLimitExceededException
import ai.deneb.testutil.FakeDataRepository
import androidx.lifecycle.viewModelScope
import app.cash.turbine.test
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
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class ChatViewModelTest {

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
            clearViewModels()
            testDispatcher.scheduler.runCurrent()
            testScheduler.runCurrent()
        }
    }

    private fun createViewModel(): ChatViewModel {
        val noOpScheduler = TaskScheduler(fakeRepository, enabled = false)
        return ChatViewModel(fakeRepository, noOpScheduler, unconfinedDispatcher)
            .also(viewModels::add)
    }

    @Test
    fun `restore runs off the main thread and flips isRestoring`() = runViewModelTest {
        // Isolated paused dispatcher so the launched restore coroutine doesn't run synchronously.
        val backgroundDispatcher = StandardTestDispatcher()
        val noOpScheduler = TaskScheduler(fakeRepository, enabled = false)
        val viewModel = ChatViewModel(fakeRepository, noOpScheduler, backgroundDispatcher)
            .also(viewModels::add)

        viewModel.state.test {
            // Restore hasn't run yet — initial state still has isRestoring=true.
            assertTrue(awaitItem().isRestoring)

            backgroundDispatcher.scheduler.runCurrent()
            testDispatcher.scheduler.runCurrent()

            assertFalse(awaitItem().isRestoring)
            cancelAndIgnoreRemainingEvents()
        }
        // This test deliberately gives the restore job its own paused scheduler.
        // Cancel the ViewModel, then execute its cancellation continuation on that
        // scheduler before runTest performs its unfinished-work check.
        clearViewModels()
        backgroundDispatcher.scheduler.runCurrent()
    }

    @Test
    fun `ask completes successfully and updates history`() = runViewModelTest {
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            assertFalse(initialState.isLoading)
            assertTrue(initialState.history.isEmpty())

            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            // Wait for completion - collect all states until we get a non-loading state with history
            var finalState: ChatUiState
            do {
                finalState = awaitItem()
            } while (finalState.isLoading || finalState.history.isEmpty())

            assertFalse(finalState.isLoading)
            assertTrue(finalState.history.isNotEmpty())
        }
    }

    @Test
    fun `successful ask adds messages to history`() = runViewModelTest {
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            assertTrue(initialState.history.isEmpty())

            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            // Wait for history to be populated
            var finalState: ChatUiState
            do {
                finalState = awaitItem()
            } while (finalState.history.isEmpty() || finalState.isLoading)

            assertEquals(2, finalState.history.size)
            assertEquals(History.Role.USER, finalState.history[0].role)
            assertEquals("Hello", finalState.history[0].content)
            assertEquals(History.Role.ASSISTANT, finalState.history[1].role)
        }
    }

    @Test
    fun `ask clears previous error`() = runViewModelTest {
        fakeRepository.askException = GenericNetworkException("First error")
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()

            // First call - will fail
            initialState.actions.ask("First")
            testDispatcher.scheduler.runCurrent()

            // Wait for error
            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)
            assertNotNull(errorState.error)

            // Clear exception and ask again
            fakeRepository.askException = null
            errorState.actions.ask("Second")
            testDispatcher.scheduler.runCurrent()

            // Wait for loading state which should have cleared error
            var loadingState: ChatUiState
            do {
                loadingState = awaitItem()
            } while (!loadingState.isLoading && loadingState.error != null)

            // Error should be cleared when loading starts
            assertNull(loadingState.error)

            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `failed ask with GeminiInvalidApiKeyException sets error`() = runViewModelTest {
        fakeRepository.askException = GeminiInvalidApiKeyException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `failed ask with GroqInvalidApiKeyException sets error`() = runViewModelTest {
        fakeRepository.askException = OpenAICompatibleInvalidApiKeyException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `failed ask with GeminiRateLimitExceededException sets error`() = runViewModelTest {
        fakeRepository.askException = GeminiRateLimitExceededException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `failed ask with GroqRateLimitExceededException sets error`() = runViewModelTest {
        fakeRepository.askException = OpenAICompatibleRateLimitExceededException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `failed ask with AnthropicInvalidApiKeyException sets error`() = runViewModelTest {
        fakeRepository.askException = AnthropicInvalidApiKeyException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `failed ask with AnthropicRateLimitExceededException sets error`() = runViewModelTest {
        fakeRepository.askException = AnthropicRateLimitExceededException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `failed ask with AnthropicOverloadedException sets error`() = runViewModelTest {
        fakeRepository.askException = AnthropicOverloadedException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `failed ask with AnthropicInsufficientCreditsException sets error`() = runViewModelTest {
        fakeRepository.askException = AnthropicInsufficientCreditsException()
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)

            assertNotNull(errorState.error)
            assertFalse(errorState.isLoading)
        }
    }

    @Test
    fun `clearHistory clears history and error`() = runViewModelTest {
        fakeRepository.askException = GenericNetworkException("Error")
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()

            // Trigger an error first
            initialState.actions.ask("Hello")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)
            assertNotNull(errorState.error)

            // Clear history
            errorState.actions.clearHistory()
            testDispatcher.scheduler.runCurrent()

            var clearedState: ChatUiState
            do {
                clearedState = awaitItem()
            } while (clearedState.error != null || clearedState.history.isNotEmpty())

            assertNull(clearedState.error)
            assertTrue(clearedState.history.isEmpty())
            assertEquals(1, fakeRepository.clearHistoryCalls)
        }
    }

    @Test
    fun `toggleSpeechOutput toggles isSpeechOutputEnabled`() = runViewModelTest {
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            assertFalse(initialState.isSpeechOutputEnabled)

            initialState.actions.toggleSpeechOutput()
            testDispatcher.scheduler.runCurrent()

            val enabledState = awaitItem()
            assertTrue(enabledState.isSpeechOutputEnabled)

            enabledState.actions.toggleSpeechOutput()
            testDispatcher.scheduler.runCurrent()

            val disabledState = awaitItem()
            assertFalse(disabledState.isSpeechOutputEnabled)
        }
    }

    @Test
    fun `setIsSpeaking updates speaking state`() = runViewModelTest {
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            assertFalse(initialState.isSpeaking)

            initialState.actions.setIsSpeaking(true, "content-123")
            testDispatcher.scheduler.runCurrent()

            val speakingState = awaitItem()
            assertTrue(speakingState.isSpeaking)
            assertEquals("content-123", speakingState.isSpeakingContentId)

            speakingState.actions.setIsSpeaking(false, "")
            testDispatcher.scheduler.runCurrent()

            val notSpeakingState = awaitItem()
            assertFalse(notSpeakingState.isSpeaking)
            // Content ID should be preserved when stopping
            assertEquals("content-123", notSpeakingState.isSpeakingContentId)
        }
    }

    @Test
    fun `error card action dismisses without resending`() = runViewModelTest {
        // The failed text is already restored into the composer (failedInput), so the
        // card's action must clear the banner and nothing else — resending here would
        // put the same message in the transcript twice. It used to call ask(null),
        // which askGateway drops on its empty-text guard: a button that did nothing.
        fakeRepository.askException = GenericNetworkException("boom")
        val viewModel = createViewModel()

        viewModel.state.test {
            val initialState = awaitItem()
            initialState.actions.ask("질문")
            testDispatcher.scheduler.runCurrent()

            var errorState: ChatUiState
            do {
                errorState = awaitItem()
            } while (errorState.error == null)
            val callsBefore = fakeRepository.askCalls.size

            errorState.actions.retry()
            testDispatcher.scheduler.runCurrent()

            var dismissed: ChatUiState
            do {
                dismissed = awaitItem()
            } while (dismissed.error != null)

            assertNull(dismissed.error)
            assertEquals(callsBefore, fakeRepository.askCalls.size)
        }
    }

    @Test
    fun `allowFileAttachment is true when repository supports it`() = runViewModelTest {
        fakeRepository.fileAttachmentSupported = true
        val viewModel = createViewModel()

        viewModel.state.test {
            skipItems(1)
            val state = awaitItem()
            assertTrue(state.supportedFileExtensions.isNotEmpty())
        }
    }

    @Test
    fun `allowFileAttachment is false when repository does not support it`() = runViewModelTest {
        fakeRepository.fileAttachmentSupported = false
        val viewModel = createViewModel()

        viewModel.state.test {
            val state = awaitItem()
            assertTrue(state.supportedFileExtensions.isEmpty())
        }
    }
}
