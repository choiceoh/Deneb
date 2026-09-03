package ai.deneb.deneb

import ai.deneb.data.ScheduledTask
import ai.deneb.data.ServiceEntry
import ai.deneb.data.TaskStatus
import ai.deneb.data.TaskTrigger
import ai.deneb.deneb.generated.MiniappCronDetail
import ai.deneb.deneb.generated.SelfCorrectionCandidate
import ai.deneb.deneb.generated.SelfImprovementCodingListResponse
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
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Admin surface of [DenebGatewayClient]: the model registry / role switcher
 * (`miniapp.models.*`), the chat-input model switcher entries, skills
 * (`miniapp.skills.*`), and cron jobs (`miniapp.crons.*`). Extensions so the
 * gateway client stays one facade while each RPC domain lives in its own file.
 */

// --- Model switcher → Deneb registry ------------------------------------
// Settings (`setRoleModel`) still writes the gateway default via models.set
// with no sessionKey. The chat-input switcher passes sessionKey so only
// that conversation's model changes.

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
        .filter { it.id.isNotBlank() }
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
                runs24h = it.runs24h,
                inputTokens24h = it.inputTokens24h,
                outputTokens24h = it.outputTokens24h,
                cacheReadTokens24h = it.cacheReadTokens24h,
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
// sources that list from here so the switcher binds the current conversation's
// model instead of the upstream local providers.

/** Gateway models as switcher entries, the session (or global) selection first
 *  (the ServiceSelector renders the first entry as selected). */
