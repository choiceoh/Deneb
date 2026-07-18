package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class SmsDraftStoreTest {

    private data class Fixture(
        val settings: AppSettings,
        val store: SmsDraftStore,
    )

    private fun fixture(initialJson: String = ""): Fixture {
        val settings = AppSettings(MapSettings())
        settings.setSmsDraftsJson(initialJson)
        return Fixture(settings, SmsDraftStore(settings))
    }

    private fun draft(
        id: String,
        status: SmsDraftStatus = SmsDraftStatus.PENDING,
        body: String = "body-$id",
        error: String? = null,
    ) = SmsDraft(
        id = id,
        address = "+821000000000",
        body = body,
        createdAtEpochMs = id.filter(Char::isDigit).toLongOrNull() ?: 0,
        inReplyToSmsId = 42,
        status = status,
        lastError = error,
    )

    @Test
    fun missingPersistenceLoadsAsEmptyAndMalformedPersistenceIsCleared() {
        assertEquals(emptyList(), fixture().store.drafts.value)

        for (raw in listOf("broken", "{}")) {
            val f = fixture(raw)
            assertEquals(emptyList(), f.store.drafts.value, raw)
            assertEquals("", f.settings.getSmsDraftsJson(), raw)
        }
    }

    @Test
    fun persistedDraftsLoadInStoredOrder() {
        val source = fixture()
        val expected = listOf(draft("1"), draft("2", SmsDraftStatus.FAILED, error = "network"))
        source.settings.setSmsDraftsJson(SharedJson.encodeToString(expected))

        val reloaded = SmsDraftStore(source.settings)

        assertEquals(expected, reloaded.drafts.value)
        assertEquals(expected.first(), reloaded.getDraft("1"))
    }

    @Test
    fun addPublishesAndPersistsTheNewSnapshot() = runTest {
        val f = fixture()
        val first = draft("1")
        val second = draft("2")

        f.store.addDraft(first)
        f.store.addDraft(second)

        assertEquals(listOf(first, second), f.store.drafts.value)
        assertEquals(listOf(first, second), SmsDraftStore(f.settings).drafts.value)
        assertTrue(f.settings.getSmsDraftsJson().startsWith("["))
    }

    @Test
    fun addingSameIdReplacesStaleDraftInsteadOfDuplicatingBannerIdentity() = runTest {
        val f = fixture()
        f.store.addDraft(draft("same", body = "old"))
        val replacement = draft("same", body = "new")

        f.store.addDraft(replacement)

        assertEquals(listOf(replacement), f.store.drafts.value)
        assertEquals(replacement, f.store.getDraft("same"))
    }

    @Test
    fun capDropsOldestDraftsAndKeepsNewestTwenty() = runTest {
        val f = fixture()

        repeat(25) { index -> f.store.addDraft(draft(index.toString())) }

        assertEquals(20, f.store.drafts.value.size)
        assertEquals((5 until 25).map(Int::toString), f.store.drafts.value.map { it.id })
    }

    @Test
    fun replacingAnOldIdMovesItsFreshSnapshotToTheTail() = runTest {
        val f = fixture()
        f.store.addDraft(draft("a"))
        f.store.addDraft(draft("b"))
        f.store.addDraft(draft("c"))

        f.store.addDraft(draft("a", body = "updated"))

        assertEquals(listOf("b", "c", "a"), f.store.drafts.value.map { it.id })
        assertEquals("updated", f.store.drafts.value.last().body)
    }

    @Test
    fun removeDeletesMatchingIdentityAndPersists() = runTest {
        val f = fixture()
        f.store.addDraft(draft("a"))
        f.store.addDraft(draft("b"))

        f.store.removeDraft("a")

        assertNull(f.store.getDraft("a"))
        assertEquals(listOf("b"), f.store.drafts.value.map { it.id })
        assertEquals(listOf("b"), SmsDraftStore(f.settings).drafts.value.map { it.id })
    }

    @Test
    fun removeMissingIdKeepsEquivalentSnapshot() = runTest {
        val f = fixture()
        val existing = draft("a")
        f.store.addDraft(existing)

        f.store.removeDraft("missing")

        assertEquals(listOf(existing), f.store.drafts.value)
    }

    @Test
    fun statusTransitionPreservesMessageIdentityAndContent() = runTest {
        val f = fixture()
        val original = draft("a", body = "send this")
        f.store.addDraft(original)

        f.store.updateStatus("a", SmsDraftStatus.SENDING)
        val sending = requireNotNull(f.store.getDraft("a"))
        f.store.updateStatus("a", SmsDraftStatus.SENT)
        val sent = requireNotNull(f.store.getDraft("a"))

        assertEquals(original.copy(status = SmsDraftStatus.SENDING), sending)
        assertEquals(original.copy(status = SmsDraftStatus.SENT), sent)
        assertEquals("send this", sent.body)
        assertEquals(original.address, sent.address)
        assertEquals(original.inReplyToSmsId, sent.inReplyToSmsId)
    }

    @Test
    fun failureStoresErrorAndLaterSuccessClearsIt() = runTest {
        val f = fixture()
        f.store.addDraft(draft("a"))

        f.store.updateStatus("a", SmsDraftStatus.FAILED, error = "radio unavailable")
        assertEquals("radio unavailable", f.store.getDraft("a")?.lastError)

        f.store.updateStatus("a", SmsDraftStatus.SENT)
        assertEquals(SmsDraftStatus.SENT, f.store.getDraft("a")?.status)
        assertNull(f.store.getDraft("a")?.lastError)
    }

    @Test
    fun updateMissingIdDoesNotChangePersistence() = runTest {
        val f = fixture()
        f.store.addDraft(draft("a"))
        val before = f.settings.getSmsDraftsJson()

        f.store.updateStatus("missing", SmsDraftStatus.FAILED, "ignored")

        assertEquals(before, f.settings.getSmsDraftsJson())
        assertEquals(listOf("a"), f.store.drafts.value.map { it.id })
    }

    @Test
    fun identicalStatusAndErrorIsANoOp() = runTest {
        val f = fixture()
        val failed = draft("a", SmsDraftStatus.FAILED, error = "same")
        f.store.addDraft(failed)
        val before = f.settings.getSmsDraftsJson()

        f.store.updateStatus("a", SmsDraftStatus.FAILED, error = "same")

        assertEquals(before, f.settings.getSmsDraftsJson())
        assertEquals(failed, f.store.getDraft("a"))
    }

    @Test
    fun pendingSelectorExcludesEveryNonPendingStateAndKeepsOrder() = runTest {
        val f = fixture()
        val drafts = listOf(
            draft("p1"),
            draft("sending", SmsDraftStatus.SENDING),
            draft("failed", SmsDraftStatus.FAILED, error = "x"),
            draft("p2"),
            draft("sent", SmsDraftStatus.SENT),
        )
        drafts.forEach { f.store.addDraft(it) }

        assertEquals(listOf("p1", "p2"), f.store.getPending().map { it.id })
    }

    @Test
    fun concurrentUniqueAddsDoNotLoseDraftsBelowTheCap() = runTest {
        val f = fixture()

        coroutineScope {
            repeat(20) { index ->
                launch { f.store.addDraft(draft(index.toString())) }
            }
        }

        assertEquals(20, f.store.drafts.value.size)
        assertEquals((0 until 20).map(Int::toString).toSet(), f.store.drafts.value.map { it.id }.toSet())
    }
}
