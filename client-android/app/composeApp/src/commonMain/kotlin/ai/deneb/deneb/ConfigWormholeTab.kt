package ai.deneb.deneb

import ai.deneb.deneb.generated.WormholeStatusOut
import ai.deneb.ui.denebHairline
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Settings hub "Wormhole" tab: the wormhole model router's live status
// (miniapp.wormhole.status) + its global feature toggles. Read-mostly — a
// router dashboard plus two on/off switches; the gateway writes the toggle to
// the wormhole config, which hot-reloads it. Editing models/keys stays in chat
// (the agentic-actions pattern). Hosted by [DenebConfigScreen]'s pager.
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
                LazyColumn(Modifier.fillMaxSize()) {
                    item {
                        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp)) {
                            Text(
                                if (s.reachable) "● 가동 중" else "○ 꺼짐",
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.SemiBold,
                                color = if (s.reachable) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            if (s.listen.isNotBlank()) {
                                Text(
                                    s.listen,
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                        HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                    }

                    item { WormholeSectionHeader("기능") }
                    item {
                        WormholeToggleRow("로컬 전용 (클라우드 차단)", s.localOnly, busy) { toggle("localOnly", it) }
                        HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                    }
                    item {
                        WormholeToggleRow("Effort 라우팅 (간단한 턴은 thinking off)", s.effortRouting, busy) { toggle("effortRouting", it) }
                        HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                    }

                    item { WormholeSectionHeader("모델") }
                    if (s.models.isEmpty()) {
                        item {
                            Text(
                                "구성된 모델이 없습니다.",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.fillMaxWidth().padding(16.dp),
                            )
                        }
                    } else {
                        items(s.models, key = { it.name }) { m ->
                            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                                Text(m.name, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
                                Text(
                                    buildString {
                                        append(if (m.local) "로컬" else "클라우드")
                                        append(" · ")
                                        append(m.protocol)
                                        if (m.thinking) append(" · thinking 토글")
                                    },
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            HorizontalDivider(Modifier.padding(start = 16.dp), color = denebHairline())
                        }
                    }

                    if (s.auto.isNotEmpty()) {
                        item { WormholeSectionHeader("auto 후보 (순서)") }
                        item {
                            Text(
                                s.auto.joinToString(" → "),
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                            )
                        }
                    }
                }
            }
        }
    }
}

/** A label + Material Switch row in the tab's flat-hairline idiom. The switch is
 *  Material (control), the label Material typography (matching the config pager). */
@Composable
private fun WormholeToggleRow(label: String, checked: Boolean, busy: Boolean, onToggle: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
        Switch(checked = checked, onCheckedChange = { if (!busy) onToggle(it) }, enabled = !busy)
    }
}

@Composable
private fun WormholeSectionHeader(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelMedium,
        fontWeight = FontWeight.SemiBold,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp),
    )
}
