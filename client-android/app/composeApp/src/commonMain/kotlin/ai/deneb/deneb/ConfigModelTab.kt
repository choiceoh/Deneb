package ai.deneb.deneb

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RichTooltip
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TooltipBox
import androidx.compose.material3.rememberTooltipState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.PopupPositionProvider
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import ai.deneb.ui.denebPressable
import ai.deneb.ui.settings.SettingsCard
import ai.deneb.ui.statusDanger
import ai.deneb.ui.statusSuccess
import ai.deneb.ui.statusWarning
import kotlinx.coroutines.launch

/**
 * Settings hub "모델" tab: per-role model assignment (with the "?" role tooltip),
 * the provider-grouped model list, and the custom OpenAI-compatible endpoint
 * form. Hosted by [DenebConfigScreen]'s pager.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun ModelTab(client: DenebGatewayClient) {
    val models by client.denebModels.collectAsState()
    val roleModels by client.denebRoleModels.collectAsState()
    val advisories by client.denebModelAdvisories.collectAsState()
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    var role by remember { mutableStateOf(ModelRole.MAIN) }
    var switching by remember { mutableStateOf(false) }
    var switchFailed by remember { mutableStateOf(false) }
    var addBaseUrl by remember { mutableStateOf("") }
    var addModel by remember { mutableStateOf("") }
    var adding by remember { mutableStateOf(false) }
    var addError by remember { mutableStateOf<String?>(null) }
    var pendingDelete by remember { mutableStateOf<ModelOption?>(null) }
    // Model id whose tuner detail is expanded (ⓘ tap); null = all collapsed.
    var expandedInfoId by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { client.refreshModels() }
    if (models.isEmpty()) {
        DenebLoading()
        return
    }
    val currentForRole = roleModels[role.wire]
    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // Header + a "?" affordance: tapping the circled question mark opens a
        // rich tooltip explaining what each of the five roles does. Descriptions
        // live on ModelRole.desc so the segmented buttons and the tooltip stay in
        // sync from one source.
        val roleTooltip = rememberTooltipState(isPersistent = true)
        // Material3's default rich-tooltip position provider computes a negative x
        // when a wide tooltip is anchored near the left edge: as an off-screen
        // fallback it centers the tooltip on the tiny "?" anchor, so the tooltip
        // clips off the left of narrow phone screens. Use a provider that
        // left-aligns to the anchor and clamps fully into the window — the same
        // approach the chat ServiceSelector's AnchorAbovePositionProvider takes.
        val tooltipSpacingPx = with(LocalDensity.current) { 4.dp.roundToPx() }
        val clampedTooltipPosition = remember(tooltipSpacingPx) {
            ClampedTooltipPositionProvider(tooltipSpacingPx)
        }
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                "역할별 모델",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            TooltipBox(
                positionProvider = clampedTooltipPosition,
                tooltip = {
                    RichTooltip(title = { Text("모델 역할") }) {
                        Text(ModelRole.entries.joinToString("\n") { "${it.label} — ${it.desc}" })
                    }
                },
                state = roleTooltip,
            ) {
                Box(
                    modifier = Modifier
                        .size(18.dp)
                        .border(1.dp, MaterialTheme.colorScheme.outline, CircleShape)
                        .clickable { scope.launch { roleTooltip.show() } }
                        .handCursor(),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "?",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        // Legend for the per-model response-status dot.
        Row(
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            HealthLegendItem(ModelHealth.ONLINE, "응답 가능")
            HealthLegendItem(ModelHealth.OFFLINE, "응답 없음")
            HealthLegendItem(ModelHealth.UNKNOWN, "미확인")
        }
        // Model tuner advisories: open recommendations from the background
        // per-model optimization loop (stalls, cache breaks, slow tails). Shown
        // only when something needs attention — silence is the normal state.
        // Collapsed to a one-line count by default; tap to read the details.
        if (advisories.isNotEmpty()) {
            var advisoriesOpen by remember { mutableStateOf(false) }
            SettingsCard(innerPadding = false) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { haptics.tap(); advisoriesOpen = !advisoriesOpen }
                        .handCursor()
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        "튜너 권고 ${advisories.size}건",
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        if (advisoriesOpen) "▾" else "▸",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (advisoriesOpen) {
                    Column(
                        modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 12.dp),
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        advisories.forEach { line ->
                            Text(
                                line,
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }
        // Role summary: every role and its currently-assigned model at a glance,
        // so you don't have to click through each segment to see what's wired.
        // Tapping a row selects that role for the model list below.
        SettingsCard(innerPadding = false) {
            ModelRole.entries.forEachIndexed { i, r ->
                val assignedId = roleModels[r.wire]
                val assignedName = models.firstOrNull { it.id == assignedId }?.display
                    ?: assignedId ?: "미설정"
                val isSel = role == r
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { haptics.tap(); role = r }
                        .handCursor()
                        .padding(horizontal = 16.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        r.label,
                        style = MaterialTheme.typography.labelLarge,
                        fontWeight = if (isSel) FontWeight.SemiBold else FontWeight.Normal,
                        color = if (isSel) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.width(56.dp),
                    )
                    Spacer(Modifier.width(8.dp))
                    Text(
                        assignedName,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                }
                if (i < ModelRole.entries.lastIndex) {
                    HorizontalDivider(
                        Modifier.padding(start = 16.dp),
                        color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f),
                    )
                }
            }
        }
        if (switchFailed) {
            Text(
                "모델 전환에 실패했어요. 다시 시도해 주세요.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
        Text(
            "'${role.label}' 역할에 사용할 모델",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        // Model list grouped by provider (the id prefix before "/"), so local
        // vLLM, custom endpoints, and cloud providers don't blur into one flat
        // list. Tapping a row assigns that model to the role selected above.
        SettingsCard(innerPadding = false) {
            val grouped = remember(models) { models.groupBy { modelProviderLabel(it.id) } }
            grouped.entries.forEachIndexed { gi, (provider, groupModels) ->
                Text(
                    provider,
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(
                        start = 16.dp,
                        top = if (gi == 0) 12.dp else 18.dp,
                        bottom = 2.dp,
                    ),
                )
                groupModels.forEachIndexed { mi, model ->
                    val isCurrent = model.id == currentForRole
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .denebPressable(
                                enabled = !isCurrent && !switching,
                                onClick = {
                                    haptics.tap()
                                    scope.launch {
                                        switching = true
                                        switchFailed = !client.setRoleModel(model.id, role.wire)
                                        switching = false
                                    }
                                },
                            )
                            .padding(horizontal = 16.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        // Color = response status (online/offline/unknown); filled = the
                        // model currently selected for this role, ring = not selected.
                        HealthDot(health = ModelHealth.parse(model.health), selected = isCurrent)
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f)) {
                            Text(
                                // Circuit breaker open → consecutive failures; the
                                // gateway is routing this model's turns to fallback.
                                // The marker stays always-visible (rare + important);
                                // the stat detail hides behind the ⓘ toggle.
                                if (model.unhealthy) model.display + " ⚠️연속실패" else model.display,
                                style = MaterialTheme.typography.bodyLarge,
                                fontWeight = if (isCurrent) FontWeight.SemiBold else FontWeight.Normal,
                                color = if (model.unhealthy) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface,
                            )
                            Text(
                                model.id + ModelHealth.parse(model.health).suffix,
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            // Tuner stat line (24h runs, p95, cache hit, fallback/stall,
                            // calibration probe, tuned output floor) — only when the row's
                            // ⓘ toggle is expanded, so the list stays clean by default.
                            if (expandedInfoId == model.id && model.note.isNotBlank()) {
                                Text(
                                    model.note,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.7f),
                                )
                            }
                        }
                        // ⓘ toggle: same circled-glyph affordance as the role "?"
                        // tooltip above. Shown only when the tuner has data for this
                        // model, so rows without stats carry no extra chrome.
                        if (model.note.isNotBlank()) {
                            Spacer(Modifier.width(8.dp))
                            Box(
                                modifier = Modifier
                                    .size(18.dp)
                                    .border(1.dp, MaterialTheme.colorScheme.outline, CircleShape)
                                    .clickable {
                                        haptics.tap()
                                        expandedInfoId = if (expandedInfoId == model.id) null else model.id
                                    }
                                    .handCursor(),
                                contentAlignment = Alignment.Center,
                            ) {
                                Text(
                                    "i",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                        // User-added models can be removed; built-in/role models can't.
                        if (model.custom) {
                            TextButton(
                                onClick = {
                                    haptics.reject()
                                    pendingDelete = model
                                },
                                enabled = !switching,
                            ) {
                                Text("삭제", color = MaterialTheme.colorScheme.error)
                            }
                        }
                    }
                    if (mi < groupModels.lastIndex) {
                        HorizontalDivider(
                            Modifier.padding(start = 16.dp),
                            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f),
                        )
                    }
                }
            }
        }

        // Add an OpenAI-compatible endpoint (vLLM / LM Studio / etc.) by base URL
        // + model name in its own card, matching the gateway-connection card so
        // the form doesn't float on the bare background below the model list.
        SettingsCard {
            Text(
                "OpenAI 호환 모델 직접 추가",
                style = DenebType.cardTitle,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                "Base URL과 모델 이름으로 vLLM·LM Studio 같은 OpenAI 호환 엔드포인트를 추가합니다. 인증 키가 필요 없는 엔드포인트용입니다.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = addBaseUrl,
                onValueChange = { addBaseUrl = it; addError = null },
                label = { Text("Base URL") },
                placeholder = { Text("http://127.0.0.1:8000/v1") },
                singleLine = true,
                enabled = !adding,
                isError = addError != null,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = addModel,
                onValueChange = { addModel = it; addError = null },
                label = { Text("모델 이름") },
                placeholder = { Text("예: qwen2.5-coder-7b") },
                singleLine = true,
                enabled = !adding,
                isError = addError != null,
                modifier = Modifier.fillMaxWidth(),
            )
            addError?.let {
                Spacer(Modifier.height(8.dp))
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }
            Spacer(Modifier.height(12.dp))
            Button(
                onClick = {
                    haptics.confirm()
                    scope.launch {
                        adding = true
                        addError = null
                        val ok = client.addCustomModel(addBaseUrl.trim(), addModel.trim())
                        if (ok) {
                            addBaseUrl = ""
                            addModel = ""
                        } else {
                            addError = "추가에 실패했어요. Base URL과 모델 이름을 확인해 주세요."
                        }
                        adding = false
                    }
                },
                enabled = !adding && addBaseUrl.isNotBlank() && addModel.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(if (adding) "추가 중…" else "모델 추가")
            }
        }
    }

    pendingDelete?.let { target ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("모델 삭제") },
            text = {
                Text("'${target.display}' 모델을 목록에서 삭제할까요? 이 모델에 연결된 역할은 기본값으로 되돌아갑니다.")
            },
            confirmButton = {
                TextButton(onClick = {
                    haptics.reject()
                    val id = target.id
                    pendingDelete = null
                    scope.launch { client.deleteCustomModel(id) }
                }) { Text("삭제", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) { Text("취소") }
            },
        )
    }
}

// Response-status dot. Color = health (online/offline/unknown); a filled circle
// marks the model currently selected for the role, a ring marks the rest.
@Composable
private fun HealthDot(health: ModelHealth, selected: Boolean) {
    val color = health.color
    val base = Modifier.size(10.dp)
    Box(
        modifier = if (selected) {
            base.clip(CircleShape).background(color)
        } else {
            base.border(1.5.dp, color, CircleShape)
        },
    )
}

@Composable
private fun HealthLegendItem(health: ModelHealth, label: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(health.color))
        Spacer(Modifier.width(5.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Model response status. Color: online → green, offline/auth → red, unknown/unprobed → amber
 *  (the shared status accents from ui/Theme.kt, saturated on every flavor).
 *  [suffix] is appended to the model id line. Parsed once from the wire string.
 *  "auth" comes from the gateway's role health watch: the endpoint answers but
 *  rejects the credential (expired key) — distinct from "no response" so the
 *  operator knows to rotate the key, not check the network. */