fun DenebGatewayClient.denebServiceEntries(sessionKey: String? = null): List<ServiceEntry> {
    val models = _denebModels.value
    val selectedId = sessionKey?.let { _sessionModels.value[it] }?.takeIf { it.isNotBlank() }
        ?: models.firstOrNull { it.current }?.id
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

/** Bind [instanceId] to the current conversation only — not the global main role. */
fun DenebGatewayClient.selectDenebModelInstance(instanceId: String) {
    val modelId = instanceId.removePrefix(DENEB_MODEL_PREFIX)
    if (modelId.isBlank() || modelId == instanceId) return
    val key = sessionKey
    if (key.isBlank()) return
    scope.launch { setSessionModel(key, modelId) }
}

/** Persist a chat-picker choice on one session. Settings still uses [setRoleModel]. */
suspend fun DenebGatewayClient.setSessionModel(sessionKey: String, id: String): Boolean {
    val key = sessionKey.trim()
    if (key.isEmpty()) return false
    val modelId = id.trim()
    val ok = callRpc<JsonObject>(
        "miniapp.models.set",
        buildJsonObject {
            put("id", modelId)
            put("sessionKey", key)
        },
    ) != null
    if (ok) {
        _sessionModels.value = if (modelId.isEmpty()) {
            _sessionModels.value - key
        } else {
            _sessionModels.value + (key to modelId)
        }
    }
    return ok
}

// Prefix marking a switcher instanceId as a gateway model (vs. an upstream
// local-provider instance).
private const val DENEB_MODEL_PREFIX = "deneb-model:"

// --- Skills → Settings Skills tab ---------------------------------------
// The native client doesn't know server-side skill paths; miniapp.skills.list
// resolves the workspace itself and returns the same skills the agent sees.
// Mutations are guarded server-side and only enabled for local mutable skills.

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

/** Self-improvement coding queue for Settings > 자가개선 코딩.
 *  These are deferred code-change hypotheses, not skills and not Propus log
 *  events. */
suspend fun DenebGatewayClient.fetchSelfImprovementCodingQueue(
    limit: Int = 60,
    status: String = "proposed",
): SelfImprovementCodingListResponse? = callRpc<SelfImprovementCodingListResponse>(
    "miniapp.self_improvement_coding.list",
    buildJsonObject {
        put("limit", limit)
        if (status.isNotBlank()) put("status", status)
        // The review models answer in English — measured 2026-09-03, 78% of the
        // live queue's titles carried no Hangul. This screen is read by a person,
        // so it asks for Korean. The flag is opt-in precisely so the L4 miners and
        // the dispatch selector, which feed a coding agent its instructions, keep
        // the untranslated text.
        put("translate", true)
    },
)

/** Pending self-improvement coding candidates for callers that only need rows. */
suspend fun DenebGatewayClient.fetchSelfImprovementCodingCandidates(
    limit: Int = 60,
    status: String = "proposed",
): List<SelfCorrectionCandidate>? = fetchSelfImprovementCodingQueue(limit, status)?.candidates

/** One skill's enriched row + SKILL.md body for the tap-through detail screen.
 *  Null on transport failure or unknown skill name. */
suspend fun DenebGatewayClient.fetchSkillDetail(name: String): SkillDetailResponse? = callRpc<SkillDetailResponse>(
    "miniapp.skills.detail",
    buildJsonObject { put("name", name) },
)

/** Replace one mutable local skill's SKILL.md. Returns null on success, or the
 *  gateway's reason when validation/write fails. */
suspend fun DenebGatewayClient.updateSkill(name: String, body: String): String? {
    val err = rpcWrite(
        "miniapp.skills.update",
        buildJsonObject {
            put("name", name)
            put("body", body)
        },
    )
    if (err == null) refreshSkills()
    return err
}

/** Delete one mutable local skill directory. Returns null on success, or the
 *  gateway's reason when the skill is protected or deletion fails. */
suspend fun DenebGatewayClient.deleteSkill(name: String): String? {
    val err = rpcWrite("miniapp.skills.delete", buildJsonObject { put("name", name) })
    if (err == null) refreshSkills()
    return err
}

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
        .distinctByLast { it.id }
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

// --- moved from DenebGatewayClient.kt (stage 3, logic unchanged): client
// status hello, update manifest check, push token register/unregister. ---

suspend fun DenebGatewayClient.refreshClientStatus(): ClientStatus? {
    val payload = callRpc<ClientHelloPayload>("miniapp.client.hello", buildJsonObject {}) ?: run {
        _clientStatus.value = null
        return null
    }
    val status = ClientStatus(
        version = payload.version,
        nativeApiVersion = payload.nativeApiVersion,
        model = payload.model,
        capabilities = payload.capabilities,
        endpoints = payload.endpoints,
        timestampMs = payload.tsMs,
    )
    _clientStatus.value = status
    return status
}

/**
 * Check the gateway-served update manifest. The gateway exposes the APK +
 * metadata on its own port (the same base URL used for chat), so this works
 * over the cloudflare tunnel — unlike the old :19010 side-server the tunnel
 * never routed. Returns non-null only when a strictly newer build than the
 * compiled-in [DENEB_VERSION_CODE] is published.
 */
suspend fun DenebGatewayClient.checkUpdate(): UpdateInfo? {
    return try {
        val base = gatewayUrl.trim().removeSuffix("/")
        if (base.isEmpty() || clientToken.isEmpty()) return null
        val response = http.get("$base/api/v1/app/update/manifest") {
            header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
            // Bounded timeout: a missing or blocked gateway must fail fast
            // instead of hanging the "check for update" spinner forever.
            timeout {
                requestTimeoutMillis = 10_000
                connectTimeoutMillis = 6_000
            }
        }
        if (!response.status.isSuccess()) return null
        val m = response.body<UpdateManifest>()
        if (m.code > DENEB_VERSION_CODE && m.file.isNotBlank()) {
            // The browser opening this link can't set a header, so a token rides
            // in the query string. Prefer the manifest's short-lived download
            // token (URLs leak via logs/history — the long-lived client token
            // shouldn't); fall back to the legacy clientToken query only for
            // old gateways that don't mint one.
            val auth = if (m.downloadToken.isNotBlank()) {
                "dl=${m.downloadToken.encodeURLParameter()}"
            } else {
                "clientToken=${clientToken.encodeURLParameter()}"
            }
            val apk = "$base/api/v1/app/update/download" +
                "?file=${m.file.encodeURLParameter()}&$auth"
            UpdateInfo(buildLabel = m.code.toString(), apkUrl = apk, notes = m.notes)
        } else {
            null
        }
    } catch (cancel: CancellationException) {
        throw cancel
    } catch (_: Exception) {
        null
    }
}

/**
 * Registers this device's FCM registration token so the gateway can deliver
 * proactive reports when no live SSE connection is held (app fully closed /
 * Doze). Best-effort and idempotent — the gateway dedups by token — so it is
 * cheap to call on every foreground. Returns true on success. Android-only
 * caller, but the RPC itself is platform-agnostic so this lives in commonMain.
 *
 * A definitive response also refreshes [DenebGatewayClient.fcmDeliveryReady]
 * from the gateway's `delivery` field (false on older gateways that omit it —
 * conservative: unknown delivery keeps background SSE alive). No response
 * (offline / RPC error) leaves the last persisted answer untouched.
 */
suspend fun DenebGatewayClient.registerPushToken(token: String, platform: String): Boolean {
    if (token.isBlank()) return false
    val payload = callRpc<PushRegisterPayload>(
        "miniapp.push.register",
        buildJsonObject {
            put("token", token)
            put("platform", platform)
        },
    ) ?: return false
    _fcmDeliveryReady.value = payload.delivery
    appSettings.settings.putBoolean(DenebGatewayClient.KEY_FCM_DELIVERY, payload.delivery)
    return payload.ok
}

@Serializable
internal data class PushRegisterPayload(val ok: Boolean = false, val delivery: Boolean = false)

/** Removes a device token (e.g. on sign-out / token invalidation). */
suspend fun DenebGatewayClient.unregisterPushToken(token: String): Boolean {
    if (token.isBlank()) return false
    return rpcWrite(
        "miniapp.push.unregister",
        buildJsonObject { put("token", token) },
    ) == null
}
