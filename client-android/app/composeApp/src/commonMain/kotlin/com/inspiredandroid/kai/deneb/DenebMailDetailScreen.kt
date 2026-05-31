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

/**
 * Full Gmail message + triage actions, backed by `miniapp.gmail.get` and the
 * mark_read / archive / trash / analyze RPCs. Opening a message marks it read;
 * archive and trash drop it from the inbox list and pop back to it.
 */
@Composable
fun DenebMailDetailScreen(
    client: DenebGatewayClient,
    messageId: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val scope = rememberCoroutineScope()
    var detail by remember(messageId) { mutableStateOf<MailDetail?>(null) }
    var analysis by remember(messageId) { mutableStateOf<String?>(null) }
    var analyzing by remember(messageId) { mutableStateOf(false) }

    // Open marks the message read (server + optimistic list dot), like the Mini App.
    LaunchedEffect(messageId) {
        detail = client.fetchMailDetail(messageId)
        client.markMailRead(messageId)
    }

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
                "메일",
                style = MaterialTheme.typography.headlineSmall,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = onBack) { Text("닫기") }
        }
        Spacer(Modifier.height(8.dp))

        val mail = detail
        if (mail == null) {
            Text(
                "불러오는 중…",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else {
            Text(
                mail.subject.ifBlank { "(제목 없음)" },
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                mail.from,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (mail.date.isNotBlank()) {
                Text(
                    mail.date,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(12.dp))

            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                OutlinedButton(
                    onClick = { scope.launch { if (client.archiveMail(mail.id)) onBack() } },
                    modifier = Modifier.weight(1f),
                ) { Text("📁 보관") }
                OutlinedButton(
                    onClick = { scope.launch { if (client.trashMail(mail.id)) onBack() } },
                    modifier = Modifier.weight(1f),
                ) { Text("🗑 휴지통") }
            }
            Spacer(Modifier.height(8.dp))
            Button(
                onClick = {
                    scope.launch {
                        analyzing = true
                        analysis = client.analyzeMail(mail.id) ?: "분석을 가져오지 못했습니다."
                        analyzing = false
                    }
                },
                enabled = !analyzing,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(if (analyzing) "분석 중…" else "🤖 AI 분석") }

            analysis?.let { text ->
                Spacer(Modifier.height(12.dp))
                Text(
                    "AI 분석",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.primary,
                )
                Spacer(Modifier.height(4.dp))
                Text(text, style = MaterialTheme.typography.bodyMedium)
            }

            Spacer(Modifier.height(16.dp))
            HorizontalDivider()
            Spacer(Modifier.height(12.dp))
            Text(
                mail.body.ifBlank { "(본문 없음)" },
                style = MaterialTheme.typography.bodyMedium,
            )
            if (mail.attachments.isNotEmpty()) {
                Spacer(Modifier.height(12.dp))
                Text(
                    "첨부: ${mail.attachments.joinToString(", ")}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
