package ai.deneb.data

import ai.deneb.TerminalLine
import com.russhwolf.settings.MapSettings
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ConversationStorageBehaviorTest {

    private val json = Json { encodeDefaults = true }

    private fun fixture(): Pair<AppSettings, ConversationStorage> {
        val settings = AppSettings(MapSettings())
        return settings to ConversationStorage(settings)
    }

    private fun conversation(
        id: String,
        title: String = id,
        messages: List<Conversation.Message> = emptyList(),
        shell: List<TerminalLine> = emptyList(),
    ) = Conversation(
        id = id,
        title = title,
        messages = messages,
        shellTranscript = shell,
        createdAt = 100,
        updatedAt = 200,
    )

    private fun message(id: String, role: String, content: String) = Conversation.Message(
        id = id,
        role = role,
        content = content,
    )

    @Test
    fun loadRestoresPersistedConversations() {
        val (settings, storage) = fixture()
        val expected = listOf(
            conversation("a", messages = listOf(message("u1", "user", "질문"))),
            conversation("b", messages = listOf(message("a1", "assistant", "답변"))),
        )
        settings.setConversationsJson(json.encodeToString(ConversationsData(conversations = expected)))

        storage.loadConversations()

        assertEquals(expected, storage.conversations.value)
    }

    @Test
    fun malformedPersistenceLoadsAsAnEmptyList() {
        val (settings, storage) = fixture()
        settings.setConversationsJson("not-json")

        storage.loadConversations()

        assertEquals(emptyList(), storage.conversations.value)
        assertEquals("not-json", settings.getConversationsJson())
    }

    @Test
    fun saveAppendsNewConversationsInOrder() {
        val (_, storage) = fixture()

        storage.saveConversation(conversation("a"))
        storage.saveConversation(conversation("b"))

        assertEquals(listOf("a", "b"), storage.conversations.value.map { it.id })
    }

    @Test
    fun saveReplacesExistingConversationWithoutReordering() {
        val (_, storage) = fixture()
        storage.saveConversation(conversation("a", title = "old"))
        storage.saveConversation(conversation("b"))

        storage.saveConversation(conversation("a", title = "new"))

        assertEquals(listOf("a", "b"), storage.conversations.value.map { it.id })
        assertEquals("new", storage.conversations.value.first().title)
    }

    @Test
    fun chatOnlySavePreservesExistingShellTranscript() {
        val (_, storage) = fixture()
        val shell = listOf(TerminalLine.Command("make test"), TerminalLine.Output("ok"))
        storage.saveConversation(conversation("a", shell = shell))

        storage.saveConversation(
            conversation(
                "a",
                title = "updated",
                messages = listOf(message("m1", "assistant", "done")),
                shell = emptyList(),
            ),
        )

        val saved = storage.conversations.value.single()
        assertEquals("updated", saved.title)
        assertEquals(listOf("m1"), saved.messages.map { it.id })
        assertEquals(shell, saved.shellTranscript)
    }

    @Test
    fun explicitShellSnapshotReplacesExistingTranscript() {
        val (_, storage) = fixture()
        storage.saveConversation(conversation("a", shell = listOf(TerminalLine.Output("old"))))

        val replacement = listOf(TerminalLine.Error("new"))
        storage.saveConversation(conversation("a", shell = replacement))

        assertEquals(replacement, storage.conversations.value.single().shellTranscript)
    }

    @Test
    fun updateShellTranscriptIsNoOpForUnknownConversation() {
        val (settings, storage) = fixture()
        storage.saveConversation(conversation("known"))
        val before = settings.getConversationsJson()

        storage.updateShellTranscript("missing", listOf(TerminalLine.Output("ignored")))

        assertEquals(before, settings.getConversationsJson())
        assertEquals(emptyList(), storage.conversations.value.single().shellTranscript)
    }

    @Test
    fun updateShellTranscriptPersistsAndSurvivesReload() {
        val (settings, storage) = fixture()
        val lines = listOf(
            TerminalLine.Command("git status"),
            TerminalLine.Output("clean"),
            TerminalLine.Error("none"),
        )
        storage.saveConversation(conversation("a"))

        storage.updateShellTranscript("a", lines)

        assertEquals(lines, storage.conversations.value.single().shellTranscript)
        val reloaded = ConversationStorage(settings)
        reloaded.loadConversations()
        assertEquals(lines, reloaded.conversations.value.single().shellTranscript)
    }

    @Test
    fun sameShellSnapshotDoesNotRewritePersistence() {
        val (settings, storage) = fixture()
        val lines = listOf(TerminalLine.Output("same"))
        storage.saveConversation(conversation("a"))
        storage.updateShellTranscript("a", lines)
        val before = settings.getConversationsJson()

        storage.updateShellTranscript("a", lines)

        assertEquals(before, settings.getConversationsJson())
    }

    @Test
    fun oversizedTranscriptDropsOldestWholeLinesFirst() {
        val (_, storage) = fixture()
        storage.saveConversation(conversation("a"))
        val lines = listOf(
            TerminalLine.Command("a".repeat(4_000)),
            TerminalLine.Output("b".repeat(4_000)),
            TerminalLine.Error("c".repeat(4_000)),
            TerminalLine.Output("d".repeat(4_000)),
        )

        storage.updateShellTranscript("a", lines)

        val kept = storage.conversations.value.single().shellTranscript
        assertEquals(listOf(lines[2], lines[3]), kept)
        assertTrue(kept.sumOf { it.text.length } <= 10_000)
    }

    @Test
    fun oneOversizedLatestLineKeepsItsTailAndType() {
        val (_, storage) = fixture()
        storage.saveConversation(conversation("a"))
        val long = "prefix-" + "x".repeat(12_000) + "-tail"

        storage.updateShellTranscript("a", listOf(TerminalLine.Error(long)))

        val kept = storage.conversations.value.single().shellTranscript.single()
        assertTrue(kept is TerminalLine.Error)
        assertEquals(10_000, kept.text.length)
        assertTrue(kept.text.endsWith("-tail"))
    }

    @Test
    fun emptyTranscriptCanExplicitlyClearStoredShell() {
        val (_, storage) = fixture()
        storage.saveConversation(conversation("a", shell = listOf(TerminalLine.Output("old"))))

        storage.updateShellTranscript("a", emptyList())

        assertEquals(emptyList(), storage.conversations.value.single().shellTranscript)
    }

    @Test
    fun deleteRemovesOnlyTheRequestedConversationAndPersists() {
        val (settings, storage) = fixture()
        storage.saveConversation(conversation("a"))
        storage.saveConversation(conversation("b"))

        storage.deleteConversation("a")

        assertEquals(listOf("b"), storage.conversations.value.map { it.id })
        val reloaded = ConversationStorage(settings)
        reloaded.loadConversations()
        assertEquals(listOf("b"), reloaded.conversations.value.map { it.id })
    }

    @Test
    fun deleteMissingConversationLeavesTheListUnchanged() {
        val (_, storage) = fixture()
        storage.saveConversation(conversation("a"))

        storage.deleteConversation("missing")

        assertEquals(listOf("a"), storage.conversations.value.map { it.id })
    }
}
