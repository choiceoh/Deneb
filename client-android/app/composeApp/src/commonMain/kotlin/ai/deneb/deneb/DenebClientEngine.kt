package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineStatusResult
import kotlinx.serialization.json.buildJsonObject

/**
 * Local serving engine surface of [DenebGatewayClient] (`miniapp.engine.status`).
 * An extension so the gateway client stays one facade while each RPC domain
 * lives in its own file (same split as [DenebClientUsage] et al.).
 *
 * Returns the engine's live reachability, the days it measured itself, and the
 * router's own account of who actually served. Returns null on a fetch failure
 * so the screen can tell a real "nothing configured" from a network error.
 */
suspend fun DenebGatewayClient.fetchEngineStatus(): EngineStatusResult? = callRpc<EngineStatusResult>("miniapp.engine.status", buildJsonObject { })
