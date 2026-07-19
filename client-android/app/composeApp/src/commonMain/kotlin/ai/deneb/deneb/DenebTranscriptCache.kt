package ai.deneb.deneb

import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.stableTranscriptId
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer

/**
 * Local transcript cache (cache-then-network): a reopened session renders
 * instantly from the encrypted settings store while [DenebGatewayClient]'s
 * loadTranscriptGuarded revalidates over the network and overwrites with the
 * authoritative copy. The network fetch is gzip/304-friendly later; this layer
 * is the perceived-speed win (no spinner on reopen) and saves a full fetch when
 * the user just glances at a session.
 *
 * Text-only by design: attachment bytes (base64 images) are NOT cached — they'd
 * bloat the settings store, and the network fetch restores them moments later.
 * Storage + LRU eviction live in AppSettings (encrypted at rest), while the
 * shared owner envelope prevents another gateway/account's cache from rendering.
 */

// Skip caching a transcript whose serialized text exceeds this — a runaway
// session shouldn't bloat the settings store; it simply stays network-only.
internal const val TX_CACHE_MAX_CHARS = 256 * 1024

@Serializable
private data class CachedTxMsg(val role: String, val content: String, val ts: Long = 0)

private val txCacheSerializer = ListSerializer(CachedTxMsg.serializer())
private const val TX_CACHE_PAYLOAD_KEY = "messages"

/** Decode one persisted transcript cache payload without touching settings. */
internal fun decodeCachedTranscript(json: String, expectedOwner: String): List<History>? {
    val msgs = decodeOwnedCache(json, expectedOwner, TX_CACHE_PAYLOAD_KEY, txCacheSerializer) ?: return null
    if (msgs.isEmpty()) return null
    return msgs.map {
        val role = if (it.role == "user") History.Role.USER else History.Role.ASSISTANT
        History(
            id = stableTranscriptId(role, it.content, it.ts),
            role = role,
            content = it.content,
            timestampMs = it.ts,
        )
    }
}

/** Encode a bounded text-only cache payload, or null when it should be evicted. */
internal fun encodeCachedTranscript(transcript: List<History>, owner: String): String? {
    val msgs = transcript.mapNotNull { h ->
        if (h.content.isBlank()) return@mapNotNull null
        CachedTxMsg(
            role = if (h.role == History.Role.USER) "user" else "assistant",
            content = h.content,
            ts = h.timestampMs,
        )
    }
    if (msgs.isEmpty()) return null
    return encodeOwnedCache(owner, TX_CACHE_PAYLOAD_KEY, txCacheSerializer, msgs)
        .takeIf { it.length <= TX_CACHE_MAX_CHARS }
}

/** Cached transcript for [key] as History rows, or null when absent/undecodable.
 *  Text-only (no attachments) — enough to render the bubbles instantly. */
internal fun DenebGatewayClient.loadCachedTranscript(key: String): List<History>? = loadCachedOrClear(
    appSettings.getCachedTranscript(key),
    { decodeCachedTranscript(it, mailCacheOwner(gatewayUrl, clientToken)) },
    { appSettings.removeCachedTranscript(key) },
)

/** Persist [transcript] (text-only) for [key]. Blank-content rows (e.g. image-only
 *  proactive cards) are dropped; an all-blank transcript clears the slot. */
internal fun DenebGatewayClient.storeCachedTranscript(key: String, transcript: List<History>) {
    val json = encodeCachedTranscript(transcript, mailCacheOwner(gatewayUrl, clientToken))
    if (json == null) {
        // No cache-worthy text remains, or the authoritative transcript outgrew
        // the budget. Drop an existing entry instead of rendering a stale snapshot.
        appSettings.removeCachedTranscript(key)
        return
    }
    appSettings.putCachedTranscript(key, json)
}

/** Evict the cached transcript for [key] (session deleted, emptied, or too large). */
internal fun DenebGatewayClient.removeCachedTranscript(key: String) {
    appSettings.removeCachedTranscript(key)
}
