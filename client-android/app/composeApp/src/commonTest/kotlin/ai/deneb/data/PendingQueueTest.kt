package ai.deneb.data

import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class PendingQueueTest {

    private class Fixture(maxSize: Int = 100, initial: String = "") {
        var raw = initial
        var beforeWrite: (() -> Unit)? = null
        val writes = mutableListOf<String>()
        val queue = PendingQueue(
            readJson = { raw },
            writeJson = {
                beforeWrite?.invoke()
                raw = it
                writes += it
            },
            serializer = ListSerializer(String.serializer()),
            keyOf = { it.substringBefore(':') },
            maxSize = maxSize,
        )
    }

    @Test
    fun emptyStorageReadsWithoutWriting() {
        val f = Fixture()

        assertEquals(emptyList(), f.queue.get())
        assertEquals(emptyList(), f.writes)
    }

    @Test
    fun malformedStorageIsClearedAfterFirstRead() {
        for (raw in listOf("not-json", "{}", "[1, 2]")) {
            val f = Fixture(initial = raw)

            assertEquals(emptyList(), f.queue.get(), raw)
            assertEquals("", f.raw, raw)
            assertEquals(listOf(""), f.writes, raw)

            assertEquals(emptyList(), f.queue.get(), raw)
            assertEquals(1, f.writes.size, raw)
        }
    }

    @Test
    fun addPersistsItemsInFifoOrder() = runTest {
        val f = Fixture()

        f.queue.add(listOf("a:1", "b:1"))
        f.queue.add(listOf("c:1"))

        assertEquals(listOf("a:1", "b:1", "c:1"), f.queue.get())
        assertEquals(2, f.writes.size)
        assertEquals(f.raw, f.writes.last())
    }

    @Test
    fun addCapsFromTheHeadAndKeepsNewestItems() = runTest {
        val f = Fixture(maxSize = 3)

        f.queue.add(listOf("a:1", "b:1"))
        f.queue.add(listOf("c:1", "d:1"))

        assertEquals(listOf("b:1", "c:1", "d:1"), f.queue.get())
    }

    @Test
    fun oneOversizedBatchKeepsOnlyItsNewestTail() = runTest {
        val f = Fixture(maxSize = 2)

        f.queue.add(listOf("a:1", "b:1", "c:1", "d:1"))

        assertEquals(listOf("c:1", "d:1"), f.queue.get())
    }

    @Test
    fun duplicateKeysRemainUntilExplicitlyRemoved() = runTest {
        val f = Fixture()

        f.queue.add(listOf("mail:old", "mail:new", "other:1"))

        assertEquals(listOf("mail:old", "mail:new", "other:1"), f.queue.get())
        f.queue.remove(listOf("mail:any"))
        assertEquals(listOf("other:1"), f.queue.get())
    }

    @Test
    fun removeUsesStableKeysAndIgnoresMissingKeys() = runTest {
        val f = Fixture()
        f.queue.add(listOf("a:1", "b:1", "c:1"))

        f.queue.remove(listOf("c:other", "missing:any"))

        assertEquals(listOf("a:1", "b:1"), f.queue.get())
    }

    @Test
    fun emptyMutationsDoNotWrite() = runTest {
        val f = Fixture(initial = "[\"a:1\"]")

        f.queue.add(emptyList())
        f.queue.remove(emptyList())

        assertEquals(emptyList(), f.writes)
        assertEquals(listOf("a:1"), f.queue.get())
    }

    @Test
    fun clearDropsPersistedContent() = runTest {
        val f = Fixture(initial = "[\"a:1\"]")

        f.queue.clear()

        assertEquals("", f.raw)
        assertEquals(emptyList(), f.queue.get())
    }

    @Test
    fun addingToCorruptStorageHealsItWithValidJson() = runTest {
        val f = Fixture(initial = "broken")

        f.queue.add(listOf("fresh:1"))

        assertEquals(listOf("fresh:1"), f.queue.get())
        assertTrue(f.raw.startsWith("["))
    }

    @Test
    fun readerDuringMutationCannotClearIncomingWrite() = runTest {
        val f = Fixture(initial = "broken")
        var readDuringWrite: List<String>? = null
        f.beforeWrite = { readDuringWrite = f.queue.get() }

        f.queue.add(listOf("fresh:1"))

        assertEquals(emptyList(), readDuringWrite)
        assertEquals(listOf("fresh:1"), f.queue.get())
        assertEquals(1, f.writes.size)
    }

    @Test
    fun concurrentAddsDoNotLoseItems() = runTest {
        val f = Fixture(maxSize = 50)

        coroutineScope {
            repeat(20) { index ->
                launch { f.queue.add(listOf("$index:value")) }
            }
        }

        assertEquals(20, f.queue.get().size)
        assertEquals((0 until 20).map { "$it:value" }.toSet(), f.queue.get().toSet())
        assertEquals(20, f.writes.size)
    }
}
