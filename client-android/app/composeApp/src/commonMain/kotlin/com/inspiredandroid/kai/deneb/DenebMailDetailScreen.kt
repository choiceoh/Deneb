package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ElevatedCard
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
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
 * Full Gmail message + triage, backed by gmail.get / mark_read / archive /
 * trash / analyze / ask / sender_context. Opening marks read; archive and
 * trash drop the row and pop back. AI analysis and sender context expand into
 * elevated cards; the ask box runs free-form Q&A about the message.
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
    var senderCtx by remember(messageId) { mutableStateOf<SenderContext?>(null) }
    var loadingSender by remember(messageId) { mutableStateOf(false) }
    var askText by remember(messageId) { mutableStateOf("") }
    var asking by remember(messageId) { mutableStateOf(false) }
    val qa = remember(messageId) { mutableStateListOf<Pair<String, String>>() }
    var loadFailed by remember(messageId) { mutableStateOf(false) }

    LaunchedEffect(messageId) {
        val d = client.fetchMailDetail(messageId)
        detail = d
        loadFailed = d == null
        if (d != null) client.markMailRead(messageId)
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
    Column(
        modifier = Modifier
            .statusBarsPadding()
            .padding(16.dp)
            .verticalScroll(rememberScrollState()),
    ) {
        if (navigationTabBar != null) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
            Spacer(Modifier.height(12.dp))
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onBack) { Text("← 뒤로") }
        }
        Spacer(Modifier.height(4.dp))

        val mail = detail
        if (mail == null) {
            if (loadFailed) {
                DenebError("메일을 불러오지 못했습니다.")
            } else {
                DenebLoading()
            }
        } else {
            Column(Modifier.fillMaxWidth()) {
                Text(
                    mail.subject.ifBlank { "(제목 없음)" },
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    mail.from,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                if (mail.date.isNotBlank()) {
                    Text(
                        mail.date.take(16).replace('T', ' '),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            Spacer(Modifier.height(16.dp))

            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilledTonalButton(
                    onClick = { scope.launch { if (client.archiveMail(mail.id)) onBack() } },
                    modifier = Modifier.weight(1f),
                ) { Text("보관") }
                FilledTonalButton(
                    onClick = { scope.launch { if (client.trashMail(mail.id)) onBack() } },
                    modifier = Modifier.weight(1f),
                ) { Text("휴지통") }
            }
            Spacer(Modifier.height(8.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(
                    onClick = {
                        scope.launch {
                            analyzing = true
                            analysis = client.analyzeMail(mail.id) ?: "분석을 가져오지 못했습니다."
                            analyzing = false
                        }
                    },
                    enabled = !analyzing,
                    modifier = Modifier.weight(1f),
                ) { Text(if (analyzing) "분석 중…" else "AI 분석") }
                OutlinedButton(
                    onClick = {
                        scope.launch {
                            loadingSender = true
                            senderCtx = client.fetchSenderContext(mail.from)
                            loadingSender = false
                        }
                    },
                    enabled = !loadingSender,
                    modifier = Modifier.weight(1f),
                ) { Text(if (loadingSender) "불러오는 중…" else "발신자") }
            }

            if (analyzing || analysis != null) {
                Spacer(Modifier.height(12.dp))
                ElevatedCard(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp)) {
                        Text(
                            "AI 분석",
                            style = MaterialTheme.typography.titleSmall,
                            color = MaterialTheme.colorScheme.primary,
                        )
                        Spacer(Modifier.height(6.dp))
                        if (analyzing) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                                Spacer(Modifier.width(8.dp))
                                Text(
                                    "분석 중… (최대 4분)",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        } else {
                            Text(analysis ?: "", style = MaterialTheme.typography.bodyMedium)
                        }
                    }
                }
            }

            senderCtx?.let { ctx ->
                Spacer(Modifier.height(12.dp))
                ElevatedCard(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp)) {
                        Text(
                            "발신자 컨텍스트",
                            style = MaterialTheme.typography.titleSmall,
                            color = MaterialTheme.colorScheme.primary,
                        )
                        Spacer(Modifier.height(6.dp))
                        Text(ctx.displayName, style = MaterialTheme.typography.titleSmall)
                        if (ctx.recentCount > 0) {
                            Text(
                                "최근 ${ctx.windowDays}일 · ${ctx.recentCount}통 수신",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        if (ctx.wikiFacts.isNotBlank()) {
                            Spacer(Modifier.height(8.dp))
                            Text(ctx.wikiFacts, style = MaterialTheme.typography.bodyMedium)
                        }
                        ctx.wikiHits.forEach { hit ->
                            Spacer(Modifier.height(8.dp))
                            Text(hit.title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
                            if (hit.summary.isNotBlank()) {
                                Text(
                                    hit.summary,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                        if (ctx.recentCount == 0 && ctx.wikiHits.isEmpty() && ctx.wikiFacts.isBlank()) {
                            Text(
                                "알려진 컨텍스트 없음",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }

            Spacer(Modifier.height(16.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
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

            Spacer(Modifier.height(20.dp))
            Text("이 메일에 질문", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.height(8.dp))
            qa.forEach { (question, answer) ->
                Column(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
                    Text("Q. $question", style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                    Spacer(Modifier.height(2.dp))
                    Text(answer, style = MaterialTheme.typography.bodyMedium)
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = askText,
                    onValueChange = { askText = it },
                    placeholder = { Text("질문 입력…") },
                    modifier = Modifier.weight(1f),
                    enabled = !asking,
                )
                Spacer(Modifier.width(8.dp))
                TextButton(
                    onClick = {
                        val q = askText.trim()
                        if (q.isNotEmpty() && !asking) {
                            askText = ""
                            scope.launch {
                                asking = true
                                val a = client.askMail(mail.id, q) ?: "답변을 가져오지 못했습니다."
                                qa.add(q to a)
                                asking = false
                            }
                        }
                    },
                    enabled = !asking,
                ) { Text(if (asking) "…" else "질문") }
            }
            Spacer(Modifier.height(24.dp))
        }
    }
    }
}

/** "Name <email>" -> "Name"; a bare address is returned as-is. */
private fun displayName(from: String): String {
    val lt = from.indexOf('<')
    return if (lt > 0) from.substring(0, lt).trim().trim('"') else from.trim()
}
