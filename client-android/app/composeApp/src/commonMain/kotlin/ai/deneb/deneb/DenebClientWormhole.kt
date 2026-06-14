package ai.deneb.deneb

import ai.deneb.deneb.generated.WormholeStatusOut
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
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
