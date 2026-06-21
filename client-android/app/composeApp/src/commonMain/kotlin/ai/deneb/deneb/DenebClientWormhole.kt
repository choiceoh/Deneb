package ai.deneb.deneb

import ai.deneb.deneb.generated.WormholeStatusOut
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.put

/**
 * wormhole router surface of [DenebGatewayClient] (`miniapp.wormhole.*`): the
 * management tab reads the router's live status and flips its global feature
 * flags. The gateway reads the wormhole config file and probes its /health; a
 * toggle is written to that file and hot-reloaded by wormhole — no restart.
 */

/** wormhole status: reachability + configured models + feature flags. Null on a
 *  fetch failure so the tab can offer retry instead of a misleading empty state. */
suspend fun DenebGatewayClient.fetchWormholeStatus(): WormholeStatusOut? = callRpc<WormholeStatusOut>("miniapp.wormhole.status", buildJsonObject {})

/** Toggle a wormhole feature (`localOnly` or `effortRouting`). Written to the
 *  config and hot-reloaded by wormhole; returns true on success. */
suspend fun DenebGatewayClient.setWormholeFeature(feature: String, enabled: Boolean): Boolean = callRpc<JsonObject>(
    "miniapp.wormhole.set_feature",
    buildJsonObject {
        put("feature", feature)
        put("enabled", enabled)
    },
) != null

/** Outcome of a key rotation: [ok] the secret was written, [valid] the model
 *  authenticated with the new key on the post-write probe, [status] that probe's
 *  HTTP status (0 = transport error / not probed). */
data class WormholeKeyResult(val ok: Boolean, val valid: Boolean, val status: Int)

/** Rotate a cloud model's upstream key. The gateway writes it into wormhole's
 *  secrets.env (hot-reloaded by wormhole, no restart) and probes the model to
 *  validate. Returns the outcome, or null when the RPC is rejected (e.g. the model
 *  pins a literal key not yet migrated, or wormhole is unreachable). */
suspend fun DenebGatewayClient.setWormholeKey(model: String, key: String): WormholeKeyResult? {
    val r = callRpc<JsonObject>(
        "miniapp.wormhole.set_key",
        buildJsonObject {
            put("model", model)
            put("key", key)
        },
    ) ?: return null
    fun flag(k: String) = (r[k] as? JsonPrimitive)?.booleanOrNull ?: false
    fun num(k: String) = (r[k] as? JsonPrimitive)?.intOrNull ?: 0
    return WormholeKeyResult(ok = flag("ok"), valid = flag("valid"), status = num("status"))
}
