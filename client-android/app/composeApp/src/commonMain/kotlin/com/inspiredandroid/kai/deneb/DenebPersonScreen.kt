package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
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
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Person dossier (`miniapp.gmail.sender_context` + `list_recent from:`): recent
 * volume, the wiki pages that mention them (tap -> page), and their recent
 * messages (tap -> mail detail). Surface-wrapped for dark mode.
 */
@Composable
fun DenebPersonScreen(
    client: DenebGatewayClient,
    sender: String,
    onBack: () -> Unit,
    onOpenMail: (String) -> Unit = {},
    onOpenWiki: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var ctx by remember(sender) { mutableStateOf<SenderContext?>(null) }
    var loadFailed by remember(sender) { mutableStateOf(false) }
    var recent by remember(sender) { mutableStateOf<List<MailMessage>?>(null) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun load() {
        loadFailed = false
        ctx = null
        val c = client.fetchSenderContext(sender)
        ctx = c
        loadFailed = c == null
        val email = c?.email?.ifBlank { sender } ?: sender
        recent = client.fetchRecentFromSender(email)
    }
    LaunchedEffect(sender) { load() }

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

            val c = ctx
            if (c == null) {
                if (loadFailed) {
                    DenebError("정보를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                } else {
                    DenebLoading()
                }
                return@Column
            }

            Text(
                c.displayName,
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (c.email.isNotBlank()) {
                Text(c.email, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (c.recentCount > 0) {
                Spacer(Modifier.height(8.dp))
                Text(
                    "최근 ${c.windowDays}일 · ${c.recentCount}통 수신",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
            }
            if (c.wikiFacts.isNotBlank()) {
                Spacer(Modifier.height(12.dp))
                DenebMarkdown(c.wikiFacts)
            }

            if (c.wikiHits.isNotEmpty()) {
                Spacer(Modifier.height(16.dp))
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                Spacer(Modifier.height(12.dp))
                Text("관련 위키", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                c.wikiHits.forEach { hit ->
                    Column(
                        Modifier
                            .fillMaxWidth()
                            .then(if (hit.path.isNotBlank()) Modifier.clickable { haptics.tap(); onOpenWiki(hit.path) } else Modifier)
                            .padding(vertical = 8.dp),
                    ) {
                        Text(hit.title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium, color = MaterialTheme.colorScheme.onSurface)
                        if (hit.summary.isNotBlank()) {
                            Text(hit.summary, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }

            val mail = recent
            if (!mail.isNullOrEmpty()) {
                Spacer(Modifier.height(16.dp))
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                Spacer(Modifier.height(12.dp))
                Text("최근 메일 ${mail.size}", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                Spacer(Modifier.height(4.dp))
                mail.forEach { m ->
                    MailRow(
                        message = m,
                        selecting = false,
                        isSelected = false,
                        onTap = { haptics.tap(); onOpenMail(m.id) },
                        onLongPress = {},
                    )
                }
            }

            if (c.recentCount == 0 && c.wikiHits.isEmpty() && c.wikiFacts.isBlank() && mail.isNullOrEmpty()) {
                Spacer(Modifier.height(12.dp))
                Text(
                    "알려진 컨텍스트가 없습니다.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}
