package ai.deneb.data

import com.russhwolf.settings.MapSettings
import com.russhwolf.settings.Settings
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class MemoryStoreTest {

    private class MemoryWriteFailingSettings(
        private val delegate: Settings = MapSettings(),
    ) : Settings by delegate {
        var failWrites = false
        var memoryWrites = 0

        override fun putString(key: String, value: String) {
            if (key == AppSettings.KEY_AGENT_MEMORIES) {
                if (failWrites) error("memory persistence unavailable")
                memoryWrites++
            }
            delegate.putString(key, value)
        }
    }

    private fun fixture(initialJson: String = "[]"): Pair<AppSettings, MemoryStore> {
        val settings = AppSettings(MapSettings())
        settings.setMemoriesJson(initialJson)
        return settings to MemoryStore(settings)
    }

    @Test
    fun emptyAndMalformedStorageReadAsEmptyAndMalformedStorageIsCleared() {
        assertEquals(emptyList(), fixture().second.getAllMemories())

        for (raw in listOf("broken", "{}")) {
            val (settings, store) = fixture(raw)
            assertEquals(emptyList(), store.getAllMemories(), raw)
            assertEquals("", settings.getMemoriesJson(), raw)
        }
    }

    @Test
    fun storeCreatesACompleteMemoryEntry() = runTest {
        val (_, store) = fixture()

        val entry = store.store(
            key = "preference.language",
            content = "한국어 우선",
            category = MemoryCategory.PREFERENCE,
            source = "settings",
        )

        assertEquals("preference.language", entry.key)
        assertEquals("한국어 우선", entry.content)
        assertEquals(MemoryCategory.PREFERENCE, entry.category)
        assertEquals("settings", entry.source)
        assertEquals(1, entry.hitCount)
        assertTrue(entry.createdAt > 0)
        assertEquals(entry.createdAt, entry.updatedAt)
        assertEquals(listOf(entry), store.getAllMemories())
    }

    @Test
    fun storingExistingKeyUpdatesInPlaceAndPreservesCreationAndHits() = runTest {
        val (_, store) = fixture()
        val original = store.store("project", "초안", MemoryCategory.GENERAL, source = "mail")
        val reinforced = store.reinforceMemory("project")

        val updated = store.store("project", "확정", MemoryCategory.LEARNING)

        assertEquals(original.createdAt, updated.createdAt)
        assertEquals(2, updated.hitCount)
        assertEquals("확정", updated.content)
        assertEquals(MemoryCategory.LEARNING, updated.category)
        assertEquals("mail", updated.source)
        assertEquals(reinforced?.hitCount, updated.hitCount)
        assertEquals(1, store.getAllMemories().size)
    }

    @Test
    fun explicitSourceReplacesExistingSource() = runTest {
        val (_, store) = fixture()
        store.store("k", "v1", source = "old")

        val updated = store.store("k", "v2", source = "new")

        assertEquals("new", updated.source)
    }

    @Test
    fun updateContentChangesOnlyContentAndUpdatedTime() = runTest {
        val (_, store) = fixture()
        val original = store.store("k", "before", MemoryCategory.ERROR, "session")

        val updated = assertNotNull(store.updateContent("k", "after"))

        assertEquals(original.key, updated.key)
        assertEquals(original.createdAt, updated.createdAt)
        assertEquals(original.category, updated.category)
        assertEquals(original.source, updated.source)
        assertEquals(original.hitCount, updated.hitCount)
        assertEquals("after", updated.content)
        assertTrue(updated.updatedAt >= original.updatedAt)
    }

    @Test
    fun updateContentOfMissingKeyDoesNotPersistAnything() = runTest {
        val (settings, store) = fixture()
        val before = settings.getMemoriesJson()

        assertNull(store.updateContent("missing", "value"))

        assertEquals(before, settings.getMemoriesJson())
    }

    @Test
    fun missingUpdateRepairsMalformedStorageWithoutInventingAnEntry() = runTest {
        val (settings, store) = fixture("broken")

        assertNull(store.updateContent("missing", "value"))

        assertEquals("", settings.getMemoriesJson())
        assertEquals(emptyList(), store.getAllMemories())
    }

    @Test
    fun reinforceIncrementsHitsAndKeepsOtherFields() = runTest {
        val (_, store) = fixture()
        val original = store.store("k", "value", MemoryCategory.PREFERENCE, "user")

        val first = assertNotNull(store.reinforceMemory("k"))
        val second = assertNotNull(store.reinforceMemory("k"))

        assertEquals(2, first.hitCount)
        assertEquals(3, second.hitCount)
        assertEquals(original.content, second.content)
        assertEquals(original.category, second.category)
        assertEquals(original.source, second.source)
        assertNull(store.reinforceMemory("missing"))
    }

    @Test
    fun reinforceSaturatesAtMaximumHitCount() = runTest {
        val saturated = MemoryEntry(
            key = "popular",
            content = "value",
            createdAt = 1,
            updatedAt = 1,
            hitCount = Int.MAX_VALUE,
        )
        val (settings, store) = fixture(SharedJson.encodeToString(listOf(saturated)))

        val reinforced = assertNotNull(store.reinforceMemory("popular"))

        assertEquals(Int.MAX_VALUE, reinforced.hitCount)
        assertEquals(Int.MAX_VALUE, MemoryStore(settings).getAllMemories().single().hitCount)
    }

    @Test
    fun concurrentReinforcementDoesNotLoseHits() = runTest {
        val (_, store) = fixture()
        store.store("popular", "value")

        coroutineScope {
            repeat(50) {
                launch { store.reinforceMemory("popular") }
            }
        }

        assertEquals(51, store.getAllMemories().single().hitCount)
    }

    @Test
    fun promotionCandidatesRespectInclusiveThresholdAndInsertionOrder() = runTest {
        val (_, store) = fixture()
        store.store("one", "1")
        store.store("three", "3")
        store.reinforceMemory("three")
        store.reinforceMemory("three")
        store.store("two", "2")
        store.reinforceMemory("two")

        assertEquals(listOf("three", "two"), store.getPromotionCandidates(minHits = 2).map { it.key })
        assertEquals(listOf("three"), store.getPromotionCandidates(minHits = 3).map { it.key })
        assertEquals(emptyList(), store.getPromotionCandidates(minHits = 4))
    }

    @Test
    fun forgetRemovesExactlyOneKeyAndReportsOutcome() = runTest {
        val (_, store) = fixture()
        store.store("a", "A")
        store.store("b", "B")

        assertTrue(store.forget("a"))
        assertEquals(listOf("b"), store.getAllMemories().map { it.key })
        assertFalse(store.forget("a"))
        assertFalse(store.forget("missing"))
    }

    @Test
    fun forgetRemovesEveryPersistedDuplicateOfAKey() = runTest {
        val first = MemoryEntry("dup", "first", 1, 1)
        val keep = MemoryEntry("keep", "value", 2, 2)
        val second = MemoryEntry("dup", "second", 3, 3)
        val (_, store) = fixture(SharedJson.encodeToString(listOf(first, keep, second)))

        assertTrue(store.forget("dup"))

        assertEquals(listOf(keep), store.getAllMemories())
    }

    @Test
    fun storeHealsMalformedPersistence() = runTest {
        val (settings, store) = fixture("not-json")

        val entry = store.store("fresh", "valid")

        assertEquals(listOf(entry), store.getAllMemories())
        assertTrue(settings.getMemoriesJson().startsWith("["))
    }

    @Test
    fun failedStoreDoesNotExposeAnUncommittedMemory() = runTest {
        val raw = MemoryWriteFailingSettings()
        val settings = AppSettings(raw)
        val existing = MemoryEntry("stable", "value", 1, 1)
        settings.setMemoriesJson(SharedJson.encodeToString(listOf(existing)))
        raw.memoryWrites = 0
        raw.failWrites = true
        val store = MemoryStore(settings)

        assertFailsWith<IllegalStateException> {
            store.store("lost", "value")
        }

        raw.failWrites = false
        assertEquals(listOf(existing), store.getAllMemories())
        assertEquals(0, raw.memoryWrites)
    }

    @Test
    fun failedUpdateAndForgetKeepTheCommittedSnapshot() = runTest {
        val raw = MemoryWriteFailingSettings()
        val settings = AppSettings(raw)
        val existing = MemoryEntry("stable", "value", 1, 1)
        settings.setMemoriesJson(SharedJson.encodeToString(listOf(existing)))
        raw.failWrites = true
        val store = MemoryStore(settings)

        assertFailsWith<IllegalStateException> { store.updateContent("stable", "lost") }
        assertFailsWith<IllegalStateException> { store.forget("stable") }

        raw.failWrites = false
        assertEquals(listOf(existing), store.getAllMemories())
    }

    @Test
    fun serializedEntriesSurviveAStoreRecreation() = runTest {
        val (settings, store) = fixture()
        val first = store.store("a", "A", MemoryCategory.LEARNING, "source")
        store.reinforceMemory("a")
        val second = store.store("b", "B")

        val reloaded = MemoryStore(settings).getAllMemories()

        assertEquals(listOf(first.key, second.key), reloaded.map { it.key })
        assertEquals(2, reloaded.first().hitCount)
        assertEquals("source", reloaded.first().source)
    }

    @Test
    fun concurrentStoresDoNotLoseIndependentEntries() = runTest {
        val (_, store) = fixture()

        coroutineScope {
            repeat(20) { index ->
                launch {
                    store.store(
                        key = "key-$index",
                        content = "value-$index",
                        category = MemoryCategory.LEARNING,
                    )
                }
            }
        }

        val entries = store.getAllMemories()
        assertEquals(20, entries.size)
        assertEquals((0 until 20).map { "key-$it" }.toSet(), entries.map { it.key }.toSet())
        assertTrue(entries.all { it.category == MemoryCategory.LEARNING })
    }
}
