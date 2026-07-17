package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

private fun option(
    runs: Int = 0,
    input: Long = 0,
    output: Long = 0,
    cacheRead: Long = 0,
) = ModelOption(
    id = "kimi/k3[1m]",
    display = "k3[1m]",
    current = false,
    health = "online",
    runs24h = runs,
    inputTokens24h = input,
    outputTokens24h = output,
    cacheReadTokens24h = cacheRead,
)

class ConfigModelUsageLineTest {
    @Test
    fun formatsCompactTokenCounts() {
        assertEquals("872", formatTokenCount(872))
        assertEquals("45K", formatTokenCount(45_300))
        assertEquals("1.2M", formatTokenCount(1_234_567))
        assertEquals("2M", formatTokenCount(2_000_000))
        assertEquals("0", formatTokenCount(0))
    }

    @Test
    fun usageLineShowsCacheOnlyWhenPresent() {
        val cached = option(runs = 12, input = 1_234_567, output = 45_300, cacheRead = 900_000)
        assertEquals("24h · 12회 · 입력 1.2M(캐시 900K) · 출력 45K", usage24hLine(cached))

        val uncached = cached.copy(cacheReadTokens24h = 0)
        assertEquals("24h · 12회 · 입력 1.2M · 출력 45K", usage24hLine(uncached))
    }

    @Test
    fun unusedModelCarriesNoUsageLine() {
        assertNull(usage24hLine(option()))
    }
}
