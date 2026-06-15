package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
    onEdit: (String) -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var cron by remember(cronId) { mutableStateOf<CronDetail?>(null) }
    var loadFailed by remember(cronId) { mutableStateOf(false) }
    var busy by remember(cronId) { mutableStateOf(false) }
    var status by remember(cronId) { mutableStateOf<String?>(null) }
    var confirmDelete by remember(cronId) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun reload() {
        loadFailed = false
        cron = null
        val c = client.fetchCron(cronId)
        cron = c
        loadFailed = c == null
    }
    LaunchedEffect(cronId) { reload() }

    DenebScreenScaffold(title = "크론", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(8.dp))

            val c = cron
            if (c == null) {
                if (loadFailed) {
                    DenebError("크론을 불러오지 못했습니다.", onRetry = { scope.launch { reload() } })
                } else {
                    DenebLoading()
                }
                return@Column
            }

            Row(verticalAlignment = Alignment.CenterVertically) {
                // Active cron's title carries the cool interactive accent; a disabled
                // one falls back to ink (primary = interactive/active state only).
                Text(
                    c.name.ifBlank { c.id },
                    style = DenebType.subject,
                    color = if (c.enabled) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onBackground,
                    modifier = Modifier.weight(1f),
                )
                Switch(
                    checked = c.enabled,
                    onCheckedChange = { e ->
                        scope.launch {
                            busy = true
                            haptics.tap()
                            val ok = client.setCronEnabled(c.id, e)
                            reload()
                            if (!ok) status = "변경 실패"
                            busy = false
                        }
                    },
                    enabled = !busy,
                )
            }

            Spacer(Modifier.height(8.dp))
            Text(c.schedule.ifBlank { "(일정 없음)" }, style = DenebType.body, color = MaterialTheme.colorScheme.onBackground)
            if (c.scheduleSpec.isNotBlank()) {
                Text("${c.scheduleKind} · ${c.scheduleSpec}", style = DenebType.meta, color = denebHint())
            }
            nextRun(c.nextRunAtMs)?.let {
                Text("다음 실행 $it", style = DenebType.meta, color = denebHint())
            }
            if (c.autoDisabledAtMs > 0) {
                Text("자동 비활성화됨 (연속 실패 ${c.consecutiveErrors})", style = DenebType.meta, color = MaterialTheme.colorScheme.error)
            } else if (c.consecutiveErrors > 0) {
                Text("연속 실패 ${c.consecutiveErrors}", style = DenebType.meta, color = MaterialTheme.colorScheme.error)
            }
            if (c.lastError.isNotBlank()) {
                Text("마지막 오류: ${c.lastError}", style = DenebType.meta, color = MaterialTheme.colorScheme.error)
            }

            Spacer(Modifier.height(16.dp))
            HorizontalDivider(color = denebHairline())
            DenebSectionLabel("작업")
            Text(
                payloadKindLabel(c.payloadKind) + if (c.model.isNotBlank()) "  ·  ${c.model}" else "",
                style = DenebType.meta,
                color = denebHint(),
            )
            if (c.prompt.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                MarkdownContent(c.prompt, baseStyle = MaterialTheme.typography.bodyMedium)
            }

            if (c.deliveryChannel.isNotBlank() || c.deliveryTo.isNotBlank()) {
                DenebSectionLabel("전달")
                Text(
                    listOf(c.deliveryChannel, c.deliveryTo).filter { it.isNotBlank() }.joinToString("  ·  "),
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }

            Spacer(Modifier.height(16.dp))
            HorizontalDivider(color = denebHairline())
            Spacer(Modifier.height(12.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(
                    enabled = !busy,
                    onClick = {
                        haptics.tap()
                        scope.launch {
                            busy = true
                            status = if (client.runCron(c.id)) "실행 요청됨" else "실행 실패"
                            busy = false
                        }
                    },
                ) { Text("지금 실행") }
                OutlinedButton(
                    enabled = !busy,
                    onClick = {
                        haptics.tap()
                        onEdit(c.id)
                    },
                ) { Text("편집") }
                OutlinedButton(
                    enabled = !busy,
                    onClick = {
                        haptics.reject()
                        confirmDelete = true
                    },
                ) { Text("삭제") }
            }
            status?.let {
                Spacer(Modifier.height(8.dp))
                Text(it, style = DenebType.meta, color = denebHint())
            }

            if (confirmDelete) {
                AlertDialog(
                    onDismissRequest = { confirmDelete = false },
                    title = { Text("크론 삭제") },
                    text = { Text("이 예약 작업을 삭제할까요? 되돌릴 수 없습니다.") },
                    confirmButton = {
                        TextButton(onClick = {
                            haptics.reject()
                            confirmDelete = false
                            scope.launch {
                                busy = true
                                if (client.removeCron(c.id)) {
                                    onBack()
                                } else {
                                    status = "삭제 실패"
                                    busy = false
                                }
                            }
                        }) { Text("삭제") }
                    },
                    dismissButton = {
                        TextButton(onClick = { confirmDelete = false }) { Text("취소") }
                    },
                )
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
