package ai.deneb.deneb

import ai.deneb.deneb.generated.WormholeModelOut
import ai.deneb.deneb.generated.WormholeStatusOut
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.settings.SettingsCard
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Settings hub "Wormhole" tab: the wormhole model router's live status
// (miniapp.wormhole.status) + its global feature toggles, plus per-cloud-model
// KEY HEALTH and one-tap KEY ROTATION. A dead/invalid upstream key surfaces as a
// warm "인증 실패" mark; tapping a cloud model opens a paste-a-new-key dialog that
// writes wormhole's secrets.env (gateway-side) and hot-reloads with no restart.
// Editing models still stays in chat (the agentic-actions pattern).
//
// Design refresh (2026-06): the settings grouped-card idiom — toggles in a
// [DenebGroup]/[DenebListRow] block, the model list in a [SettingsCard], the cool
// accent (`primary`) on live-state marks and the warm accent ([denebInsight]) on
// key-health problems (the two-accent doctrine).
@Composable
internal fun WormholeTab(client: DenebGatewayClient) {
    var status by remember { mutableStateOf<WormholeStatusOut?>(null) }
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var rotating by remember { mutableStateOf<WormholeModelOut?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        loading = true
        failed = false
        val s = client.fetchWormholeStatus()
        status = s
        failed = s == null
        loading = false
    }
    LaunchedEffect(Unit) { load() }

    fun toggle(feature: String, enabled: Boolean) {
        if (busy) return
        scope.launch {
            busy = true
            client.setWormholeFeature(feature, enabled)
            busy = false
            // Reload so the switch reflects the config that was actually written
            // (an honest view even if the write was rejected).
            load()
        }
    }

    Box(Modifier.fillMaxSize()) {
        when {
            loading -> DenebLoading()

            failed || status == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                DenebError("wormhole 상태를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
            }

            else -> {
                val s = status!!
                LazyColumn(Modifier.fillMaxSize().padding(vertical = 12.dp)) {
                    item { WormholeStatusHeader(s) }

                    item {
                        DenebGroup(label = "기능") {
                            DenebListRow(
                                title = "로컬 전용 (클라우드 차단)",
                                onClick = { toggle("localOnly", !s.localOnly) },
                                icon = Icons.Outlined.CloudOff,
                                selected = s.localOnly,
                                chevron = false,
                                trailing = {
                                    Switch(
                                        checked = s.localOnly,
                                        onCheckedChange = { if (!busy) toggle("localOnly", it) },
                                        enabled = !busy,
                                    )
                                },
                            )
                            DenebListRow(
                                title = "Effort 라우팅 (간단한 턴은 thinking off)",
                                onClick = { toggle("effortRouting", !s.effortRouting) },
                                icon = Icons.Outlined.Bolt,
                                selected = s.effortRouting,
                                divider = false,
                                chevron = false,
                                trailing = {
                                    Switch(
                                        checked = s.effortRouting,
                                        onCheckedChange = { if (!busy) toggle("effortRouting", it) },
                                        enabled = !busy,
                                    )
                                },
                            )
                        }
                    }

                    item { DenebSectionLabel("모델", Modifier.padding(horizontal = 16.dp)) }
                    if (s.models.isEmpty()) {
                        item {
                            SettingsCard(Modifier.padding(horizontal = 16.dp)) {
                                Text("구성된 모델이 없습니다.", style = DenebType.body, color = denebHint())
                            }
                        }
                    } else {
                        item {
                            SettingsCard(Modifier.padding(horizontal = 16.dp), innerPadding = false) {
                                s.models.forEachIndexed { i, m ->
                                    WormholeModelRow(
                                        name = m.name,
                                        local = m.local,
                                        meta = buildString {
                                            append(if (m.local) "로컬" else "클라우드")
                                            append(" · ")
                                            append(m.protocol)
                                            if (m.thinking) append(" · thinking 토글")
                                            if (m.source == "fleet") append(" · 자동발견")
                                        },
                                        keyHealth = m.keyHealth,
                                        // Cloud models carry an upstream key → tappable to rotate it.
                                        // Local models have none.
                                        onClick = if (!m.local) ({ if (!busy) rotating = m }) else null,
                                        divider = i < s.models.lastIndex,
                                    )
                                }
                            }
                        }
                    }

                    if (s.auto.isNotEmpty()) {
                        item { DenebSectionLabel("auto 후보 (순서)", Modifier.padding(horizontal = 16.dp)) }
                        item {
                            SettingsCard(Modifier.padding(horizontal = 16.dp)) {
                                Text(s.auto.joinToString(" → "), style = DenebType.body, color = MaterialTheme.colorScheme.onBackground)
                            }
                        }
                    }
                }
            }
        }

        rotating?.let { m ->
            WormholeRotateDialog(
                modelName = m.name,
                onDismiss = { rotating = null },
                onRotate = { key ->
                    val r = client.setWormholeKey(m.name, key)
                    if (r?.valid == true) load() // refresh keyHealth — now "ok"
                    r
                },
            )
        }
    }
}

