package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlin.time.Clock

/**
 * Cron job detail (`miniapp.crons.get`): schedule, the full instruction, where
 * it delivers, and runtime state. Toggle enable, run now, or delete.
 */
@Composable
fun DenebCronScreen(
    client: DenebGatewayClient,
    cronId: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var cron by remember(cronId) { mutableStateOf<CronDetail?>(null) }
    var loadFailed by remember(cronId) { mutableStateOf(false) }
    var busy by remember(cronId) { mutableStateOf(false) }
    var status by remember(cronId) { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun reload() {
        val c = client.fetchCron(cronId)
        cron = c
        loadFailed = c == null
    }
    LaunchedEffect(cronId) { reload() }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier.statusBarsPadding().padding(16.dp).verticalScroll(rememberScrollState()),
        ) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                Spacer(Modifier.height(12.dp))
            }
            TextButton(onClick = onBack) { Text("← 뒤로") }
            Spacer(Modifier.height(4.dp))

            val c = cron
            if (c == null) {
                if (loadFailed) DenebError("크론을 불러오지 못했습니다.") else DenebLoading()
                return@Column
            }

            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    c.name.ifBlank { c.id },
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                Switch(
                    checked = c.enabled,
                    onCheckedChange = { e -> scope.launch { busy = true; client.setCronEnabled(c.id, e); reload(); busy = false } },
                    enabled = !busy,
                )
            }

            Spacer(Modifier.height(8.dp))
            Text(c.schedule.ifBlank { "(일정 없음)" }, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
            if (c.scheduleSpec.isNotBlank()) {
                Text("${c.scheduleKind} · ${c.scheduleSpec}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            nextRun(c.nextRunAtMs)?.let {
                Text("다음 실행 $it", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (c.autoDisabledAtMs > 0) {
                Text("자동 비활성화됨 (연속 실패 ${c.consecutiveErrors})", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            } else if (c.consecutiveErrors > 0) {
                Text("연속 실패 ${c.consecutiveErrors}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }
            if (c.lastError.isNotBlank()) {
                Text("마지막 오류: ${c.lastError}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }

            Spacer(Modifier.height(16.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Spacer(Modifier.height(12.dp))
            Text("작업", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            Text(
                payloadKindLabel(c.payloadKind) + if (c.model.isNotBlank()) "  ·  ${c.model}" else "",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (c.prompt.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                DenebMarkdown(c.prompt)
            }

            if (c.deliveryChannel.isNotBlank() || c.deliveryTo.isNotBlank()) {
                Spacer(Modifier.height(12.dp))
                Text("전달", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                Text(
                    listOf(c.deliveryChannel, c.deliveryTo).filter { it.isNotBlank() }.joinToString("  ·  "),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            Spacer(Modifier.height(16.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Spacer(Modifier.height(12.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(
                    enabled = !busy,
                    onClick = {
                        scope.launch {
                            busy = true
                            status = if (client.runCron(c.id)) "실행 요청됨" else "실행 실패"
                            busy = false
                        }
                    },
                ) { Text("지금 실행") }
                OutlinedButton(
                    enabled = !busy,
                    onClick = { scope.launch { client.cancelScheduledTask(c.id); onBack() } },
                ) { Text("삭제") }
            }
            status?.let {
                Spacer(Modifier.height(8.dp))
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}

private fun payloadKindLabel(kind: String): String = when (kind) {
    "agentTurn" -> "에이전트 실행"
    "systemEvent" -> "시스템 이벤트"
    else -> kind.ifBlank { "—" }
}

/** Epoch-ms -> a short relative "N분 후" / "지연 N분", or null when unset. */
private fun nextRun(ms: Long): String? {
    if (ms <= 0) return null
    val diff = ms - Clock.System.now().toEpochMilliseconds()
    return when {
        diff < 0 -> "지연 ${-diff / 60000}분"
        diff < 3_600_000 -> "${diff / 60000}분 후"
        diff < 86_400_000 -> "${diff / 3_600_000}시간 후"
        else -> "${diff / 86_400_000}일 후"
    }
}
