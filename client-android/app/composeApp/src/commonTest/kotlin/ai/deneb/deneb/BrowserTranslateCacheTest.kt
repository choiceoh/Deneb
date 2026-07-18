package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class BrowserTranslateCacheTest {
    private fun upper(): suspend (List<String>, String) -> List<String>? = { segments, _ ->
        segments.map { it.uppercase() }
    }

    @Test
    fun missesRoundTripThenHitsServeLocallyInOrder() = runTest {
        val cache = BrowserTranslateCache()
        var calls = 0
        val counting: suspend (List<String>, String) -> List<String>? = { segments, lang ->
            calls++
            upper()(segments, lang)
        }

        assertEquals(listOf("A", "B"), cache.translate(listOf("a", "b"), "ko", counting))
        assertEquals(1, calls)

        // Full hit: no upstream call, order preserved (including reordering).
        assertEquals(listOf("B", "A"), cache.translate(listOf("b", "a"), "ko", counting))
        assertEquals(1, calls)

        // Partial hit: only the miss goes upstream.
        var lastUpstream: List<String>? = null
        val recording: suspend (List<String>, String) -> List<String>? = { segments, lang ->
            lastUpstream = segments
            counting(segments, lang)
        }
        assertEquals(listOf("A", "C", "B"), cache.translate(listOf("a", "c", "b"), "ko", recording))
        assertEquals(listOf("c"), lastUpstream)
        assertEquals(2, calls)
    }

    @Test
    fun duplicateSegmentsInOneBatchAreDeduplicatedUpstream() = runTest {
        val cache = BrowserTranslateCache()
        var lastUpstream: List<String>? = null
        val recording: suspend (List<String>, String) -> List<String>? = { segments, lang ->
            lastUpstream = segments
            upper()(segments, lang)
        }
        assertEquals(listOf("X", "X", "Y"), cache.translate(listOf("x", "x", "y"), "ko", recording))
        assertEquals(listOf("x", "y"), lastUpstream)
    }

    @Test
    fun upstreamFailureDropsTheBatchWithoutCachingPartials() = runTest {
        val cache = BrowserTranslateCache()
        assertNull(cache.translate(listOf("a"), "ko") { _, _ -> null })
        assertNull(cache.translate(listOf("a"), "ko") { _, _ -> listOf("too", "many") })
        assertEquals(0, cache.size())
    }

    @Test
    fun targetLanguageIsPartOfTheKey() = runTest {
        val cache = BrowserTranslateCache()
        cache.translate(listOf("a"), "ko", upper())
        var calls = 0
        cache.translate(listOf("a"), "en") { segments, _ ->
            calls++
            segments
        }
        assertEquals(1, calls)
    }

    @Test
    fun evictsLeastRecentlyUsedByCharBudget() = runTest {
        // Each entry costs 13 chars: key "ko aaaaa" (8) + value "AAAAA" (5).
        val cache = BrowserTranslateCache(maxChars = 30)
        cache.translate(listOf("aaaaa"), "ko", upper()) // 13 chars
        cache.translate(listOf("bbbbb"), "ko", upper()) // 26 chars
        cache.translate(listOf("aaaaa"), "ko", upper()) // touch a → b is now LRU
        cache.translate(listOf("ccccc"), "ko", upper()) // 39 > 30 → evicts b
        var upstreamForA = false
        var upstreamForB = false
        cache.translate(listOf("aaaaa"), "ko") { _, _ ->
            upstreamForA = true
            listOf("AAAAA")
        }
        cache.translate(listOf("bbbbb"), "ko") { _, _ ->
            upstreamForB = true
            listOf("BBBBB")
        }
        assertEquals(false, upstreamForA)
        assertEquals(true, upstreamForB)
    }

    @Test
    fun oversizeSegmentBypassesCacheWithoutWipingIt() = runTest {
        val cache = BrowserTranslateCache(maxChars = 30)
        cache.translate(listOf("aaaaa"), "ko", upper())
        cache.translate(listOf("x".repeat(100)), "ko", upper())
        assertEquals(1, cache.size())
    }

    @Test
    fun persistenceSeedsEmptyCacheAndThrottlesSaves() = runTest {
        // Threshold 20: the first stored batch (cost 13) does not save; the
        // second (cumulative 26) does. Budget 30 keeps only the newest entries.
        val cache = BrowserTranslateCache(maxChars = 200, persistThresholdChars = 20, persistBudgetChars = 30)
        var saved: Map<String, String>? = null
        cache.attachPersistence(load = { mapOf("ko seed" to "SEED") }, save = { saved = it })

        // Seeded entry serves without an upstream call.
        var upstream = false
        assertEquals(
            listOf("SEED"),
            cache.translate(listOf("seed"), "ko") { s, _ ->
                upstream = true
                s
            },
        )
        assertEquals(false, upstream)

        cache.translate(listOf("aaaaa"), "ko", upper()) // unsaved 13 < 20 → no save
        assertNull(saved)
        cache.translate(listOf("bbbbb"), "ko", upper()) // unsaved 26 ≥ 20 → save
        val snapshot = saved ?: error("expected a persisted snapshot")
        // Budget 30 fits two 13-char entries; the seed (oldest) is dropped.
        assertEquals(setOf("ko aaaaa", "ko bbbbb"), snapshot.keys)
    }

    @Test
    fun persistenceLoadDoesNotOverrideLiveEntriesAndAttachIsIdempotent() = runTest {
        val cache = BrowserTranslateCache()
        cache.translate(listOf("live"), "ko", upper())
        cache.attachPersistence(load = { mapOf("ko live" to "STALE-DISK") }, save = { })
        var upstream = false
        val out = cache.translate(listOf("live"), "ko") { s, _ ->
            upstream = true
            s
        }
        assertEquals(listOf("LIVE"), out)
        assertEquals(false, upstream)

        // Second attach is ignored (no re-seed, first save sink kept).
        cache.attachPersistence(load = { mapOf("ko other" to "X") }, save = { })
        assertEquals(1, cache.size())
    }
}
