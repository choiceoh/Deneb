package ai.deneb

internal const val WORK_FEED_REF_TYPE = "workfeed"

/**
 * Resolves a gateway push or dashboard reference into a work-feed item ID.
 * Blank refs and other ref types deliberately fall back to the caller's legacy
 * navigation behavior.
 */
internal fun workFeedItemId(refType: String?, refId: String?): String? {
    if (!refType?.trim().equals(WORK_FEED_REF_TYPE, ignoreCase = true)) return null
    return refId?.trim()?.takeIf(String::isNotEmpty)
}

internal data class FeedItemOpenConsumption(
    val openedItemId: String?,
    val pendingItemId: String?,
)

/**
 * Consumes a pending item ID only after that item is present in the feed.
 * Re-running with the returned pending ID is therefore one-shot and safe across
 * recomposition while a sync is still loading the target card.
 */
internal fun consumeFeedItemOpen(
    pendingItemId: String?,
    availableItemIds: Iterable<String>,
): FeedItemOpenConsumption {
    val id = pendingItemId?.trim()?.takeIf(String::isNotEmpty)
        ?: return FeedItemOpenConsumption(openedItemId = null, pendingItemId = null)
    return if (availableItemIds.any { it == id }) {
        FeedItemOpenConsumption(openedItemId = id, pendingItemId = null)
    } else {
        FeedItemOpenConsumption(openedItemId = null, pendingItemId = id)
    }
}
