package ai.deneb.ui.chat

import kotlinx.collections.immutable.toImmutableList
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertSame
import kotlin.test.assertTrue

class ChatUiStateModelTest {

    private fun history(
        id: String,
        role: History.Role,
        content: String,
        thinking: Boolean = false,
    ) = History(
        id = id,
        role = role,
        content = content,
        isThinking = thinking,
    )

    @Test
    fun emptyHistoryHasNoRenderedAssistant() {
        assertNull(emptyList<History>().lastRenderedAssistant())
    }

    @Test
    fun latestOrdinaryAssistantIsReturnedByIdentity() {
        val first = history("a1", History.Role.ASSISTANT, "first")
        val latest = history("a2", History.Role.ASSISTANT, "latest")
        val rows = listOf(first, history("u", History.Role.USER, "question"), latest)

        assertSame(latest, rows.lastRenderedAssistant())
    }

    @Test
    fun trailingUserAndToolRowsDoNotHidePreviousAssistant() {
        val assistant = history("a", History.Role.ASSISTANT, "answer")
        val rows = listOf(
            assistant,
            history("tool-call", History.Role.TOOL_EXECUTING, "running"),
            history("tool-result", History.Role.TOOL, "result"),
            history("user", History.Role.USER, "follow-up"),
        )

        assertSame(assistant, rows.lastRenderedAssistant())
    }

    @Test
    fun thinkingOnlyAssistantIsSkipped() {
        val rendered = history("rendered", History.Role.ASSISTANT, "visible")
        val thinking = history("thinking", History.Role.ASSISTANT, "private reasoning", thinking = true)

        assertSame(rendered, listOf(rendered, thinking).lastRenderedAssistant())
        assertNull(listOf(thinking).lastRenderedAssistant())
    }

    @Test
    fun emptyAssistantContentIsSkippedButWhitespaceContentIsRenderable() {
        val prior = history("prior", History.Role.ASSISTANT, "prior")
        val empty = history("empty", History.Role.ASSISTANT, "")
        val spaces = history("spaces", History.Role.ASSISTANT, "   ")

        assertSame(prior, listOf(prior, empty).lastRenderedAssistant())
        assertSame(spaces, listOf(prior, empty, spaces).lastRenderedAssistant())
    }

    @Test
    fun selectorWorksForImmutableHistoryWithoutMutation() {
        val rows = listOf(
            history("a1", History.Role.ASSISTANT, "one"),
            history("a2", History.Role.ASSISTANT, "two"),
        ).toImmutableList()
        val before = rows.toList()

        assertEquals("a2", rows.lastRenderedAssistant()?.id)
        assertEquals(before, rows)
    }

    @Test
    fun onlyExactProactiveSourceIsAReport() {
        assertTrue(WorkFeedItem(source = "proactive").isProactiveReport)

        for (source in listOf("", "Proactive", " proactive", "proactive ", "proactive-mail", "capture")) {
            assertFalse(WorkFeedItem(source = source).isProactiveReport, source)
        }
    }

    @Test
    fun proactiveClassificationDoesNotDependOnOtherCardState() {
        val report = WorkFeedItem(
            id = "item",
            source = "proactive",
            status = "done",
            priority = -1,
            question = true,
            readAtMs = 99,
            actions = listOf(WorkFeedAction(id = "action")),
        )

        assertTrue(report.isProactiveReport)
        assertFalse(report.copy(source = "capture").isProactiveReport)
    }
}
