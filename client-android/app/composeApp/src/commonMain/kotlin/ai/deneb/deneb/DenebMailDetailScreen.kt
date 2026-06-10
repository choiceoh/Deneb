@file:OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)

package ai.deneb.deneb

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.Image
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ElevatedCard
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
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.decodeToImageBitmap
import ai.deneb.getBackgroundDispatcher
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.MarkdownContent
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Image attachments above this size stay download-only chips (no inline decode). */
private const val MAIL_IMAGE_PREVIEW_MAX_BYTES = 8 * 1024 * 1024

/**
 * Full Gmail message + reading surface. On open it marks read and fetches any
 * cached analysis instantly (no LLM). Actions are deliberately minimal: trash
 * and AI analysis (archive and the sender-context card were dropped — sender
 * context lives in the people screen). The AI analysis card starts collapsed —
 * header plus a one-line teaser — so a long analysis doesn't push the mail
 * body below the fold; tapping the header (or explicitly running/rerunning)
 * expands it to markdown + related-project chips (-> wiki). The ask box keeps
 * multi-turn history; attachments are tappable chips, and image attachments
 * also render an inline preview. Trash pops back.
 */
@Composable
fun DenebMailDetailScreen(
    client: DenebGatewayClient,
    messageId: String,
    onBack: () -> Unit,
    onOpenWiki: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
    // panelMode = rendered as the right detail pane of the desktop split-view: drop the
    // status-bar inset and the "← 뒤로" header (the user switches mail by clicking list rows;
    // onBack is still invoked by archive/trash success to clear the selection).
    panelMode: Boolean = false,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    val uriHandler = LocalUriHandler.current
    var detail by remember(messageId) { mutableStateOf<MailDetail?>(null) }
    var analysis by remember(messageId) { mutableStateOf<MailAnalysis?>(null) }
    var analyzing by remember(messageId) { mutableStateOf(false) }
    var analysisFailed by remember(messageId) { mutableStateOf(false) }
    var analysisExpanded by remember(messageId) { mutableStateOf(false) }
    var askText by remember(messageId) { mutableStateOf("") }
    var asking by remember(messageId) { mutableStateOf(false) }
    val qa = remember(messageId) { mutableStateListOf<Pair<String, String>>() }
    var loadFailed by remember(messageId) { mutableStateOf(false) }
    var actionMsg by remember(messageId) { mutableStateOf<String?>(null) }

    suspend fun load() {
        loadFailed = false
        detail = null
        coroutineScope {
            // Cached analysis only needs the messageId, not the detail body — fetch
            // it concurrently with the detail instead of after it. markMailRead stays
            // sequential (cheap, and its optimistic unread-clear already ran).
            val analysisDeferred = async { client.fetchCachedAnalysis(messageId) }
            val d = client.fetchMailDetail(messageId)
            detail = d
            loadFailed = d == null
            if (d != null) {
                client.markMailRead(messageId)
                analysis = analysisDeferred.await()
            } else {
                analysisDeferred.cancel()
            }
        }
    }
    LaunchedEffect(messageId) { load() }

    fun runAnalysis(force: Boolean) {
        scope.launch {
            analyzing = true
            analysisFailed = false
            // An explicit run is a request to *see* the result — pop the card open.
            analysisExpanded = true
            val a = client.analyzeMail(messageId, force)
            if (a != null) analysis = a else analysisFailed = true
            analyzing = false
        }
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .then(if (panelMode) Modifier else Modifier.statusBarsPadding())
                // Keep the "ask about this mail" field above the soft keyboard instead
                // of hiding it behind the keyboard (edge-to-edge: the app owns the IME
                // inset). Before verticalScroll so it shrinks the scroll viewport.
                .imePadding()
                .padding(16.dp).verticalScroll(rememberScrollState()),
        ) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                Spacer(Modifier.height(12.dp))
            }
            if (!panelMode) {
                TextButton(onClick = onBack) { Text("← 뒤로") }
                Spacer(Modifier.height(4.dp))
            }

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
                style = DenebType.subject,
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
                        haptics.reject()
                        scope.launch { if (client.trashMail(mail.id)) onBack() else actionMsg = "휴지통 이동 실패" }
                    },
                    modifier = Modifier.weight(1f),
                ) { Text("휴지통") }
                FilledTonalButton(
                    onClick = { haptics.tap(); runAnalysis(force = false) },
                    enabled = !analyzing,
                    modifier = Modifier.weight(1f),
                ) { Text(if (analyzing) "분석 중…" else "AI 분석") }
            }
            actionMsg?.let {
                Spacer(Modifier.height(6.dp))
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }

            if (analyzing || analysis != null || analysisFailed) {
                Spacer(Modifier.height(12.dp))
                // First substantive line of the analysis doubles as the collapsed
                // card's teaser (markdown decoration stripped for plain display).
                val analysisPreview = remember(analysis) {
                    analysis?.text?.lineSequence()
                        ?.map { it.trim().trimStart('#', '*', '-', '>', ' ').replace("*", "").replace("`", "").trim() }
                        ?.firstOrNull { it.isNotEmpty() }
                        .orEmpty()
                }
                ElevatedCard(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth()
                                .clickable { haptics.toggle(!analysisExpanded); analysisExpanded = !analysisExpanded }
                                .handCursor(),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Icon(
                                imageVector = if (analysisExpanded) Icons.Default.KeyboardArrowUp else Icons.Default.KeyboardArrowDown,
                                contentDescription = if (analysisExpanded) "AI 분석 접기" else "AI 분석 펼치기",
                                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.size(16.dp),
                            )
                            Spacer(Modifier.width(6.dp))
                            Text(
                                "AI 분석",
                                style = MaterialTheme.typography.titleSmall,
                                color = MaterialTheme.colorScheme.primary,
                            )
                            if (!analysisExpanded && analysisPreview.isNotEmpty()) {
                                Text(
                                    " · $analysisPreview",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f).padding(start = 4.dp),
                                )
                            } else {
                                Spacer(Modifier.weight(1f))
                            }
                            analysis?.let { a ->
                                Text(
                                    if (a.cached) "저장됨 · ${a.createdAt.take(10)}" else "${a.durationMs / 1000}s",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                // Rerun lives behind the fold: hiding it while collapsed keeps
                                // the whole header a safe expand target (no accidental reruns).
                                if (analysisExpanded) {
                                    Spacer(Modifier.width(8.dp))
                                    TextButton(onClick = { haptics.tap(); runAnalysis(force = true) }, enabled = !analyzing) { Text("다시") }
                                }
                            }
                        }
                        AnimatedVisibility(visible = analysisExpanded, enter = denebExpandIn, exit = denebShrinkOut) {
                            Column {
                                Spacer(Modifier.height(6.dp))
                                when {
                                    analyzing -> Row(verticalAlignment = Alignment.CenterVertically) {
                                        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                                        Spacer(Modifier.width(8.dp))
                                        Text("분석 중…", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    }
                                    analysis != null -> Column {
                                        MarkdownContent(analysis!!.text)
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

                // Inline previews for image attachments (receipts, photos, stamped
                // documents) so they're readable without leaving the app. Bounded to
                // 8MB per image; a fetch/decode failure just leaves the chip above as
                // the only affordance. Tap opens the same download URL as the chip.
                val imageAtts = mail.attachments.filter {
                    it.mimeType.startsWith("image/") && it.size in 1..MAIL_IMAGE_PREVIEW_MAX_BYTES
                }
                if (imageAtts.isNotEmpty()) {
                    val previews = remember(messageId) { mutableStateMapOf<String, ImageBitmap?>() }
                    LaunchedEffect(mail.id) {
                        // Sequential on purpose (project style): a mail rarely has more
                        // than a few images, and this keeps peak memory at one decode.
                        for (att in imageAtts) {
                            if (previews.containsKey(att.id)) continue
                            val bytes = client.fetchAttachmentBytes(mail.id, att)
                            previews[att.id] = bytes?.let {
                                withContext(getBackgroundDispatcher()) { decodeToImageBitmap(it) }
                            }
                        }
                    }
                    imageAtts.forEach { att ->
                        when {
                            !previews.containsKey(att.id) -> {
                                Spacer(Modifier.height(8.dp))
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                                    Spacer(Modifier.width(8.dp))
                                    Text(att.filename, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                            }
                            else -> previews[att.id]?.let { bmp ->
                                Spacer(Modifier.height(8.dp))
                                Image(
                                    bitmap = bmp,
                                    contentDescription = att.filename,
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .heightIn(max = 360.dp)
                                        .clip(RoundedCornerShape(8.dp))
                                        .clickable { uriHandler.openUri(client.attachmentUrl(mail.id, att)) },
                                    contentScale = ContentScale.Fit,
                                )
                            }
                        }
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
                    MarkdownContent(answer)
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