private enum class ModelHealth(val color: Color, val suffix: String) {
    ONLINE(statusSuccess, ""),
    OFFLINE(statusDanger, "  ·  응답 없음"),
    AUTH(statusDanger, "  ·  인증 만료 — 키 갱신 필요"),
    UNKNOWN(statusWarning, "  ·  상태 미확인"),
    ;

    companion object {
        fun parse(health: String): ModelHealth = when (health.lowercase()) {
            "online" -> ONLINE
            "offline" -> OFFLINE
            "auth" -> AUTH
            else -> UNKNOWN
        }
    }
}

/** Model-assignment roles. [wire] is the gateway's role key (sent on the RPC and
 *  used to look up the current model); [label] is the Korean segmented-button text;
 *  [desc] is the one-line role explanation shown in the "?" tooltip. Descriptions
 *  mirror the gateway's modelrole registry (main / tiny / lightweight / analysis /
 *  fallback). */
private enum class ModelRole(val wire: String, val label: String, val desc: String) {
    MAIN("main", "메인", "채팅·분석·도구 호출 등 주 대화를 담당하는 기본 모델"),
    TINY("tiny", "초경량", "세션 제목·메일 1차 추출 같은 사소한 분류·추출"),
    LIGHTWEIGHT("lightweight", "경량", "위키 병합·파일럿·스킬 리뷰 같은 범위가 정해진 요약"),
    ANALYSIS("analysis", "분석", "메일 본문 분석·대화 압축·기록 요약 같은 추론 작업"),
    FALLBACK("fallback", "폴백", "메인 모델이 실패했을 때 대신 쓰는 모델"),
}

