package ai.deneb.deneb

import kotlinx.serialization.KSerializer
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put

/** Shared codec for settings-backed caches scoped to one gateway/account.
 *  [payloadKey] keeps each cache's existing on-disk JSON shape intact. */
private val ownedCacheJson = Json { ignoreUnknownKeys = true }

internal fun <T> encodeOwnedCache(
    owner: String,
    payloadKey: String,
    serializer: KSerializer<T>,
    value: T,
): String = buildJsonObject {
    put("owner", owner)
    put(payloadKey, ownedCacheJson.encodeToJsonElement(serializer, value))
}.toString()

internal fun <T : Any> decodeOwnedCache(
    json: String,
    expectedOwner: String,
    payloadKey: String,
    serializer: KSerializer<T>,
): T? = runCatching {
    val envelope = ownedCacheJson.parseToJsonElement(json).jsonObject
    val ownerElement = envelope["owner"]
    // Owner was added after the first cache format; missing means legacy unscoped.
    val actualOwner = if (ownerElement == null) {
        ""
    } else {
        (ownerElement as? JsonPrimitive)?.takeIf { it.isString }?.content
            ?: return@runCatching null
    }
    if (actualOwner != expectedOwner) return@runCatching null
    val payload = envelope[payloadKey] ?: return@runCatching null
    ownedCacheJson.decodeFromJsonElement(serializer, payload)
}.getOrNull()

/** Reusable bounded-list policy shared by mail, feed, calendar, and approvals. */
internal class OwnedListCacheCodec<T : Any>(
    private val payloadKey: String,
    elementSerializer: KSerializer<T>,
    private val maxEntries: Int,
) {
    private val serializer = ListSerializer(elementSerializer)

    fun encode(values: List<T>, owner: String): String = encodeOwnedCache(owner, payloadKey, serializer, values.take(maxEntries))

    fun decode(json: String, expectedOwner: String): List<T>? = decodeOwnedCache(json, expectedOwner, payloadKey, serializer)
        ?.take(maxEntries)
        ?.takeIf { it.isNotEmpty() }
}
