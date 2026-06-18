package ai.deneb.deneb

import ai.deneb.data.ScheduledTask
import ai.deneb.data.ServiceEntry
import ai.deneb.data.TaskStatus
import ai.deneb.data.TaskTrigger
import ai.deneb.deneb.generated.MiniappCronDetail
import ai.deneb.deneb.generated.SkillDetailResponse
import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.deneb.generated.SkillsLifecycleResponse
import ai.deneb.deneb.generated.SkillsListResponse
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_service_anthropic
import deneb.composeapp.generated.resources.ic_service_deepseek
import deneb.composeapp.generated.resources.ic_service_gemini
import deneb.composeapp.generated.resources.ic_service_gemma
import deneb.composeapp.generated.resources.ic_service_longcat
import deneb.composeapp.generated.resources.ic_service_mimo
import deneb.composeapp.generated.resources.ic_service_minimax
import deneb.composeapp.generated.resources.ic_service_mistral
import deneb.composeapp.generated.resources.ic_service_moonshot
import deneb.composeapp.generated.resources.ic_service_nvidia
import deneb.composeapp.generated.resources.ic_service_openai
import deneb.composeapp.generated.resources.ic_service_openai_compatible
import deneb.composeapp.generated.resources.ic_service_qwen
import deneb.composeapp.generated.resources.ic_service_step
import deneb.composeapp.generated.resources.ic_service_xai
import deneb.composeapp.generated.resources.ic_service_zai
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Admin surface of [DenebGatewayClient]: the model registry / role switcher
 * (`miniapp.models.*`), the chat-input model switcher entries, skills
 * (`miniapp.skills.list`), and cron jobs (`miniapp.crons.*`). Extensions so the
 * gateway client stays one facade while each RPC domain lives in its own file.
 */

// --- Model switcher → Deneb registry ------------------------------------
// models.set updates the gateway's default model, so switching here changes
// chat across the native app and every gateway-run automation.

fun DenebGatewayClient.refreshModelsAsync() {
    scope.launch { refreshModels() }
}

/** Pull the model registry from the gateway. Returns false when the RPC fails so
 *  a screen can render an error+retry instead of an indefinite skeleton (mirrors
 *  [refreshSkills]). Callers that don't care about the outcome can ignore it. */
suspend fun DenebGatewayClient.refreshModels(): Boolean {
    val payload = callRpc<ModelsPayload>("miniapp.models.list", buildJsonObject {}) ?: return false
    _denebModels.value = payload.sections
        .flatMap { it.models }
        .distinctBy { it.id }
        .map {
            ModelOption(
                id = it.id,
                display = it.display.ifBlank { it.label.ifBlank { it.id } },
                current = it.id == payload.current,
                health = it.health,
                custom = it.custom,
                deletable = it.deletable,
                unhealthy = it.unhealthy,
                note = it.note,
            )
        }
    _denebRoleModels.value = payload.roles.associate { it.role to it.model }
    _denebModelAdvisories.value = payload.advisories
    _denebMainHasVision.value = payload.mainHasVision
    return true
}

suspend fun DenebGatewayClient.setMainModel(id: String): Boolean = setRoleModel(id, "main")

/** Set the model for a specific role (main / lightweight / fallback). Returns
 *  false on a failed switch so the screen can surface it instead of a silent no-op. */
suspend fun DenebGatewayClient.setRoleModel(id: String, role: String): Boolean {
    val ok = callRpc<JsonObject>(
        "miniapp.models.set",
        buildJsonObject {
            put("id", id)
            put("role", role)
        },
    ) != null
    refreshModels()
    return ok
}

/** Add an OpenAI-compatible model by base URL + model name. The gateway stores
 *  it as a custom provider (api=openai) and reloads live, so the model appears
 *  in [DenebGatewayClient.denebModels] after the refresh. Returns false when the
 *  gateway rejects the endpoint/model so the screen can surface it instead of a
 *  silent no-op. */
suspend fun DenebGatewayClient.addCustomModel(endpoint: String, model: String): Boolean {
    val ok = callRpc<JsonObject>(
        "miniapp.models.add_custom",
        buildJsonObject {
            put("endpoint", endpoint)
            put("model", model)
        },
    ) != null
    if (ok) refreshModels()
    return ok
}

/** Remove a user-added custom model. The gateway resets any role bound to it
 *  back to the default. Returns false on failure. */
suspend fun DenebGatewayClient.deleteCustomModel(id: String): Boolean {
    val ok = callRpc<JsonObject>(
        "miniapp.models.delete_custom",
        buildJsonObject {
            put("id", id)
        },
    ) != null
    if (ok) refreshModels()
    return ok
}

// --- Chat-input model switcher → Deneb registry --------------------------
// the upstream chat input has a service/model switcher (ServiceSelector) driven by
// ChatUiState.availableServices. When this client is active, ChatViewModel
// sources that list from here so the switcher changes the gateway main model
// instead of the upstream local providers.

