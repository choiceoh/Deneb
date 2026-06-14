package ai.deneb.deneb

import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.components.rememberHaptics
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Schedule
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
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Settings hub "크론" tab: the scheduled-job list; tapping a row deep-links into
 * the cron detail/edit screen. Hosted by [DenebConfigScreen]'s pager. The jobs sit
 * in a grouped inset card ([DenebGroup] + [DenebListRow]) — the settings idiom —
 * each row a clock-icon title (its cron spec as the subtitle) with a chevron.
 */
@Composable
internal fun CronTab(client: DenebGatewayClient, onOpenCron: (String) -> Unit) {
    val crons by client.denebScheduledTasks.collectAsState()
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    var loadFailed by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { loadFailed = !client.loadScheduledTasks() }
    when {
        crons.isEmpty() && loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError(
                "예약 작업을 불러오지 못했습니다.",
                onRetry = { scope.launch { loadFailed = !client.loadScheduledTasks() } },
            )
        }

        crons.isEmpty() -> EmptyTab("예약된 작업이 없습니다.")

        else -> Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(vertical = 12.dp),
        ) {
            DenebGroup {
                crons.forEachIndexed { i, cron ->
                    DenebListRow(
                        title = cron.description.ifBlank { cron.id },
                        onClick = {
                            haptics.tap()
                            onOpenCron(cron.id)
                        },
                        icon = Icons.Outlined.Schedule,
                        subtitle = cron.cron,
                        divider = i < crons.lastIndex,
                    )
                }
            }
        }
    }
}
