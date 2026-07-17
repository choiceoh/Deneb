package ai.deneb.deneb

import ai.deneb.ui.chat.WorkFeedItem

// --- Work-feed (업무 home) cache (cache-then-network) ----------------------------
// Mirrors the mail cache (DenebClientMail): the recent feed is persisted, owner-
// fingerprinted, so the home renders the last-known briefing instantly on cold
// start AND when the gateway is unreachable — the offline-first launcher shell.
// The network refresh overwrites with the authoritative list. Reuses
// [mailCacheOwner] (the url#token account fingerprint — not mail-specific) so a
// prior account's feed can't render under new credentials.

// Cap the cached feed so the settings entry stays small; the home only ever shows
// a few days of recent items.
private const val WORK_FEED_CACHE_MAX = 80

private val workFeedCacheCodec = OwnedListCacheCodec("items", WorkFeedItem.serializer(), WORK_FEED_CACHE_MAX)

internal fun encodeWorkFeedCache(items: List<WorkFeedItem>, owner: String): String = workFeedCacheCodec.encode(items, owner)

internal fun decodeWorkFeedCache(json: String, expectedOwner: String): List<WorkFeedItem>? = workFeedCacheCodec.decode(json, expectedOwner)

internal fun DenebGatewayClient.loadCachedWorkFeed(): List<WorkFeedItem>? = loadCachedOrClear(
    appSettings.getCachedWorkFeed(),
    { decodeWorkFeedCache(it, mailCacheOwner(gatewayUrl, clientToken)) },
    appSettings::removeCachedWorkFeed,
)

internal fun DenebGatewayClient.storeCachedWorkFeed(items: List<WorkFeedItem>) {
    appSettings.putCachedWorkFeed(encodeWorkFeedCache(items, mailCacheOwner(gatewayUrl, clientToken)))
}