/** Gateway models as switcher entries, the ACTIVE workspace's model first (the
 *  ServiceSelector renders the first entry as selected). 챗봇 mode binds the chatbot
 *  role (falling back to the main model when no separate chatbot model is assigned);
 *  업무 mode binds the main model. */
fun DenebGatewayClient.denebServiceEntries(): List<ServiceEntry> {
    val models = _denebModels.value
    val mainId = models.firstOrNull { it.current }?.id
    val chatMode = !appSettings.isRecallEnabled()
    val selectedId = if (chatMode) {
        _denebRoleModels.value["chatbot"]?.takeIf { id -> models.any { it.id == id } } ?: mainId
    } else {
        mainId
    }
    val ordered = models.filter { it.id == selectedId } + models.filterNot { it.id == selectedId }
    return ordered.map { model ->
        ServiceEntry(
            instanceId = DENEB_MODEL_PREFIX + model.id,
            serviceId = "deneb",
            serviceName = model.display,
            modelId = model.id,
            icon = denebModelIcon(model),
        )
    }
}

/**
 * Best-effort brand icon for a gateway model. The gateway exposes no provider
 * field per model, so match well-known families on the id + display string.
 * Rendered monochrome (the switcher tints every icon), so these read as the
 * black-and-white brand marks rather than a single generic chip. Unknown or
 * local models fall back to the generic OpenAI-compatible mark.
 */
private fun denebModelIcon(model: ModelOption) = with("${model.id} ${model.display}".lowercase()) {
    when {
        contains("claude") || contains("anthropic") -> Res.drawable.ic_service_anthropic

        contains("gemma") -> Res.drawable.ic_service_gemma

        contains("gemini") -> Res.drawable.ic_service_gemini

        contains("gpt") || contains("openai") || contains("chatgpt") ||
            contains("o1-") || contains("o3") || contains("o4") -> Res.drawable.ic_service_openai

        contains("deepseek") -> Res.drawable.ic_service_deepseek

        contains("kimi") || contains("moonshot") -> Res.drawable.ic_service_moonshot

        contains("mistral") || contains("mixtral") || contains("magistral") ||
            contains("ministral") || contains("codestral") || contains("devstral") -> Res.drawable.ic_service_mistral

        contains("grok") || contains("x-ai") || contains("xai") -> Res.drawable.ic_service_xai

        contains("glm") || contains("zai") || contains("z-ai") || contains("chatglm") -> Res.drawable.ic_service_zai

        contains("minimax") -> Res.drawable.ic_service_minimax

        contains("longcat") -> Res.drawable.ic_service_longcat

        contains("llama") || contains("nemotron") || contains("nvidia") -> Res.drawable.ic_service_nvidia

        contains("qwen") || contains("qwq") || contains("tongyi") -> Res.drawable.ic_service_qwen

        contains("mimo") || contains("xiaomi") -> Res.drawable.ic_service_mimo

        contains("step") || contains("stepfun") -> Res.drawable.ic_service_step

        // Local/on-device runtimes (vLLM-served small models) keep the edge mark.
        else -> Res.drawable.ic_service_openai_compatible
    }
}

/** Switch the active workspace's model from a switcher tap (instanceId = prefixed
 *  model id). 챗봇 mode binds the chatbot role (the model 챗봇 turns actually use);
 *  업무 mode sets the main model. Mirrors [denebServiceEntries]' selection. */
fun DenebGatewayClient.selectDenebModelInstance(instanceId: String) {
    val modelId = instanceId.removePrefix(DENEB_MODEL_PREFIX)
    if (modelId.isBlank() || modelId == instanceId) return
    val role = if (appSettings.isRecallEnabled()) "main" else "chatbot"
    scope.launch { setRoleModel(modelId, role) }
}

// Prefix marking a switcher instanceId as a gateway model (vs. an upstream
// local-provider instance).
private const val DENEB_MODEL_PREFIX = "deneb-model:"

// --- Skills (read-only) → Settings Skills tab ---------------------------
// The native client doesn't know server-side skill paths; miniapp.skills.list
// resolves the workspace itself and returns the same skills the agent sees.

fun DenebGatewayClient.refreshSkillsAsync() {
    scope.launch { refreshSkills() }
}

/** Returns false on a failed load so the Skills tab can surface a retry
 *  instead of showing a misleading "no skills" empty state. */
suspend fun DenebGatewayClient.refreshSkills(): Boolean {
    val payload = callRpc<SkillsListResponse>("miniapp.skills.list", buildJsonObject {}) ?: return false
    _denebSkills.value = payload.skills
    return true
}

/** Self-evolution timeline for the Skills tab (genesis/evolve/review events,
 *  newest first). Null on transport failure so the tab can show a retry —
 *  an empty feed is a valid, distinct state ("no activity yet"). Pass
 *  [skillName] to narrow the feed to one skill (detail screen). */
suspend fun DenebGatewayClient.fetchSkillLifecycle(
    limit: Int = 60,
    skillName: String? = null,
): List<SkillLifecycleEvent>? = fetchSkillLifecycleResponse(limit = limit, skillName = skillName)?.events