/**
 * Positions a rich tooltip above its anchor (falling back to below when there is
 * no room above), left-aligned to the anchor but clamped fully inside the window
 * so a wide tooltip never spills off a screen edge.
 *
 * Material3's default rich-tooltip position provider returns a negative x for a
 * wide tooltip anchored near the left edge (its off-screen fallback centers the
 * tooltip on the small anchor), which clipped the model-role "?" tooltip off the
 * left of narrow phone screens. Mirrors [ServiceSelector]'s clamping approach.
 * Marked internal so the clamping can be unit-tested without a live window.
 */
internal class ClampedTooltipPositionProvider(
    private val verticalSpacing: Int,
) : PopupPositionProvider {
    override fun calculatePosition(
        anchorBounds: IntRect,
        windowSize: IntSize,
        layoutDirection: LayoutDirection,
        popupContentSize: IntSize,
    ): IntOffset {
        val maxX = (windowSize.width - popupContentSize.width).coerceAtLeast(0)
        val x = anchorBounds.left.coerceIn(0, maxX)
        val above = anchorBounds.top - popupContentSize.height - verticalSpacing
        val y = if (above >= 0) {
            above
        } else {
            val maxY = (windowSize.height - popupContentSize.height).coerceAtLeast(0)
            (anchorBounds.bottom + verticalSpacing).coerceAtMost(maxY)
        }
        return IntOffset(x, y)
    }
}

// modelProviderLabel maps a model id ("vllm/step3p7", "custom/gemma…") to a
// human label for grouping the model list by provider. The prefix before the
// first "/" is the provider key; unknown providers fall back to the raw prefix
// so a newly-added provider still groups sensibly instead of vanishing.
private fun modelProviderLabel(id: String): String {
    val p = id.substringBefore('/', "")
    return when {
        p.isEmpty() -> "기타"
        p == "vllm" -> "로컬 (vLLM)"
        p.startsWith("custom") -> "커스텀"
        p == "google" -> "Google"
        p == "anthropic" -> "Anthropic"
        p == "openai" -> "OpenAI"
        p == "zai" -> "Z.ai"
        else -> p
    }
}
