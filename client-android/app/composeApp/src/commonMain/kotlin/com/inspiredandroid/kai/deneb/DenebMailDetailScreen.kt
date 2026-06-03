@file:OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)

package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.material3.AssistChip
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
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Full Gmail message + triage. On open it marks read and fetches any cached
 * analysis instantly (no LLM). AI analysis renders markdown + related-project
 * chips (-> wiki) + a rerun; the ask box keeps multi-turn history; attachments
 * are tappable chips that download via the gateway. Archive/trash pop back.
 */
@Composable
fun DenebMailDetailScreen(
    client: DenebGatewayClient,
    messageId: String,
    onBack: () -> Unit,
    onOpenWiki: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    val uriHandler = LocalUriHandler.current
    var detail by remember(messageId) { mutableStateOf<MailDetail?>(null) }
    var analysis by remember(messageId) { mutableStateOf<MailAnalysis?>(null) }
    var analyzing by remember(messageId) { mutableStateOf(false) }
    var analysisFailed by remember(messageId) { mutableStateOf(false) }
    var senderCtx by remember(messageId) { mutableStateOf<SenderContext?>(null) }
    var loadingSender by remember(messageId) { mutableStateOf(false) }
    var askText by remember(messageId) { mutableStateOf("") }
    var asking by remember(messageId) { mutableStateOf(false) }
    val qa = remember(messageId) { mutableStateListOf<Pair<String, String>>() }
    var loadFailed by remember(messageId) { mutableStateOf(false) }
    var actionMsg by remember(messageId) { mutableStateOf<String?>(null) }

    suspend fun load() {
        loadFailed = false
        detail = null
        val d = client.fetchMailDetail(messageId)
        detail = d
        loadFailed = d == null
        if (d != null) {
            client.markMailRead(messageId)
            // Instant: show a previously-computed analysis without spending an LLM call.
            analysis = client.fetchCachedAnalysis(messageId)
        }
    }
    LaunchedEffect(messageId) { load() }

    fun runAnalysis(force: Boolean) {
        scope.launch {
            analyzing = true
            analysisFailed = false
            val a = client.analyzeMail(messageId, force)
            if (a != null) analysis = a else analysisFailed = true
            analyzing = false
        }
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

            val mail = detail
            if (mail == null) {
                if (loadFailed) {
                    DenebError("메일을 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                } else {
                    DenebLoading()
                }
                return@Column
            }

            Text(
                mail.subject.ifBlank { "(제목 없음)" },
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.height(6.dp))
            Text(mail.from, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface)
            if (mail.date.isNotBlank()) {
                Text(
                    shortDate(mail.date),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(16.dp))

            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilledTonalButton(
                    onClick = {
                        haptics.confirm()
                        scope.launch { if (client.archiveMail(mail.id)) onBack() else actionMsg = "보관 실패" }
                    },
                    modifier = Modifier.weight(1f),
                ) { Text("보관") }
                FilledTonalButton(
                    onClick = {
                        haptics.confirm()
                        scope.launch { if (client.trashMail(mail.id)) onBack() else actionMsg = "휴지통 이동 실패" }
                    },
                    modifier = Modifier.weight(1f),
                ) { Text("휴지통") }
            }
            actionMsg?.let {
                Spacer(Modifier.height(6.dp))
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }
            Spacer(Modifier.height(8.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(
                    onClick = { haptics.tap(); runAnalysis(force = false) },
                    enabled = !analyzing,
                    modifier = Modifier.weight(1f),
                ) { Text(if (analyzing) "분석 중…" else "AI 분석") }
                OutlinedButton(
                    onClick = {
                        haptics.tap()
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

            if (analyzing || analysis != null || analysisFailed) {
                Spacer(Modifier.height(12.dp))
                ElevatedCard(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                "AI 분석",
                                style = MaterialTheme.typography.titleSmall,
                                color = MaterialTheme.colorScheme.primary,
                                modifier = Modifier.weight(1f),
                            )
                            analysis?.let { a ->
                                Text(
                                    if (a.cached) "저장됨 · ${a.createdAt.take(10)}" else "${a.durationMs / 1000}s",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                Spacer(Modifier.width(8.dp))
                                TextButton(onClick = { haptics.tap(); runAnalysis(force = true) }, enabled = !analyzing) { Text("다시") }
                            }
                        }
                        Spacer(Modifier.height(6.dp))
                        when {
                            analyzing -> Row(verticalAlignment = Alignment.CenterVertically) {
                                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                                Spacer(Modifier.width(8.dp))
                                Text("분석 중…", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            analysis != null -> {
                                DenebMarkdown(analysis!!.text)
                                if (analysis!!.related.isNotEmpty()) {
                                    Spacer(Modifier.height(10.dp))
                                    Text("관련 프로젝트", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    Spacer(Modifier.height(4.dp))
                                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                        analysis!!.related.forEach { rp ->
                                            AssistChip(
                                                onClick = { onOpenWiki(rp.path) },
                                                label = { Text(rp.title.ifBlank { rp.path }) },
                                            )
                                        }
                                    }
                                }
                            }
                            else -> Text(
                                "분석을 가져오지 못했습니다.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.error,
                            )
                        }
                    }
                }
            }

            senderCtx?.let { ctx ->
                Spacer(Modifier.height(12.dp))
                ElevatedCard(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp)) {
                        Text("발신자 컨텍스트", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                        Spacer(Modifier.height(6.dp))
                        Text(ctx.displayName, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
                        if (ctx.recentCount > 0) {
                            Text(
                                "최근 ${ctx.windowDays}일 · ${ctx.recentCount}통 수신",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        if (ctx.wikiFacts.isNotBlank()) {
                            Spacer(Modifier.height(8.dp))
                            DenebMarkdown(ctx.wikiFacts)
                        }
                        ctx.wikiHits.forEach { hit ->
                            Spacer(Modifier.height(8.dp))
                            Text(hit.title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium, color = MaterialTheme.colorScheme.onSurface)
                            if (hit.summary.isNotBlank()) {
                                Text(hit.summary, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        if (ctx.recentCount == 0 && ctx.wikiHits.isEmpty() && ctx.wikiFacts.isBlank()) {
                            Text("알려진 컨텍스트 없음", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
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
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (mail.bodyTotal > mail.body.length) {
                Text(
                    "${mail.bodyTotal}자 중 일부만 표시",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (mail.attachments.isNotEmpty()) {
                Spacer(Modifier.height(12.dp))
                Text("첨부 ${mail.attachments.size}", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.height(4.dp))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    mail.attachments.forEach { att ->
                        AssistChip(
                            onClick = { uriHandler.openUri(client.attachmentUrl(mail.id, att)) },
                            label = { Text(att.filename + "  " + humanBytes(att.size.toLong())) },
                        )
                    }
                }
            }

            Spacer(Modifier.height(20.dp))
            Text("이 메일에 질문", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurface)
            Spacer(Modifier.height(8.dp))
            qa.forEach { (question, answer) ->
                Column(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
                    Text("Q. $question", style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurface)
                    Spacer(Modifier.height(2.dp))
                    DenebMarkdown(answer)
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
                            haptics.tap()
                            askText = ""
                            scope.launch {
                                asking = true
                                // Send prior turns so follow-ups have context.
                                val a = client.askMail(mail.id, q, qa.toList()) ?: "답변을 가져오지 못했습니다."
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
