package ai.deneb.deneb

import ai.deneb.data.SynchronousLock
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * On-device mirror of the diary corpus (~2,350 entries / ~1.9MB) so 검색's
 * 일기 section keeps working with no gateway — the diary counterpart of
 * [WikiMirrorStore]. Bulk-seeded from `miniapp.memory.diary_mirror` on the
 * same daily cadence as the wiki mirror; the diary is append-only with no
 * per-entry change events, so the full pull is the only sync path.
 *
 * Storage: one JSON document ([DIARY_MIRROR_FILE]) in the shared mirror
 * directory — [WikiMirrorFiles.write] atomically replaces a complete file,
 * which is the whole commit story for a single-document store. The document
 * carries the owner fingerprint (url#token) so a credential switch wipes
 * rather than serves another account's diary.
 */
internal class DiaryMirrorStore(
    private val files: WikiMirrorFiles,
    private val owner: () -> String,
) {
    private val mutex = Mutex()
    private val stateLock = SynchronousLock()
    private var loaded = false
    private var doc = DiaryMirrorDoc()

    suspend fun entryCount(): Int = mutex.withLock {
        withStateLock {
            loadLocked()
            doc.entries.size
        }
    }

    suspend fun syncedAtMs(): Long = mutex.withLock {
        withStateLock {
            loadLocked()
            doc.syncedAtMs
        }
    }

    /** Offline diary search: every whitespace token must appear in the entry
     *  (header or content, case-insensitive); more/matching-earlier content
     *  ranks by hit count, ties newest-first. Rows mirror the online
     *  miniapp.search.all diary mapping (title = section header). */
    suspend fun search(query: String, limit: Int = 20): List<SearchHit> = mutex.withLock {
        withStateLock state@{
            loadLocked()
            val tokens = query.trim().split(Regex("\\s+")).filter { it.isNotBlank() }
            if (tokens.isEmpty()) return@state emptyList()
            doc.entries
                .mapNotNull { entry ->
                    var score = 0
                    for (t in tokens) {
                        val inHeader = entry.header.contains(t, ignoreCase = true)
                        val inContent = entry.content.contains(t, ignoreCase = true)
                        if (!inHeader && !inContent) return@mapNotNull null
                        score += if (inHeader) 2 else 1
                    }
                    score to entry
                }
                .sortedWith(compareByDescending<Pair<Int, DiaryMirrorEntry>> { it.first }.thenByDescending { it.second.at })
                .take(limit)
                .map { (_, entry) ->
                    SearchHit(
                        path = "",
                        title = entry.header.ifBlank { "일기" },
                        snippet = diaryMirrorSnippet(entry.content, tokens.first()),
                        category = "diary",
                    )
                }
        }
    }

    /** Atomically replace the whole mirror after a successful bulk pull.
     *  [expectedOwner] pins the account fingerprint captured when the pull
     *  started — same fencing as the wiki mirror. */
    suspend fun replaceAll(all: List<DiaryMirrorEntry>, nowMs: Long, expectedOwner: String? = null): Boolean = mutex.withLock {
        withStateLock state@{
            if (expectedOwner != null && owner() != expectedOwner) return@state false
            loadLocked()
            val next = DiaryMirrorDoc(
                owner = expectedOwner ?: owner(),
                syncedAtMs = nowMs,
                entries = all,
            )
            runCatching { files.write(DIARY_MIRROR_FILE, diaryJson.encodeToString(DiaryMirrorDoc.serializer(), next)) }
                .getOrNull() ?: return@state false
            doc = next
            true
        }
    }

    /** Drop hot entries immediately on credential switch (non-suspend); disk
     *  wipe follows via [clear], reads are owner-guarded meanwhile. */
    fun evictMemoryForCredentialSwitch() = withStateLock {
        evictMemoryLocked()
    }

    /** Credential switch: drop memory and disk so account B never sees A's diary. */
    suspend fun clear(): Unit = mutex.withLock {
        withStateLock {
            evictMemoryLocked()
            runCatching { files.delete(DIARY_MIRROR_FILE) }
        }
    }

    private fun evictMemoryLocked() {
        doc = DiaryMirrorDoc()
        loaded = true
    }

    private fun <T> withStateLock(action: () -> T): T = stateLock.withLock(action)

    private fun loadLocked() {
        if (loaded) {
            // Credentials can switch while entries stay hot (clear() is async).
            if (doc.owner.isNotEmpty() && doc.owner != owner()) doc = DiaryMirrorDoc()
            return
        }
        val raw = runCatching { files.read(DIARY_MIRROR_FILE) }.getOrNull()
        val stored = raw?.let { runCatching { diaryJson.decodeFromString(DiaryMirrorDoc.serializer(), it) }.getOrNull() }
        if (stored == null || stored.owner != owner()) {
            // Foreign/corrupt mirror: wipe rather than serve another account's diary.
            if (raw != null) runCatching { files.delete(DIARY_MIRROR_FILE) }
            loaded = true
            return
        }
        doc = stored
        loaded = true
    }

    private companion object {
        const val DIARY_MIRROR_FILE = "diary-mirror.json"
        val diaryJson = Json { ignoreUnknownKeys = true }
    }
}

@Serializable
internal data class DiaryMirrorDoc(
    val owner: String = "",
    val syncedAtMs: Long = 0,
    val entries: List<DiaryMirrorEntry> = emptyList(),
)

/** One diary section: `## HH:MM` in a `diary-YYYY-MM-DD.md` file. */
@Serializable
internal data class DiaryMirrorEntry(
    val file: String,
    val header: String,
    val content: String,
    val at: Long = 0,
)

/** ~96 chars of content context around the first hit of [token] (else the head). */
internal fun diaryMirrorSnippet(content: String, token: String): String {
    val idx = content.indexOf(token, ignoreCase = true)
    val start = if (idx < 0) 0 else (idx - 24).coerceAtLeast(0)
    return content.drop(start).take(96).replace('\n', ' ').trim()
}
