package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Recent-diary timeline (`miniapp.memory.diary_recent`). Deneb writes a daily
 * diary as part of normal operation; this is the "what's been happening lately
 * in my world" view — a vertical list of entries, newest first, each rendered
 * as markdown. Read-only (the agent and the dreamer own diary writes).
 */
@Composable
fun DenebDiaryScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var entries by remember { mutableStateOf<List<DiaryEntry>?>(null) }
    var loadFailed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        loadFailed = false
        entries = null
        val e = client.fetchRecentDiary()
        entries = e
        loadFailed = e == null
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "최근 일기", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(12.dp))

            val list = entries
            when {
                list == null && loadFailed -> DenebError(
                    "일기를 불러오지 못했습니다.",
                    onRetry = { scope.launch { load() } },
                )

                list == null -> DenebLoading()

                list.isEmpty() -> DenebEmpty("아직 기록된 일기가 없습니다.")

                else -> {
                    list.forEach { entry ->
                        if (entry.header.isNotBlank()) {
                            Text(
                                entry.header,
                                style = DenebType.cardTitle,
                                color = MaterialTheme.colorScheme.onSurface,
                            )
                            Spacer(Modifier.height(4.dp))
                        }
                        DenebMarkdown(entry.content.ifBlank { "(빈 항목)" })
                        Spacer(Modifier.height(12.dp))
                        HorizontalDivider(color = denebHairline())
                        Spacer(Modifier.height(12.dp))
                    }
                    Spacer(Modifier.height(12.dp))
                }
            }
        }
    }
}
