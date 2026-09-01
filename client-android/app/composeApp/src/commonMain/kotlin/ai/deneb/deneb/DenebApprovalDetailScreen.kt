package ai.deneb.deneb

import ai.deneb.network.httpTeardownTolerantHandler
import ai.deneb.openUrl
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.icons.outlined.AutoAwesome
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

internal const val APPROVAL_QA_GROUNDING_LABEL = "본문 및 가용한 첨부 근거"

private val approvalTeardownHandler = httpTeardownTolerantHandler("ApprovalDetail")

/**
 * 전자결재 상세 — AI 분석 위, 결재선·첨부는 접기, 본문 분리, 미결 시 하단 고정 승인/반려.
 */
@Composable
fun DenebApprovalDetailScreen(
    client: DenebGatewayClient,
    docId: String,
    title: String = "",
    drafter: String = "",
    date: String = "",
    canAct: Boolean = false,
    folder: String = "",
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
    // Approvals are irreversible on the groupware side, so a failed 결재 must keep
    // the row and say so. It used to just close the dialog: indistinguishable from
    // success until the list refreshed and the document was still pending.
    var actError by remember(docId) { mutableStateOf<String?>(null) }
    var lineOpen by remember(docId) { mutableStateOf(false) }
    var attachOpen by remember(docId) { mutableStateOf(false) }

    suspend fun load() {
        loadFailed = false
        body = null
        coroutineScope {
            val analysisDeferred = async { client.fetchCachedApprovalAnalysis(docId) }
            val fetched = client.fetchApprovalBody(docId, title, folder)
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
        Column(Modifier.fillMaxWidth().weight(1f)) {
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

                val sections = remember(doc.body) { parseApprovalDocBody(doc.body) }

                // Document header: 양식 badge + title + 기안 meta. 문서번호/id stay
                // out of the UI — agent plumbing, not operator info.
                if (sections.form.isNotBlank()) {
                    Text(
                        sections.form,
                        style = DenebType.meta.copy(fontWeight = FontWeight.SemiBold),
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier
                            .clip(RoundedCornerShape(999.dp))
                            .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.12f))
                            .padding(horizontal = 8.dp, vertical = 3.dp),
                    )
                    Spacer(Modifier.height(8.dp))
                }
                Text(
                    text = sections.title
                        .ifBlank { title }
                        .ifBlank { doc.title }
                        .ifBlank { "(제목 없음)" },
                    style = DenebType.subject,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(Modifier.height(6.dp))
                val meta = listOfNotNull(
                    sections.drafter.ifBlank { drafter }.takeIf { it.isNotBlank() }?.let { "기안 $it" },
                    sections.draftedAt.ifBlank { date }.takeIf { it.isNotBlank() },
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

                ApprovalSectionGap()
                ApprovalAskBox(
                    client = client,
                    docId = docId,
                    title = title.ifBlank { doc.title },
                    folder = folder,
                )

                ApprovalSectionGap()
                Text("본문", style = DenebType.sectionLabel, color = denebHint())
                Spacer(Modifier.height(8.dp))
                SelectionContainer {
                    val content = sections.body.ifBlank { doc.body }
                    if (content.isBlank()) {
                        Text(
                            "(본문 없음)",
                            style = DenebType.body,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                    } else {
                        MarkdownContent(
                            content = content,
                            modifier = Modifier.fillMaxWidth(),
                            baseStyle = DenebType.body,
                        )
                    }
                }

                // Reference sections live below the read: 결재선·첨부 fold at the bottom.
                if (sections.line.isNotBlank()) {
                    ApprovalSectionGap()
                    ApprovalDisclosure(
                        title = "결재선",
                        teaser = if (sections.lineCount > 0) "${sections.lineCount}명" else null,
                        expanded = lineOpen,
                        onToggle = {
                            haptics.tap()
                            lineOpen = !lineOpen
                        },
                    ) {
                        SelectionContainer {
                            Text(sections.line, style = DenebType.body, color = MaterialTheme.colorScheme.onSurface)
                        }
                    }
                }

                if (sections.attachments.isNotBlank()) {
                    ApprovalSectionGap()
                    ApprovalDisclosure(
                        title = "첨부",
                        teaser = when {
                            sections.attachmentCount > 0 -> "${sections.attachmentCount}건"

                            sections.attachmentHeader.isNotBlank() ->
                                sections.attachmentHeader.removePrefix("첨부").trim()

                            else -> null
                        },
                        expanded = attachOpen,
                        onToggle = {
                            haptics.tap()
                            attachOpen = !attachOpen
                        },
                    ) {
                        val attachmentRows = remember(sections.attachments) {
                            parseAttachmentRows(sections.attachments)
                        }
                        if (attachmentRows.isEmpty()) {
                            SelectionContainer {
                                Text(
                                    sections.attachments,
                                    style = DenebType.body,
                                    color = MaterialTheme.colorScheme.onSurface,
                                )
                            }
                        } else {
                            // Tap opens/downloads the real file via the binary
                            // download route — mail attachment chip parity.
                            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                attachmentRows.forEach { row ->
                                    AssistChip(
                                        onClick = {
                                            haptics.tap()
                                            openUrl(
                                                client.approvalAttachmentUrl(
                                                    docId = docId,
                                                    selector = row.index.toString(),
                                                    filename = row.name,
                                                ),
                                            )
                                        },
                                        label = {
                                            Text(
                                                if (row.meta.isBlank()) row.name else "${row.name}  ${row.meta}",
                                            )
                                        },
                                    )
                                }
                            }
                        }
                    }
                }
                Spacer(Modifier.height(24.dp))
            }

            // Sticky act bar — always visible while scrolling the body (미결 only).
            if (canAct && body != null) {
                HorizontalDivider(color = denebHairline())
                Surface(tonalElevation = 2.dp) {
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 24.dp, vertical = 12.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        FilledTonalButton(
                            onClick = {
                                haptics.confirm()
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
            }
        }
    }

    pendingAct?.let { decision ->
        val label = if (decision == "approve") "승인" else "반려"
        AlertDialog(
            onDismissRequest = { if (!acting) pendingAct = null },
            title = { Text("${label}할까요?") },
            text = {
                Column {
                    Text(
                        buildString {
                            append(
                                title.ifBlank { body?.title.orEmpty() }.ifBlank { "이 결재 문서" },
                            )
                            append("\n그룹웨어에 즉시 반영됩니다.")
                        },
                    )
                    actError?.let { message ->
                        Spacer(Modifier.height(8.dp))
                        Text(message, style = DenebType.meta, color = MaterialTheme.colorScheme.error)
                    }
                }
            },
            confirmButton = {
                TextButton(
                    enabled = !acting,
                    onClick = {
                        // The decision lands here, not on the button that opened the
                        // dialog — 승인 commits, 반려 is the negative commit.
                        if (decision == "approve") haptics.confirm() else haptics.reject()
                        scope.launch {
                            acting = true
                            actError = null
                            val ok = client.actApproval(docId, decision)?.ok == true
                            acting = false
                            if (ok) {
                                pendingAct = null
                                onActed()
                            } else {
                                actError = "처리하지 못했습니다. 연결을 확인하고 다시 시도해 주세요."
                            }
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
private fun ApprovalAskBox(
    client: DenebGatewayClient,
    docId: String,
    title: String,
    folder: String,
) {
    val scope = rememberCoroutineScope { approvalTeardownHandler }
    val haptics = rememberHaptics()
    val history = remember(docId) { mutableStateListOf<Pair<String, String>>() }
    var question by remember(docId) { mutableStateOf("") }
    var asking by remember(docId) { mutableStateOf(false) }
    var error by remember(docId) { mutableStateOf("") }

    fun ask() {
        val q = boundApprovalQAText(question)
        if (q.isEmpty() || asking) return
        haptics.tap()
        asking = true
        error = ""
        question = ""
        scope.launch {
            val answer = client.askApproval(
                docId = docId,
                question = q,
                title = title,
                folder = folder,
                history = history.toList(),
            )
            if (answer == null) {
                error = "답변을 가져오지 못했습니다. 다시 시도해 주세요."
                question = q
            } else {
                history.add(q to answer)
            }
            asking = false
        }
    }

    Column(Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Icon(
                imageVector = Icons.Outlined.Search,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.size(18.dp),
            )
            Spacer(Modifier.size(8.dp))
            Text("근거 질문", style = DenebType.rowTitleStrong)
            Spacer(Modifier.weight(1f))
            Text(APPROVAL_QA_GROUNDING_LABEL, style = DenebType.meta, color = denebHint())
        }
        if (history.isNotEmpty()) {
            Spacer(Modifier.height(10.dp))
            history.forEach { (q, a) ->
                Text("Q. $q", style = DenebType.rowTitleStrong)
                Spacer(Modifier.height(2.dp))
                MarkdownContent(a)
                Spacer(Modifier.height(10.dp))
            }
        }
        if (error.isNotBlank()) {
            Spacer(Modifier.height(6.dp))
            Text(error, style = DenebType.meta, color = MaterialTheme.colorScheme.error)
        }
        Spacer(Modifier.height(8.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(
                value = question,
                onValueChange = { question = it },
                placeholder = { Text(if (asking) "근거를 확인하는 중…" else "예: 첨부 견적과 본문 금액이 같아?") },
                enabled = !asking,
                maxLines = 3,
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.size(8.dp))
            TextButton(onClick = { ask() }, enabled = !asking && question.isNotBlank()) {
                Text(if (asking) "…" else "질문")
            }
        }
    }
}

@Composable
private fun ApprovalSectionGap() {
    Spacer(Modifier.height(16.dp))
    HorizontalDivider(color = denebHairline())
    Spacer(Modifier.height(12.dp))
}

@Composable
private fun ApprovalDisclosure(
    title: String,
    teaser: String?,
    expanded: Boolean,
    onToggle: () -> Unit,
    content: @Composable () -> Unit,
) {
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(10.dp))
                .clickable(onClick = onToggle)
                .padding(vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(title, style = DenebType.sectionLabel, color = denebHint())
            if (!expanded && !teaser.isNullOrBlank()) {
                Text(
                    " · $teaser",
                    style = DenebType.meta,
                    color = denebHint(),
                    maxLines = 1,
                    modifier = Modifier.weight(1f).padding(start = 4.dp),
                )
            } else {
                Spacer(Modifier.weight(1f))
            }
            Icon(
                imageVector = if (expanded) Icons.Default.KeyboardArrowUp else Icons.Default.KeyboardArrowDown,
                contentDescription = if (expanded) "$title 접기" else "$title 펼치기",
                tint = denebHint(),
                modifier = Modifier.size(18.dp),
            )
        }
        AnimatedVisibility(visible = expanded, enter = denebExpandIn, exit = denebShrinkOut) {
            Column {
                Spacer(Modifier.height(6.dp))
                content()
            }
        }
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
