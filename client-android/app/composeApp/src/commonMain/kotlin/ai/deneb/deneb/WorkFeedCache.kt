package ai.deneb.deneb

import ai.deneb.ui.chat.WorkFeedItem
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

// --- Work-feed (업무 home) cache (cache-then-network) ----------------------------
// Mirrors the mail cache (DenebClientMail): the recent feed is persisted, owner-
// fingerprinted, so the home renders the last-known briefing instantly on cold
// start AND when the gateway is unreachable — the offline-first launcher shell.
// The network refresh overwrites with the authoritative list. Reuses
// [mailCacheOwner] (the url#token account fingerprint — not mail-specific) so a
// prior account's feed can't render under new credentials.

private val workFeedCacheJson = Json { ignoreUnknownKeys = true }

// Cap the cached feed so the settings entry stays small; the home only ever shows
// a few days of recent items.
private const val WORK_FEED_CACHE_MAX = 80

@Serializable
private data class WorkFeedCacheEnvelope(
    val owner: String = "",
    val items: List<WorkFeedItem> = emptyList(),
)

internal fun encodeWorkFeedCache(items: List<WorkFeedItem>, owner: String): String = workFeedCacheJson.encodeToString(
    WorkFeedCacheEnvelope(owner = owner, items = items.take(WORK_FEED_CACHE_MAX)),
)

internal fun decodeWorkFeedCache(json: String, expectedOwner: String): List<WorkFeedItem>? = runCatching { workFeedCacheJson.decodeFromString<WorkFeedCacheEnvelope>(json) }
    .getOrNull()
    ?.takeIf { it.owner == expectedOwner }
    ?.items
    ?.takeIf { it.isNotEmpty() }

internal fun DenebGatewayClient.loadCachedWorkFeed(): List<WorkFeedItem>? {
    val json = appSettings.getCachedWorkFeed() ?: return null
    return decodeWorkFeedCache(json, mailCacheOwner(gatewayUrl, clientToken))
}

internal fun DenebGatewayClient.storeCachedWorkFeed(items: List<WorkFeedItem>) {
    appSettings.putCachedWorkFeed(encodeWorkFeedCache(items, mailCacheOwner(gatewayUrl, clientToken)))
}