suspend fun DenebGatewayClient.fetchSkillLifecycleResponse(
    limit: Int = 60,
    skillName: String? = null,
): SkillsLifecycleResponse? = callRpc<SkillsLifecycleResponse>(
    "miniapp.skills.lifecycle",
    buildJsonObject {
        put("limit", limit)
        if (!skillName.isNullOrBlank()) put("skillName", skillName)
    },
)

/** One skill's enriched row + SKILL.md body for the tap-through detail screen.
 *  Null on transport failure or unknown skill name. */
suspend fun DenebGatewayClient.fetchSkillDetail(name: String): SkillDetailResponse? = callRpc<SkillDetailResponse>(
    "miniapp.skills.detail",
    buildJsonObject { put("name", name) },
)

// --- Scheduler screen → Deneb cron --------------------------------------

/** Suspend refresh that reports success, for screens that want an error state. */
suspend fun DenebGatewayClient.loadScheduledTasks(): Boolean = refreshScheduledTasks()

/** Delete a cron, reporting success so the screen can confirm the delete landed
 *  before navigating away instead of popping back on a failed remove. */
suspend fun DenebGatewayClient.removeCron(id: String): Boolean {
    val ok = callRpc<JsonObject>("miniapp.crons.remove", buildJsonObject { put("id", id) }) != null
    refreshScheduledTasks()
    return ok
}

internal suspend fun DenebGatewayClient.refreshScheduledTasks(): Boolean {
    val payload = callRpc<CronListPayload>(
        "miniapp.crons.list",
        buildJsonObject { put("includeDisabled", true) },
    ) ?: return false
    _denebScheduledTasks.value = payload.jobs
        .filter { it.id.isNotBlank() }
        .map { j ->
            ScheduledTask(
                id = j.id,
                description = j.name.ifBlank { j.id },
                prompt = j.payloadPreview,
                scheduledAtEpochMs = j.nextRunAtMs,
                createdAtEpochMs = 0,
                cron = j.schedule.ifBlank { null },
                trigger = TaskTrigger.CRON,
                status = TaskStatus.PENDING,
                lastResult = j.lastError.ifBlank { null },
                consecutiveFailures = j.consecutiveErrors,
            )
        }
    return true
}

/** Trigger a cron job immediately (`miniapp.crons.run`). */
suspend fun DenebGatewayClient.runCron(id: String): Boolean = callRpc<JsonObject>("miniapp.crons.run", buildJsonObject { put("id", id) }) != null

/** Full cron job detail (`miniapp.crons.get`). */
suspend fun DenebGatewayClient.fetchCron(id: String): CronDetail? {
    val p = callRpc<MiniappCronDetail>("miniapp.crons.get", buildJsonObject { put("id", id) }) ?: return null
    return CronDetail(
        id = p.id,
        name = p.name,
        enabled = p.enabled,
        schedule = p.schedule,
        scheduleSpec = p.scheduleSpec,
        scheduleKind = p.scheduleKind,
        timezone = p.timezone,
        payloadKind = p.payloadKind,
        prompt = p.prompt,
        model = p.model,
        deliveryChannel = p.deliveryChannel,
        deliveryTo = p.deliveryTo,
        nextRunAtMs = p.nextRunAtMs,
        lastDeliveryStatus = p.lastDeliveryStatus,
        lastError = p.lastError,
        consecutiveErrors = p.consecutiveErrors,
        autoDisabledAtMs = p.autoDisabledAtMs,
    )
}

/** Enable or disable a cron job (`miniapp.crons.update`). */
suspend fun DenebGatewayClient.setCronEnabled(id: String, enabled: Boolean): Boolean = callRpc<JsonObject>(
    "miniapp.crons.update",
    buildJsonObject {
        put("id", id)
        put("enabled", enabled)
    },
) != null

/**
 * Patch an existing cron job (`miniapp.crons.update`). Only the arguments the
 * caller passes non-null are sent; each maps to the gateway's optional-pointer
 * patch, so omitted fields stay untouched (editing the schedule alone never
 * blanks the prompt). The gateway parses the schedule spec and returns its
 * reason on a bad expression — surfaced here so the edit form can show it.
 * Returns null on success, an error message otherwise. Refreshes the cached
 * task list on success so the list row reflects the edit.
 */
suspend fun DenebGatewayClient.updateCron(
    id: String,
    name: String? = null,
    schedule: String? = null,
    tz: String? = null,
    prompt: String? = null,
    model: String? = null,
): String? {
    val err = rpcWrite(
        "miniapp.crons.update",
        buildJsonObject {
            put("id", id)
            if (name != null) put("name", name)
            if (schedule != null) put("schedule", schedule)
            if (tz != null) put("tz", tz)
            if (prompt != null) put("prompt", prompt)
            if (model != null) put("model", model)
        },
    )
    if (err == null) refreshScheduledTasks()
    return err
}
