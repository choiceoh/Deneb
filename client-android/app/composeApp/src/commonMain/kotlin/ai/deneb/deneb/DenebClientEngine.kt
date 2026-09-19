package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineGlance
import ai.deneb.deneb.generated.EngineServing
import ai.deneb.deneb.generated.EngineStatusResult
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Local serving engine surface of [DenebGatewayClient] (`miniapp.engine.*`).
 * An extension so the gateway client stays one facade while each RPC domain
 * lives in its own file (same split as [DenebClientUsage] et al.).
 *
 * Returns the engine's live reachability, the gateway's own routing verdict for
 * it, the days it measured itself, and the router's account of who actually
 * served. Returns null on a fetch failure so the screen can tell a real
 * "nothing configured" from a network error.
 *
 * [model] picks whose statistics come back (days, totals, diagnostics); null
 * means the model the engine serves now. Liveness and the router's split are
 * the engine's whichever model is picked.
 */
suspend fun DenebGatewayClient.fetchEngineStatus(model: String? = null): EngineStatusResult? = callRpc<EngineStatusResult>(
    "miniapp.engine.status",
    buildJsonObject { if (!model.isNullOrBlank()) put("model", model) },
)

/**
 * The engine's standing from state the gateway already holds — no probe, no
 * router call — cheap enough for the 더보기 tile to ask on every visit.
 */
suspend fun DenebGatewayClient.fetchEngineGlance(): EngineGlance? = callRpc<EngineGlance>("miniapp.engine.glance", buildJsonObject { })

/**
 * Which model the engine's production serves, which one it was told to serve,
 * and what its supervisor is doing about it (`miniapp.engine.serving`). The
 * gateway asks the fleet's head; an answer with available=false says why it
 * could not. Null only on a fetch failure.
 */
suspend fun DenebGatewayClient.fetchEngineServing(): EngineServing? = callRpc<EngineServing>("miniapp.engine.serving", buildJsonObject { })

/**
 * Tells production to serve [profile] (`miniapp.engine.select`). The fleet's
 * supervisor does the switch — minutes of downtime — so the answer is the
 * fleet's state right after the choice was written, not after the switch.
 * Null when the choice could not be written.
 */
suspend fun DenebGatewayClient.selectEngineModel(profile: String): EngineServing? = callRpc<EngineServing>(
    "miniapp.engine.select",
    buildJsonObject { put("profile", profile) },
)

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
