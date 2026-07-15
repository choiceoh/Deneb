package ai.deneb.deneb

import ai.deneb.network.httpTeardownTolerantHandler
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private val approvalTeardownHandler = httpTeardownTolerantHandler("ApprovalDetail")

/**
 * 전자결재 상세 — AI 분석(캐시 우선) 위, 본문 아래, 미결 시 하단 승인/반려.
 * 메일 상세 패리티를 따르되 Q&A·접기 없이 단순하게 유지.
 */
@Composable
fun DenebApprovalDetailScreen(
    client: DenebGatewayClient,
    docId: String,
    title: String = "",
    drafter: String = "",
    date: String = "",
    canAct: Boolean = false,
    onBack: () -> Unit,
    onActed: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val scope = rememberCoroutineScope { approvalTeardownHandler }
    val haptics = rememberHaptics()
    var body by remember(docId) { mutableStateOf<ApprovalBody?>(null) }
    var analysis by remember(docId) { mutableStateOf<ApprovalAnalysis?>(null) }
    var analyzing by remember(docId) { mutableStateOf(false) }
    var analysisFailed by remember(docId) { mutableStateOf(false) }
    var loadFailed by remember(docId) { mutableStateOf(false) }
    var acting by remember(docId) { mutableStateOf(false) }
    var pendingAct by remember(docId) { mutableStateOf<String?>(null) }

    suspend fun load() {
        loadFailed = false
        body = null
        coroutineScope {
            val analysisDeferred = async { client.fetchCachedApprovalAnalysis(docId) }
            val fetched = client.fetchApprovalBody(docId, title)
            body = fetched
            loadFailed = fetched == null
            if (fetched != null) {
                analysis = analysisDeferred.await()
                if (analysis == null) {
                    analyzing = true
                    analysisFailed = false
                    analysis = client.analyzeApproval(
                        docId = docId,
                        title = title.ifBlank { fetched.title },
                        drafter = drafter,
                        date = date,
                    )
                    if (analysis == null) analysisFailed = true
                    analyzing = false
                }
            } else {
                analysisDeferred.cancel()
            }
        }
    }
    LaunchedEffect(docId) { withContext(approvalTeardownHandler) { load() } }

    fun runAnalysis(force: Boolean) {
        scope.launch {
            analyzing = true
            analysisFailed = false
            val a = client.analyzeApproval(
                docId = docId,
                title = title.ifBlank { body?.title.orEmpty() },
                force = force,
                drafter = drafter,
                date = date,
            )
            if (a != null) {
                analysis = a
            } else {
                analysisFailed = true
            }
            analyzing = false
        }
    }

    DenebScreenScaffold(title = "결재", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(8.dp))
            val doc = body
            if (doc == null) {
                if (loadFailed) {
                    DenebError("결재 문서를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
                } else {
                    DenebLoading()
                }
                return@Column
            }

            Text(
                text = title.ifBlank { doc.title }.ifBlank { "(제목 없음)" },
                style = DenebType.subject,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.height(6.dp))
            val meta = listOfNotNull(
                drafter.takeIf { it.isNotBlank() }?.let { "기안 $it" },
                date.takeIf { it.isNotBlank() },
            ).joinToString(" · ")
            if (meta.isNotBlank()) {
                Text(meta, style = DenebType.meta, color = denebHint())
            }
            Spacer(Modifier.height(16.dp))

            ApprovalAnalysisCard(
                analysis = analysis,
                analyzing = analyzing,
                failed = analysisFailed,
                onRerun = {
                    haptics.tap()
                    runAnalysis(force = true)
                },
            )

            Spacer(Modifier.height(16.dp))
            HorizontalDivider(color = denebHairline())
            Spacer(Modifier.height(12.dp))
            Text("본문", style = DenebType.sectionLabel, color = denebHint())
            Spacer(Modifier.height(8.dp))
            // Same renderer as chat/피드 — Amaranth bodies ship GFM tables (and
            // occasional <br> cell breaks). Plain Text leaked raw "| … |" pipes.
            SelectionContainer {
                if (doc.body.isBlank()) {
                    Text(
                        "(본문 없음)",
                        style = DenebType.body,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                } else {
                    MarkdownContent(
                        content = doc.body,
                        modifier = Modifier.fillMaxWidth(),
                        baseStyle = DenebType.body,
                    )
                }
            }

            if (canAct) {
                Spacer(Modifier.height(20.dp))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilledTonalButton(
                        onClick = {
                            haptics.tap()
                            pendingAct = "approve"
                        },
                        enabled = !acting,
                        modifier = Modifier.weight(1f),
                    ) { Text("승인") }
                    FilledTonalButton(
                        onClick = {
                            haptics.reject()
                            pendingAct = "reject"
                        },
                        enabled = !acting,
                        modifier = Modifier.weight(1f),
                    ) { Text("반려") }
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }

    pendingAct?.let { decision ->
        val label = if (decision == "approve") "승인" else "반려"
        AlertDialog(
            onDismissRequest = { if (!acting) pendingAct = null },
            title = { Text("${label}할까요?") },
            text = {
                Text(
                    buildString {
                        append(
                            title.ifBlank { body?.title.orEmpty() }.ifBlank { "이 결재 문서" },
                        )
                        append("\n그룹웨어에 즉시 반영됩니다.")
                    },
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !acting,
                    onClick = {
                        scope.launch {
                            acting = true
                            val ok = client.actApproval(docId, decision)?.ok == true
                            acting = false
                            pendingAct = null
                            if (ok) onActed()
                        }
                    },
                ) { Text(label) }
            },
            dismissButton = {
                TextButton(enabled = !acting, onClick = { pendingAct = null }) {
                    Text("취소")
                }
            },
        )
    }
}

@Composable
private fun ApprovalAnalysisCard(
    analysis: ApprovalAnalysis?,
    analyzing: Boolean,
    failed: Boolean,
    onRerun: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(denebInsightContainer())
            .padding(16.dp),
    ) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Icon(
                imageVector = Icons.Outlined.AutoAwesome,
                contentDescription = null,
                tint = denebInsight(),
                modifier = Modifier.size(20.dp),
            )
            Spacer(Modifier.size(10.dp))
            Text("AI 분석", style = DenebType.rowTitleStrong, color = denebInsight())
            Spacer(Modifier.weight(1f))
            analysis?.let { a ->
                Text(
                    if (a.cached) "저장됨 · ${a.createdAt.take(10)}" else "${a.durationMs / 1000}s",
                    style = DenebType.meta,
                    color = denebHint(),
                )
                Spacer(Modifier.size(8.dp))
                TextButton(onClick = onRerun, enabled = !analyzing) { Text("다시 분석") }
            }
        }
        Spacer(Modifier.height(8.dp))
        when {
            analyzing -> Row(verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp, color = denebInsight())
                Spacer(Modifier.size(8.dp))
                Text("분석 중…", style = DenebType.rowSubtitle, color = denebHint())
            }

            analysis != null -> MarkdownContent(analysis.text)

            failed -> Text(
                "분석을 가져오지 못했습니다.",
                style = DenebType.rowSubtitle,
                color = MaterialTheme.colorScheme.error,
            )

            else -> Text("분석 없음", style = DenebType.rowSubtitle, color = denebHint())
        }
    }
}
