package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

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
 * Storage + LRU eviction live in AppSettings (encrypted at rest).
 */

// Skip caching a transcript whose serialized text exceeds this — a runaway
// session shouldn't bloat the settings store; it simply stays network-only.
private const val TX_CACHE_MAX_CHARS = 256 * 1024

private val txCacheJson = Json { ignoreUnknownKeys = true }

@Serializable
private data class CachedTxMsg(val role: String, val content: String, val ts: Long = 0)

private val txCacheSerializer = ListSerializer(CachedTxMsg.serializer())

/** Cached transcript for [key] as History rows, or null when absent/undecodable.
 *  Text-only (no attachments) — enough to render the bubbles instantly. */
internal fun DenebGatewayClient.loadCachedTranscript(key: String): List<History>? {
    val json = appSettings.getCachedTranscript(key) ?: return null
    val msgs = runCatching { txCacheJson.decodeFromString(txCacheSerializer, json) }.getOrNull() ?: return null
    if (msgs.isEmpty()) return null
    return msgs.map {
        History(
            role = if (it.role == "user") History.Role.USER else History.Role.ASSISTANT,
            content = it.content,
            timestampMs = it.ts,
        )
    }
}

/** Persist [transcript] (text-only) for [key]. Blank-content rows (e.g. image-only
 *  proactive cards) are dropped; an all-blank transcript clears the slot. */
internal fun DenebGatewayClient.storeCachedTranscript(key: String, transcript: List<History>) {
    val msgs = transcript.mapNotNull { h ->
        if (h.content.isBlank()) return@mapNotNull null
        CachedTxMsg(
            role = if (h.role == History.Role.USER) "user" else "assistant",
            content = h.content,
            ts = h.timestampMs,
        )
    }
    if (msgs.isEmpty()) {
        appSettings.removeCachedTranscript(key)
        return
    }
    val json = txCacheJson.encodeToString(txCacheSerializer, msgs)
    if (json.length > TX_CACHE_MAX_CHARS) return
    appSettings.putCachedTranscript(key, json)
}