/** Router liveness header: the running-state mark in the cool interactive accent
 *  (`primary`) when reachable, muted hint when off, with the listen address below. */
@Composable
private fun WormholeStatusHeader(s: WormholeStatusOut) {
    val accent = MaterialTheme.colorScheme.primary
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Text(
            if (s.reachable) "● 가동 중" else "○ 꺼짐",
            style = DenebType.rowTitleStrong,
            color = if (s.reachable) accent else denebHint(),
        )
        if (s.listen.isNotBlank()) {
            Text(s.listen, style = DenebType.meta, color = denebHint())
        }
    }
}

/** A model row inside the model [SettingsCard]: a live-state dot (warm accent on a
 *  key-health problem, cool accent when served locally, else muted), the model
 *  name, its protocol/source metadata, and — for cloud models — a key-health label
 *  and a chevron marking it tappable to rotate the key. */
@Composable
private fun WormholeModelRow(
    name: String,
    local: Boolean,
    meta: String,
    keyHealth: String,
    onClick: (() -> Unit)?,
    divider: Boolean,
) {
    val hairline = denebHairline()
    val problem = keyHealthIsProblem(keyHealth)
    val dot = when {
        problem -> denebInsight()
        local -> MaterialTheme.colorScheme.primary
        else -> denebHint()
    }
    val insetPx = with(LocalDensity.current) { 48.dp.toPx() }
    Row(
        Modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .drawBehind {
                if (divider) {
                    val stroke = 1.dp.toPx()
                    val y = size.height - stroke / 2f
                    drawLine(hairline, Offset(insetPx, y), Offset(size.width, y), strokeWidth = stroke)
                }
            }
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).clip(CircleShape).drawBehind { drawRect(dot) })
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text(name, style = DenebType.rowTitleStrong, color = MaterialTheme.colorScheme.onBackground)
            Text(meta, style = DenebType.rowSubtitle, color = denebHint())
        }
        val kh = keyHealthKo(keyHealth)
        if (kh.isNotEmpty()) {
            Spacer(Modifier.width(8.dp))
            Text(kh, style = DenebType.meta, color = if (problem) denebInsight() else denebHint())
        }
        if (onClick != null) {
            Spacer(Modifier.width(10.dp))
            Text("›", style = DenebType.rowTitle, color = denebHint())
        }
    }
}

/** Paste-a-new-key dialog. Writes wormhole's secrets.env via the gateway (no
 *  restart) and reports the post-write validation probe: closes on a verified key,
 *  stays open with an explanation otherwise. */
@Composable
private fun WormholeRotateDialog(
    modelName: String,
    onDismiss: () -> Unit,
    onRotate: suspend (String) -> WormholeKeyResult?,
) {
    var key by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<String?>(null) }
    var resultProblem by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text("키 회전 · $modelName", style = DenebType.subject) },
        text = {
            Column {
                Text("새 API 키를 붙여넣으면 재시작 없이 적용됩니다.", style = DenebType.body, color = denebHint())
                Spacer(Modifier.height(12.dp))
                OutlinedTextField(
                    value = key,
                    onValueChange = { key = it },
                    singleLine = true,
                    enabled = !busy,
                    label = { Text("새 API 키") },
                    modifier = Modifier.fillMaxWidth(),
                )
                result?.let {
                    Spacer(Modifier.height(10.dp))
                    Text(
                        it,
                        style = DenebType.meta,
                        color = if (resultProblem) denebInsight() else MaterialTheme.colorScheme.primary,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = !busy && key.isNotBlank(),
                onClick = {
                    scope.launch {
                        busy = true
                        result = null
                        val r = onRotate(key.trim())
                        when {
                            r == null -> {
                                result = "회전 실패 — 거부되었거나 wormhole에 닿지 못했습니다."
                                resultProblem = true
                            }

                            r.valid -> onDismiss()

                            // verified: keyHealth refreshed, close
                            else -> {
                                result = "키는 적용됐으나 인증 실패 (HTTP ${r.status}). 키를 확인하세요."
                                resultProblem = true
                            }
                        }
                        busy = false
                    }
                },
            ) { Text(if (busy) "적용 중…" else "회전") }
        },
        dismissButton = { TextButton(enabled = !busy, onClick = onDismiss) { Text("취소") } },
    )
}

private fun keyHealthIsProblem(kh: String): Boolean = kh == "auth_failed" || kh == "rate_limited" || kh == "unreachable" || kh.startsWith("http_")

private fun keyHealthKo(kh: String): String = when (kh) {
    "" -> ""
    "ok" -> "키 정상"
    "auth_failed" -> "인증 실패"
    "rate_limited" -> "쿼터 초과"
    "unreachable" -> "연결 불가"
    "unchecked" -> "확인 전"
    else -> kh
}
