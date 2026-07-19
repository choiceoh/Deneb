package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** In-memory [WikiMirrorFiles] — the diary mirror shares the wiki mirror's
 *  storage interface, so the same fake exercises its persistence. */
private class MemoryDiaryFiles : WikiMirrorFiles {
    val files = mutableMapOf<String, String>()
    var failWrites = false

    override fun read(name: String): String? = files[name]

    override fun write(name: String, content: String) {
        if (failWrites) error("write failed: $name")
        files[name] = content
    }

    override fun delete(name: String) {
        files.remove(name)
    }
}

private fun entry(file: String, header: String, content: String, at: Long = 0) = DiaryMirrorEntry(file = file, header = header, content = content, at = at)

class DiaryMirrorStoreTest {
    private val owner = "gw#token-a"

    private fun store(files: WikiMirrorFiles, owner: () -> String = { this.owner }) = DiaryMirrorStore(files, owner)

    @Test
    fun replaceAllPersistsAndSearchRanksByHitsThenRecency() = runTest {
        val files = MemoryDiaryFiles()
        val s = store(files)
        assertTrue(
            s.replaceAll(
                listOf(
                    entry("diary-2026-07-18.md", "09:00", "곡성 납기 회의 — 곡성 자재 지연", at = 1),
                    entry("diary-2026-07-19.md", "10:00", "곡성 현장 방문", at = 2),
                    entry("diary-2026-07-19.md", "11:00", "무관한 업무 기록", at = 3),
                ),
                nowMs = 1000,
            ),
        )
        assertEquals(3, s.entryCount())
        assertEquals(1000, s.syncedAtMs())

        val hits = s.search("곡성")
        assertEquals(2, hits.size)
        // Presence-based scoring (wiki mirror parity): equal-score hits rank
        // newest-first.
        assertTrue(hits[0].snippet.contains("현장"))
        assertEquals("diary", hits[0].category)
        assertEquals("10:00", hits[0].title)

        // Reload from disk into a fresh store: persistence round-trip.
        val reloaded = store(files)
        assertEquals(3, reloaded.entryCount())
        assertEquals(1, reloaded.search("현장").size)
    }

    @Test
    fun searchRequiresEveryToken() = runTest {
        val s = store(MemoryDiaryFiles())
        s.replaceAll(
            listOf(
                entry("diary-2026-07-19.md", "10:00", "곡성 납기 확정"),
                entry("diary-2026-07-19.md", "11:00", "곡성 방문"),
            ),
            nowMs = 1,
        )
        assertEquals(1, s.search("곡성 납기").size)
        assertEquals(0, s.search("곡성 없는말").size)
        assertEquals(0, s.search("   ").size)
    }

    @Test
    fun foreignOwnerMirrorIsWipedNotServed() = runTest {
        val files = MemoryDiaryFiles()
        store(files).replaceAll(listOf(entry("diary-2026-07-19.md", "10:00", "계정 A의 일기")), nowMs = 1)

        val other = store(files) { "gw#token-b" }
        assertEquals(0, other.entryCount())
        assertTrue(files.files.isEmpty(), "foreign mirror file must be deleted, not kept")
    }

    @Test
    fun replaceAllRefusesWhenOwnerChangedAfterFence() = runTest {
        val files = MemoryDiaryFiles()
        var current = owner
        val s = store(files) { current }
        current = "gw#token-b"
        assertFalse(s.replaceAll(listOf(entry("diary-2026-07-19.md", "10:00", "x")), nowMs = 1, expectedOwner = owner))
    }

    @Test
    fun failedWriteKeepsPreviousMirror() = runTest {
        val files = MemoryDiaryFiles()
        val s = store(files)
        assertTrue(s.replaceAll(listOf(entry("diary-2026-07-18.md", "09:00", "이전 코퍼스")), nowMs = 1))
        files.failWrites = true
        assertFalse(s.replaceAll(listOf(entry("diary-2026-07-19.md", "10:00", "새 코퍼스")), nowMs = 2))
        assertEquals(1, s.search("이전").size)
    }

    @Test
    fun credentialSwitchEvictsHotEntries() = runTest {
        val files = MemoryDiaryFiles()
        val s = store(files)
        s.replaceAll(listOf(entry("diary-2026-07-19.md", "10:00", "비밀 일기")), nowMs = 1)
        s.evictMemoryForCredentialSwitch()
        assertEquals(0, s.search("비밀").size)
        s.clear()
        assertTrue(files.files.isEmpty())
    }
}
