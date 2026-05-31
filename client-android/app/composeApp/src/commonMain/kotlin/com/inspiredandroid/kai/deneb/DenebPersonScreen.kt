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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

/**
 * Person relationship context (`miniapp.gmail.sender_context`): recent volume
 * and the wiki pages that mention this person. Surface-wrapped for dark mode.
 */
@Composable
fun DenebPersonScreen(
    client: DenebGatewayClient,
    sender: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var ctx by remember(sender) { mutableStateOf<SenderContext?>(null) }
    var loadFailed by remember(sender) { mutableStateOf(false) }

    LaunchedEffect(sender) {
        val c = client.fetchSenderContext(sender)
        ctx = c
        loadFailed = c == null
    }

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
                if (loadFailed) DenebError("정보를 불러오지 못했습니다.") else DenebLoading()
            } else {
                Text(
                    c.displayName,
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                if (c.email.isNotBlank()) {
                    Text(
                        c.email,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
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
                    Text(c.wikiFacts, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface)
                }
                if (c.wikiHits.isNotEmpty()) {
                    Spacer(Modifier.height(16.dp))
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    Spacer(Modifier.height(12.dp))
                    Text("관련 위키", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                    c.wikiHits.forEach { hit ->
                        Spacer(Modifier.height(8.dp))
                        Text(hit.title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium, color = MaterialTheme.colorScheme.onSurface)
                        if (hit.summary.isNotBlank()) {
                            Text(hit.summary, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                if (c.recentCount == 0 && c.wikiHits.isEmpty() && c.wikiFacts.isBlank()) {
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
}
