package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineGlance
import ai.deneb.deneb.generated.EngineStatusResult
import kotlinx.serialization.json.buildJsonObject

/**
 * Local serving engine surface of [DenebGatewayClient] (`miniapp.engine.*`).
 * An extension so the gateway client stays one facade while each RPC domain
 * lives in its own file (same split as [DenebClientUsage] et al.).
 *
 * Returns the engine's live reachability, the gateway's own routing verdict for
 * it, the days it measured itself, and the router's account of who actually
 * served. Returns null on a fetch failure so the screen can tell a real
 * "nothing configured" from a network error.
 */
suspend fun DenebGatewayClient.fetchEngineStatus(): EngineStatusResult? = callRpc<EngineStatusResult>("miniapp.engine.status", buildJsonObject { })

/**
 * The engine's standing from state the gateway already holds — no probe, no
 * router call — cheap enough for the 더보기 tile to ask on every visit.
 */
suspend fun DenebGatewayClient.fetchEngineGlance(): EngineGlance? = callRpc<EngineGlance>("miniapp.engine.glance", buildJsonObject { })

/**
 * One readiness change of the local engine, as the gateway pushes it on the
 * events stream (`kind=engine`, a state frame — never a notification). The
 * engine screen and the 더보기 tile refetch on it, so the moment a turn's
 * routing changes the phone says so without a pull-to-refresh.
 */
internal data class EnginePush(
    val down: Boolean,
    val endpoint: String,
    val reason: String,
    val sinceMs: Long,
    val models: List<String>,
    val downForSec: Long,
) {
    companion object {
        fun fromPushData(data: Map<String, String>): EnginePush = EnginePush(
            down = data["down"] == "true",
            endpoint = data["endpoint"].orEmpty(),
            reason = data["reason"].orEmpty(),
            sinceMs = data["sinceMs"]?.toLongOrNull() ?: 0L,
            models = data["models"].orEmpty().split(',').map { it.trim() }.filter { it.isNotEmpty() },
            downForSec = data["downForSec"]?.toLongOrNull() ?: 0L,
        )
    }
}
