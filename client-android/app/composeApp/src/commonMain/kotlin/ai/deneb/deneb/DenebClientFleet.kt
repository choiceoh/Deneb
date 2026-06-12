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
import kotlinx.serialization.json.Json
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
internal data class FleetModel(val name: String = "", val sizeBytes: Long = 0)

@Serializable
internal data class FleetNode(
    val name: String,
    val role: String = "",
    val reachable: Boolean = false,
    val error: String? = null,
    val metrics: FleetNodeMetrics = FleetNodeMetrics(),
    val models: List<FleetModel> = emptyList(),
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
internal data class FleetVllm(
    val gpuMemoryUtilization: Double? = null,
    val maxModelLen: Int? = null,
    val maxNumSeqs: Int? = null,
)

@Serializable
internal data class FleetRecipe(
    val name: String,
    val description: String = "",
    val node: String = "",
    val port: Int = 0,
    val vllm: FleetVllm? = null,
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

@Serializable
internal data class FleetHFModel(
    val id: String,
    val downloads: Long = 0,
    val likes: Long = 0,
    val params: Long = 0, // safetensors parameter count; 0 = unknown (GGUF)
    val gated: Boolean = false,
)

@Serializable
private data class FleetHFSearchResponse(val models: List<FleetHFModel> = emptyList())

@Serializable
internal data class FleetHFInfo(
    val repo: String = "",
    val sizeBytes: Long = 0,
    val files: Int = 0,
    val gated: Boolean = false,
)

// fleetJson decodes SparkFleet (a Go server) responses: Go marshals nil
// slices/maps as JSON null, so an unreachable node arrives as
// "services": null / "gpus": null. coerceInputValues turns those nulls into
// the field defaults instead of failing the whole decode (which left the
// 노드 tab stuck on "불러오는 중" the moment any node carried a null array).
internal val fleetJson = Json {
    ignoreUnknownKeys = true
    isLenient = true
    coerceInputValues = true
}

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
    fleetGetText("/api/state")?.let { runCatching { fleetJson.decodeFromString<FleetState>(it) }.getOrNull() }

internal suspend fun DenebGatewayClient.fleetRecipes(): List<FleetRecipe>? =
    fleetGetText("/api/recipes")?.let { runCatching { fleetJson.decodeFromString<List<FleetRecipe>>(it) }.getOrNull() }

internal suspend fun DenebGatewayClient.fleetJobs(): List<FleetJob>? =
    fleetGetText("/api/jobs")?.let { runCatching { fleetJson.decodeFromString<List<FleetJob>>(it) }.getOrNull() }

internal suspend fun DenebGatewayClient.fleetJob(id: String): FleetJob? =
    fleetGetText("/api/jobs/${id.encodeURLParameter()}")?.let {
        runCatching { fleetJson.decodeFromString<FleetJob>(it) }.getOrNull()
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
    overrides: FleetVllm? = null,
    onJob: (String) -> Unit = {},
): String? {
    val body = buildJsonObject {
        put("recipe", recipe)
        put("action", action)
        // Per-launch memory-budget tweaks — SparkFleet applies them to a clone,
        // so the recipe file itself never changes.
        if (action == "launch" && overrides != null &&
            (overrides.gpuMemoryUtilization != null || overrides.maxModelLen != null || overrides.maxNumSeqs != null)
        ) {
            put(
                "overrides",
                buildJsonObject {
                    overrides.gpuMemoryUtilization?.let { put("gpuMemoryUtilization", it) }
                    overrides.maxModelLen?.let { put("maxModelLen", it) }
                    overrides.maxNumSeqs?.let { put("maxNumSeqs", it) }
                },
            )
        }
    }.toString()
    val (ok, err) = fleetPost("/api/recipes/action", body)
    if (err != null) return err
    ok?.let { text ->
        runCatching { fleetJson.decodeFromString<FleetJobIdResponse>(text) }.getOrNull()
            ?.jobId?.takeIf { it.isNotBlank() }?.let(onJob)
    }
    return null
}

internal suspend fun DenebGatewayClient.fleetHFSearch(q: String): List<FleetHFModel>? =
    fleetGetText("/api/hf/search?q=${q.encodeURLParameter()}")?.let {
        runCatching { fleetJson.decodeFromString<FleetHFSearchResponse>(it).models }.getOrNull()
    }

internal suspend fun DenebGatewayClient.fleetHFInfo(repo: String): FleetHFInfo? =
    fleetGetText("/api/hf/info?repo=${repo.encodeURLParameter()}")?.let {
        runCatching { fleetJson.decodeFromString<FleetHFInfo>(it) }.getOrNull()
    }

/** Start a HuggingFace download on a node. Error message, or null on success. */
internal suspend fun DenebGatewayClient.fleetDownloadModel(node: String, repo: String, onJob: (String) -> Unit = {}): String? {
    val body = buildJsonObject {
        put("node", node)
        put("repo", repo)
    }.toString()
    val (ok, err) = fleetPost("/api/models/download", body)
    if (err != null) return err
    ok?.let { text ->
        runCatching { fleetJson.decodeFromString<FleetJobIdResponse>(text) }.getOrNull()
            ?.jobId?.takeIf { it.isNotBlank() }?.let(onJob)
    }
    return null
}

/** Cancel a running job. Error message, or null on success. */
internal suspend fun DenebGatewayClient.fleetCancelJob(id: String): String? =
    fleetPost("/api/jobs/${id.encodeURLParameter()}/cancel", "{}").second
