package ai.deneb.deneb

import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsText
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Fleet surface of [DenebGatewayClient]: manage the SparkFleet control plane
 * (the sibling service that launches/monitors the GPU containers Deneb's
 * models run on) from the app. All calls go through the gateway's
 * authenticated passthrough (`/api/v1/fleet/…` → SparkFleet REST), because the
 * app may be off the tailnet that SparkFleet binds to. Read calls return null
 * on any failure so the tab degrades to its error state; writes return a
 * user-facing error message or null on success (the [rpcWrite] convention).
 */

// --- wire types (subset of SparkFleet's responses; unknown keys ignored) ---

@Serializable
internal data class FleetGpu(val index: Int = 0, val utilPct: Int? = null, val tempC: Int? = null)

@Serializable
internal data class FleetMemory(val totalKB: Long = 0, val availableKB: Long = 0)

@Serializable
internal data class FleetDisk(val path: String = "", val totalKB: Long = 0, val usedKB: Long = 0, val usePct: Int = 0)

@Serializable
internal data class FleetServiceHealth(val name: String = "", val ok: Boolean = false)

@Serializable
internal data class FleetNodeMetrics(
    val gpus: List<FleetGpu> = emptyList(),
    val memory: FleetMemory? = null,
    val disks: List<FleetDisk> = emptyList(),
    val services: List<FleetServiceHealth> = emptyList(),
)

@Serializable
internal data class FleetNode(
    val name: String,
    val role: String = "",
    val reachable: Boolean = false,
    val error: String? = null,
    val metrics: FleetNodeMetrics = FleetNodeMetrics(),
)

@Serializable
internal data class FleetState(val nodes: List<FleetNode> = emptyList())

@Serializable
internal data class FleetRecipeStatus(
    val running: Boolean = false,
    val weightsPresent: Boolean = false,
    val node: String = "",
)

@Serializable
internal data class FleetRecipe(
    val name: String,
    val description: String = "",
    val node: String = "",
    val port: Int = 0,
    val status: FleetRecipeStatus = FleetRecipeStatus(),
)

@Serializable
internal data class FleetJob(
    val id: String,
    val title: String = "",
    val state: String = "", // running | done | failed
    val log: String = "",
    val startedAt: String = "",
    val endedAt: String = "",
)

@Serializable
private data class FleetJobIdResponse(val jobId: String = "")

// --- transport helpers -----------------------------------------------------

private suspend fun DenebGatewayClient.fleetGetText(path: String): String? {
    if (clientToken.isEmpty() || gatewayUrl.isBlank()) return null
    return runCatching {
        val resp = http.get("$gatewayUrl/api/v1/fleet$path") {
            header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
        }
        if (resp.status.isSuccess()) resp.bodyAsText() else null
    }.getOrNull()
}

/** POST returning the upstream's error text on failure, null on success. */
private suspend fun DenebGatewayClient.fleetPost(path: String, jsonBody: String): Pair<String?, String?> {
    if (clientToken.isEmpty()) return null to "게이트웨이에 연결되어 있지 않습니다."
    return runCatching {
        val resp = http.post("$gatewayUrl/api/v1/fleet$path") {
            header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
            contentType(ContentType.Application.Json)
            setBody(jsonBody)
        }
        val text = resp.bodyAsText()
        if (resp.status.isSuccess()) text to null else null to text.ifBlank { "요청 실패 (${resp.status.value})" }
    }.getOrElse { null to "플릿에 연결하지 못했습니다: ${it.message ?: "전송 오류"}" }
}

// --- reads ------------------------------------------------------------------

internal suspend fun DenebGatewayClient.fleetState(): FleetState? =
    fleetGetText("/api/state")?.let { runCatching { jsonCodec.decodeFromString<FleetState>(it) }.getOrNull() }

internal suspend fun DenebGatewayClient.fleetRecipes(): List<FleetRecipe>? =
    fleetGetText("/api/recipes")?.let { runCatching { jsonCodec.decodeFromString<List<FleetRecipe>>(it) }.getOrNull() }

internal suspend fun DenebGatewayClient.fleetJobs(): List<FleetJob>? =
    fleetGetText("/api/jobs")?.let { runCatching { jsonCodec.decodeFromString<List<FleetJob>>(it) }.getOrNull() }

internal suspend fun DenebGatewayClient.fleetJob(id: String): FleetJob? =
    fleetGetText("/api/jobs/${id.encodeURLParameter()}")?.let {
        runCatching { jsonCodec.decodeFromString<FleetJob>(it) }.getOrNull()
    }

// --- writes -----------------------------------------------------------------

/**
 * Launch / stop / restart a recipe (or pull its weights). Returns a user-facing
 * error message, or null on success; [onJob] receives the job id when the
 * action runs as a background job (launch/pull).
 */
internal suspend fun DenebGatewayClient.fleetRecipeAction(
    recipe: String,
    action: String,
    onJob: (String) -> Unit = {},
): String? {
    val body = buildJsonObject {
        put("recipe", recipe)
        put("action", action)
    }.toString()
    val (ok, err) = fleetPost("/api/recipes/action", body)
    if (err != null) return err
    ok?.let { text ->
        runCatching { jsonCodec.decodeFromString<FleetJobIdResponse>(text) }.getOrNull()
            ?.jobId?.takeIf { it.isNotBlank() }?.let(onJob)
    }
    return null
}
