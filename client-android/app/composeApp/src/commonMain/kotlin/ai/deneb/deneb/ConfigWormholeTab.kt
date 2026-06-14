package ai.deneb.deneb

import ai.deneb.deneb.generated.WormholeStatusOut
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.settings.SettingsCard
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
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
// (miniapp.wormhole.status) + its global feature toggles. Read-mostly — a
// router dashboard plus two on/off switches; the gateway writes the toggle to
// the wormhole config, which hot-reloads it. Editing models/keys stays in chat
// (the agentic-actions pattern). Hosted by [DenebConfigScreen]'s pager.
//
// Design refresh (2026-06): the settings grouped-card idiom — toggles in a
// [DenebGroup]/[DenebListRow] block, the model list in a [SettingsCard], and the
// restrained cool accent (`primary`) only on the live-state marks (router up,
// locally-served models).
@Composable
internal fun WormholeTab(client: DenebGatewayClient) {
    var status by remember { mutableStateOf<WormholeStatusOut?>(null) }
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
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

/** A read-only model row inside the model [SettingsCard]: a small live-state dot
 *  (cool accent when served locally, muted otherwise), the model name, and its
 *  protocol/source metadata. Not tappable — editing models stays in chat. */
@Composable
private fun WormholeModelRow(name: String, local: Boolean, meta: String, divider: Boolean) {
    val hairline = denebHairline()
    val dot = if (local) MaterialTheme.colorScheme.primary else denebHint()
    val insetPx = with(LocalDensity.current) { 48.dp.toPx() }
    Row(
        Modifier
            .fillMaxWidth()
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
    }
}
