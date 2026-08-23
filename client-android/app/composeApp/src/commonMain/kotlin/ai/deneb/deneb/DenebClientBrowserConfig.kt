package ai.deneb.deneb

import ai.deneb.deneb.generated.BrowserConfigOut
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.Json
import kotlin.time.Clock
import kotlin.time.Duration
import kotlin.time.Duration.Companion.minutes
import kotlin.time.ExperimentalTime

private val browserRulesJson = Json { ignoreUnknownKeys = true }

/** Browser screen refresh cadence: file edits on the gateway land within minutes. */
private val BROWSER_RULES_REFRESH_TTL: Duration = 10.minutes

/**
 * Remote browser rules fetch: cache-then-network, like the work feed. The disk
 * cache seeds the registry at process start (see DenebGatewayClient.init);
 * [refreshBrowserRules] re-fetches at most once per TTL and installs the result.
 * A failed fetch keeps whatever is installed — remote rules are additive, so
 * stale is harmless and missing means built-ins only.
 */
private val browserRulesRefreshMutex = Mutex()

@Volatile
private var browserRulesLastFetchMs: Long = 0

internal fun decodeCachedBrowserRules(raw: String): BrowserConfigOut? = raw.trim().takeIf(String::isNotEmpty)?.let {
    runCatching { browserRulesJson.decodeFromString(BrowserConfigOut.serializer(), it) }.getOrNull()
}

internal fun seedBrowserRulesFromDisk(raw: String): Boolean {
    val out = decodeCachedBrowserRules(raw) ?: return false
    BrowserRuleRegistry.install(browserRemoteRulesFromWire(out))
    return true
}

@OptIn(ExperimentalTime::class)
internal suspend fun DenebGatewayClient.refreshBrowserRules(force: Boolean = false): Boolean {
    val now = Clock.System.now().toEpochMilliseconds()
    if (!force && now - browserRulesLastFetchMs < BROWSER_RULES_REFRESH_TTL.inWholeMilliseconds) {
        return true
    }
    return browserRulesRefreshMutex.withLock {
        // Another caller may have refreshed while this one waited on the mutex.
        val checked = Clock.System.now().toEpochMilliseconds()
        if (!force && checked - browserRulesLastFetchMs < BROWSER_RULES_REFRESH_TTL.inWholeMilliseconds) {
            return@withLock true
        }
        val out: BrowserConfigOut? = callRpc("miniapp.browser.config.get", kotlinx.serialization.json.buildJsonObject { })
        browserRulesLastFetchMs = checked
        val fetched = out ?: return@withLock false
        BrowserRuleRegistry.install(browserRemoteRulesFromWire(fetched))
        appSettings.setBrowserRemoteRulesJson(
            runCatching { browserRulesJson.encodeToString(BrowserConfigOut.serializer(), fetched) }.getOrDefault(""),
        )
        true
    }
}
