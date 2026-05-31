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
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

/**
 * Native recent-mail view backed by the gateway's `miniapp.gmail.list_recent`
 * RPC. Read-only for now (sender / subject / snippet / unread); detail and
 * actions (open, archive) are a follow-up.
 */
@Composable
fun DenebMailScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenDetail: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val mail by client.denebMail.collectAsState()
    LaunchedEffect(Unit) { client.refreshMail() }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .padding(16.dp)
            .verticalScroll(rememberScrollState()),
    ) {
        if (navigationTabBar != null) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) {
                navigationTabBar()
            }
            Spacer(Modifier.height(16.dp))
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "받은 메일",
                style = MaterialTheme.typography.headlineSmall,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = onBack) { Text("닫기") }
        }
        Spacer(Modifier.height(8.dp))

        if (mail.isEmpty()) {
            Text(
                "불러오는 중…",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else {
            mail.forEach { m ->
                Column(
                    Modifier.fillMaxWidth()
                        .clickable { onOpenDetail(m.id) }
                        .padding(vertical = 10.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (m.unread) {
                            Text("● ", color = MaterialTheme.colorScheme.primary)
                        }
                        Text(
                            senderName(m.from),
                            style = MaterialTheme.typography.bodyMedium,
                            fontWeight = if (m.unread) FontWeight.Bold else FontWeight.Normal,
                            maxLines = 1,
                            modifier = Modifier.weight(1f),
                        )
                        Text(
                            shortDate(m.date),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Spacer(Modifier.height(2.dp))
                    Text(
                        m.subject.ifBlank { "(제목 없음)" },
                        style = MaterialTheme.typography.bodyLarge,
                        fontWeight = if (m.unread) FontWeight.SemiBold else FontWeight.Normal,
                        maxLines = 1,
                    )
                    if (m.snippet.isNotBlank()) {
                        Text(
                            m.snippet,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 2,
                        )
                    }
                }
                HorizontalDivider()
            }
        }
    }
}

/** "Name <email>" → "Name"; a bare address is returned as-is. */
private fun senderName(from: String): String {
    val lt = from.indexOf('<')
    return if (lt > 0) from.substring(0, lt).trim().trim('"') else from.trim()
}

/** "2026-05-30T12:41:31Z" → "05-30 12:41". */
private fun shortDate(date: String): String =
    if (date.length >= 16) date.substring(5, 16).replace('T', ' ') else date
