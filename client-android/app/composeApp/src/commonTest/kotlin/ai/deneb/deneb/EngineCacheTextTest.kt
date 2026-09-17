package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineDay
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class EngineCacheTextTest {
    @Test
    fun oldGatewayOrHistoryCannotTurnRequestCountsIntoTokenReuse() {
        val day = Json.decodeFromString<EngineDay>("""{"day":"2026-09-17","promptCacheHitRatio":0.16,"cachedPromptTokens":126}""")
        assertEquals("미측정", engineTokenCacheText(day.promptCacheMeasured, day.promptCacheHitRatio, day.cachedPromptTokens, day.cachePromptTokens))
    }

    @Test
    fun measuredZeroAndMissingMeasurementsDiffer() {
        assertTrue(engineTokenCacheText(true, 0.0, 0, 1000).startsWith("0%"))
        assertEquals("미측정", engineTokenCacheText(false, 0.0, 0, 0))
        assertTrue(engineRequestCacheText(8.0 / 130, 8, 130).contains("8 / 130 건"))
        assertTrue(engineTokenCacheText(true, 0.59, 249600, 423319).endsWith("토큰"))
    }
}
