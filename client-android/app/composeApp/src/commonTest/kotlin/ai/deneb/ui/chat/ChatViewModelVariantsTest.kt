package ai.deneb.ui.chat

import ai.deneb.data.TaskScheduler
import ai.deneb.testutil.FakeDataRepository
import androidx.lifecycle.viewModelScope
import app.cash.turbine.test
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * 응답 변형(‹ n/N ›)과 편집-재전송의 계약:
 *  - regenerate는 대체되는 답변을 스태시하고 인덱스를 라이브(=size)에 둔다.
 *  - selectAnswerVariant는 [0, size] 범위로 클램프된다.
 *  - editResendLast는 마지막 교환을 되감고 고친 텍스트로 재질의하며 스태시를 비운다.
 *  - 새 질문(ask)은 스태시를 비운다 — 변형은 항상 "마지막 답변"에만 속한다.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ChatViewModelVariantsTest {

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
            testScheduler.runCurrent()
        }
    }

    private fun createViewModel(): ChatViewModel {
        val noOpScheduler = TaskScheduler(fakeRepository, enabled = false)
        return ChatViewModel(fakeRepository, noOpScheduler, unconfinedDispatcher)
            .also(viewModels::add)
    }

    private fun seedExchange(question: String = "첫 질문", answer: String = "첫 답변") {
        fakeRepository.chatHistory.value = listOf(
            History(role = History.Role.USER, content = question),
            History(role = History.Role.ASSISTANT, content = answer),
        )
    }

    @Test
    fun `regenerate stashes the replaced answer and parks the index on live`() = runViewModelTest {
        seedExchange(answer = "옛 답변")
        val viewModel = createViewModel()
        viewModel.state.test {
            awaitItem()
            viewModel.state.value.actions.regenerate()
            advanceTimeBy(200)
            testScheduler.runCurrent()
            val state = expectMostRecentItem()
            assertEquals(1, state.lastAnswerVariants.size)
            assertEquals("옛 답변", state.lastAnswerVariants[0].content)
            assertEquals(1, state.lastAnswerVariantIndex) // == variants.size → 라이브 답변
            assertEquals("첫 질문", fakeRepository.askCalls.last().first)
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `selectAnswerVariant clamps into the valid range`() = runViewModelTest {
        seedExchange()
        val viewModel = createViewModel()
        viewModel.state.test {
            awaitItem()
            viewModel.state.value.actions.regenerate()
            advanceTimeBy(200)
            testScheduler.runCurrent()

            viewModel.state.value.actions.selectAnswerVariant(0)
            advanceTimeBy(200)
            testScheduler.runCurrent()
            assertEquals(0, expectMostRecentItem().lastAnswerVariantIndex)

            viewModel.state.value.actions.selectAnswerVariant(99)
            advanceTimeBy(200)
            testScheduler.runCurrent()
            assertEquals(1, expectMostRecentItem().lastAnswerVariantIndex)

            viewModel.state.value.actions.selectAnswerVariant(-5)
            advanceTimeBy(200)
            testScheduler.runCurrent()
            assertEquals(0, expectMostRecentItem().lastAnswerVariantIndex)
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `editResendLast rewinds and re-asks with the edited text, clearing variants`() = runViewModelTest {
        seedExchange(question = "원래 질문")
        val viewModel = createViewModel()
        viewModel.state.test {
            awaitItem()
            viewModel.state.value.actions.regenerate()
            advanceTimeBy(200)
            testScheduler.runCurrent()
            assertEquals(1, expectMostRecentItem().lastAnswerVariants.size)

            viewModel.state.value.actions.editResendLast("고친 질문")
            advanceTimeBy(200)
            testScheduler.runCurrent()

            val state = expectMostRecentItem()
            assertTrue(state.lastAnswerVariants.isEmpty())
            assertEquals(0, state.lastAnswerVariantIndex)
            assertEquals("고친 질문", fakeRepository.askCalls.last().first)
            // The rewound exchange was replaced by the edited one (fake appends Q+A).
            val history = fakeRepository.chatHistory.value
            assertEquals("고친 질문", history.first { it.role == History.Role.USER }.content)
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `a new question clears the previous answer's variants`() = runViewModelTest {
        seedExchange()
        val viewModel = createViewModel()
        viewModel.state.test {
            awaitItem()
            viewModel.state.value.actions.regenerate()
            advanceTimeBy(200)
            testScheduler.runCurrent()
            assertEquals(1, expectMostRecentItem().lastAnswerVariants.size)

            viewModel.state.value.actions.ask("완전히 새 질문")
            advanceTimeBy(200)
            testScheduler.runCurrent()

            val state = expectMostRecentItem()
            assertTrue(state.lastAnswerVariants.isEmpty())
            assertEquals(0, state.lastAnswerVariantIndex)
            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `editResendLast is a no-op on an empty conversation`() = runViewModelTest {
        val viewModel = createViewModel()
        viewModel.state.test {
            awaitItem()
            viewModel.state.value.actions.editResendLast("허공에 보내기")
            advanceTimeBy(200)
            testScheduler.runCurrent()
            assertTrue(fakeRepository.askCalls.isEmpty())
            cancelAndIgnoreRemainingEvents()
        }
    }
}
