package ai.deneb.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Settings hub "크론" tab: the scheduled-job list; tapping a row deep-links into
 * the cron detail/edit screen. Hosted by [DenebConfigScreen]'s pager.
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
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(crons, key = { it.id }) { cron ->
                Column(
                    Modifier.animateItem().fillMaxWidth().clickable { haptics.tap(); onOpenCron(cron.id) }.padding(horizontal = 16.dp, vertical = 14.dp),
                ) {
                    Text(
                        cron.description.ifBlank { cron.id },
                        style = MaterialTheme.typography.bodyLarge,
                        fontWeight = FontWeight.Medium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    cron.cron?.let {
                        Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
            }
        }
    }
}
