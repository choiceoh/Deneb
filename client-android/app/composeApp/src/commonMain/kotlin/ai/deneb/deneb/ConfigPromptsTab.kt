package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.deneb.generated.PromptDetailOut
import ai.deneb.deneb.generated.PromptRow
import ai.deneb.deneb.generated.TopicDocOut
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Article
import androidx.compose.material.icons.outlined.Restore
import androidx.compose.material.icons.outlined.Save
import androidx.compose.material.icons.outlined.TextSnippet
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

// Topic-background byte cap, mirrored from the gateway's
// prompt.MaxTopicKnowledgeChars (handlerminiapp/topicdocs.go pins the write cap to
// it). The gateway rejects an over-cap write by byte length, so the editor warns as
// the UTF-8 byte count nears it. Kept here as a local copy because the native client
// has no generated mirror of gateway constants.
private const val TOPIC_DOC_MAX_BYTES = 24_000

// Settings hub "프롬프트 코너" tab: native-editable prompt templates that tools and
// automations read at runtime, plus the per-topic background knowledge doc.
//
// Two kinds of editable text share this one corner but have different storage, so
// they are loaded/saved through different RPCs while reusing the SAME editor UI
// ([PromptStyleEditor]):
//   - Prompts (mail analysis, future tool prompts) are override-JSON backed →
//     miniapp.prompts.* (see DenebClientPrompts.kt).
//   - The topic background is file backed (workspace/topics/<key>.md, injected into
//     the system prompt's Static block) → miniapp.topicdocs.* (DenebClientTopicDoc.kt).
//     It is deliberately NOT a prompts-store entry: routing its edits through the
//     prompts store would never write the .md, so injection would silently break.
//     Hence the UI integration here, with a dedicated save path.
@Composable
internal fun PromptsTab(client: DenebGatewayClient) {
    var prompts by remember { mutableStateOf<List<PromptRow>?>(null) }
    var topicDoc by remember { mutableStateOf<TopicDocOut?>(null) }
    var listLoading by remember { mutableStateOf(true) }
    var listFailed by remember { mutableStateOf(false) }
    var reloadListSeq by remember { mutableIntStateOf(0) }
    // The open editor: a prompt id, the topic sentinel, or null (the list). Topic is
    // tracked separately from selectedId so it can never collide with a prompt id.
    var selectedId by rememberSaveable { mutableStateOf<String?>(null) }
    var topicOpen by rememberSaveable { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun loadList() {
        listLoading = true
        listFailed = false
        val fetched = client.fetchPrompts()
        prompts = fetched
        // The topic doc is its own RPC; a failure there only drops the topic row, it
        // must not fail the whole prompts list. List failure is prompts-only.
        topicDoc = client.fetchTopicDoc()
        listFailed = fetched == null
        listLoading = false
    }

    LaunchedEffect(reloadListSeq) { loadList() }

    if (topicOpen) {
        TopicDocDetailPane(
            client = client,
            initial = topicDoc,
            onBack = { topicOpen = false },
            onChanged = { reloadListSeq += 1 },
        )
        return
    }

    val id = selectedId
    if (id != null) {
        PromptDetailPane(
            client = client,
            id = id,
            onBack = { selectedId = null },
            onChanged = {
                reloadListSeq += 1
            },
        )
        return
    }

    when {
        listLoading && prompts == null -> DenebLoading()

        listFailed && prompts == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError("프롬프트를 불러오지 못했습니다.", onRetry = { scope.launch { loadList() } })
        }

        prompts.orEmpty().isEmpty() && topicDoc == null -> EmptyTab("편집할 프롬프트가 없습니다.")

        else -> PromptList(
            prompts = prompts.orEmpty(),
            topicDoc = topicDoc,
            onOpen = { selectedId = it },
            onOpenTopic = { topicOpen = true },
        )
    }
}

@Composable
private fun PromptList(
    prompts: List<PromptRow>,
    topicDoc: TopicDocOut?,
    onOpen: (String) -> Unit,
    onOpenTopic: () -> Unit,
) {
    val haptics = rememberHaptics()
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = 12.dp, bottom = 24.dp),
    ) {
        // Topic background first: a single file-backed doc, its own group so it reads
        // as distinct from the tool prompts below (different storage, same editor).
        if (topicDoc != null) {
            DenebGroup(label = "토픽 배경") {
                DenebListRow(
                    title = topicDoc.name.ifBlank { topicDoc.key.ifBlank { "토픽 배경" } },
                    subtitle = topicDocRowSubtitle(topicDoc),
                    icon = Icons.Outlined.Article,
                    onClick = {
                        haptics.tap()
                        onOpenTopic()
                    },
                    divider = false,
                )
            }
        }
        val grouped = prompts.groupBy { it.category.ifBlank { "프롬프트" } }
        grouped.forEach { (category, rows) ->
            DenebGroup(label = category) {
                rows.forEachIndexed { i, row ->
                    DenebListRow(
                        title = row.title.ifBlank { row.id },
                        subtitle = promptRowSubtitle(row),
                        icon = Icons.Outlined.TextSnippet,
                        onClick = {
                            haptics.tap()
                            onOpen(row.id)
                        },
                        divider = i < rows.lastIndex,
                    )
                }
            }
        }
    }
}

@Composable
private fun PromptDetailPane(
    client: DenebGatewayClient,
    id: String,
    onBack: () -> Unit,
    onChanged: () -> Unit,
) {
    var detail by remember { mutableStateOf<PromptDetailOut?>(null) }
    var draft by rememberSaveable(id) { mutableStateOf("") }
    var loading by remember { mutableStateOf(true) }
    var failed by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        loading = true
        failed = false
        error = null
        notice = null
        val fetched = client.fetchPrompt(id)
        detail = fetched
        draft = fetched?.text.orEmpty()
        failed = fetched == null
        loading = false
    }

    fun save() {
        val current = detail ?: return
        val text = draft.trim()
        if (text.isBlank()) {
            error = "프롬프트 내용을 입력해 주세요."
            return
        }
        scope.launch {
            saving = true
            error = null
            notice = null
            val saved = client.updatePrompt(current.id, text)
            saving = false
            if (saved == null) {
                error = "저장하지 못했습니다."
            } else {
                detail = saved
                draft = saved.text
                notice = "저장됨"
                onChanged()
            }
        }
    }

    fun reset() {
        val current = detail ?: return
        scope.launch {
            saving = true
            error = null
            notice = null
            val saved = client.resetPrompt(current.id)
            saving = false
            if (saved == null) {
                error = "기본값으로 되돌리지 못했습니다."
            } else {
                detail = saved
                draft = saved.text
                notice = "기본값으로 복구됨"
                onChanged()
            }
        }
    }

    LaunchedEffect(id) { load() }

    val current = detail
    val dirty = current != null && draft != current.text
    val requestBack = rememberDiscardGuard(dirty, onBack)
    PlatformBackHandler(enabled = true) { requestBack() }

    when {
        loading && current == null -> DenebLoading()

        failed && current == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError("프롬프트를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
        }

        current != null -> PromptStyleEditor(
            title = current.title.ifBlank { current.id },
            meta = promptDetailMeta(current),
            description = current.description,
            draft = draft,
            onDraft = {
                draft = it
                error = null
                notice = null
            },
            readOnly = !current.editable,
            saving = saving,
            error = error,
            notice = notice,
            onBack = requestBack,
            // Prompts: enable save only on an editable, changed, non-blank draft.
            canSave = current.editable && draft != current.text && draft.isNotBlank(),
            onSave = { save() },
            // Prompts get a "reset to default" action; the topic doc has no default.
            trailingActions = {
                OutlinedButton(
                    onClick = { reset() },
                    enabled = current.editable && current.overridden && !saving,
                ) {
                    Icon(Icons.Outlined.Restore, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("복구")
                }
            },
        )
    }
}

// Topic-background editor: same shell + editor as the prompts above ([PromptStyleEditor]),
// but loads/saves through miniapp.topicdocs.* and adds a byte-cap warning + an optional
// "apply this session now" toggle. [initial] seeds the body from the list fetch so the
// editor opens instantly; it re-reads on open to pick up any out-of-band edit.
@Composable
private fun TopicDocDetailPane(
    client: DenebGatewayClient,
    initial: TopicDocOut?,
    onBack: () -> Unit,
    onChanged: () -> Unit,
) {
    var doc by remember { mutableStateOf(initial) }
    var saved by rememberSaveable { mutableStateOf(initial?.content.orEmpty()) }
    var draft by rememberSaveable { mutableStateOf(initial?.content.orEmpty()) }
    var loading by remember { mutableStateOf(initial == null) }
    var failed by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    // Optional immediate-apply (RPC analog of slash --now). Off by default to keep the
    // Static prompt cache stable; on, the gateway drops this session's frozen topic
    // snapshot so the edit lands this turn.
    var applyNow by rememberSaveable { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        loading = true
        failed = false
        error = null
        notice = null
        val fetched = client.fetchTopicDoc()
        if (fetched != null) {
            doc = fetched
            saved = fetched.content
            draft = fetched.content
        } else if (doc == null) {
            // Only a hard failure (no seed and the re-read failed) blocks the editor.
            failed = true
        }
        loading = false
    }

    fun save() {
        val text = draft // preserve exact text (incl. leading/trailing blank lines)
        if (text.isBlank()) {
            error = "토픽 배경 내용을 입력해 주세요."
            return
        }
        if (text.encodeToByteArray().size > TOPIC_DOC_MAX_BYTES) {
            error = "주입 한도(24KB)를 초과했습니다. 내용을 줄여 주세요."
            return
        }
        scope.launch {
            saving = true
            error = null
            notice = null
            val result = client.saveTopicDoc(text, applyNow = applyNow)
            saving = false
            if (result == null) {
                error = "저장하지 못했습니다."
            } else {
                saved = text
                doc = doc?.copy(content = text, size = result.size, modified = result.modified)
                notice = if (result.applied) "저장됨 · 이번 세션에 적용됨" else "저장됨 · 다음 세션부터 반영"
                onChanged()
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    val dirty = draft != saved
    val requestBack = rememberDiscardGuard(dirty, onBack)
    PlatformBackHandler(enabled = true) { requestBack() }

    val current = doc
    when {
        loading && current == null -> DenebLoading()

        failed && current == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError("토픽 배경을 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
        }

        else -> PromptStyleEditor(
            title = current?.name?.ifBlank { current.key }.orEmpty().ifBlank { "토픽 배경" },
            meta = topicDocMeta(current, draft),
            description = "시스템 프롬프트에 주입되는 이 토픽의 배경 지식입니다. 저장하면 다음 세션부터 반영됩니다.",
            draft = draft,
            onDraft = {
                draft = it
                error = null
                notice = null
            },
            readOnly = false,
            saving = saving,
            error = error,
            notice = notice,
            onBack = requestBack,
            // Topic: enable save on a changed, non-blank, within-cap draft.
            canSave = draft != saved && draft.isNotBlank() && draft.encodeToByteArray().size <= TOPIC_DOC_MAX_BYTES,
            onSave = { save() },
            trailingActions = {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(
                        checked = applyNow,
                        onCheckedChange = { applyNow = it },
                        enabled = !saving,
                    )
                    Text(
                        "즉시 적용",
                        style = DenebType.meta,
                        color = denebHint(),
                    )
                }
            },
        )
    }
}

// Shared stateless editor body for both the prompts and the topic background: a header
// row (back + title + meta), an optional description, the monospace text field, and a
// footer (status/char line + caller-supplied trailing actions + save). Splitting the
// stateful detail panes from this pure body keeps their distinct load/save wiring out
// of the visuals, and lets RenderPreview exercise the look with mock data.
@Composable
internal fun PromptStyleEditor(
    title: String,
    meta: String,
    description: String,
    draft: String,
    onDraft: (String) -> Unit,
    readOnly: Boolean,
    saving: Boolean,
    error: String?,
    notice: String?,
    onBack: () -> Unit,
    canSave: Boolean,
    onSave: () -> Unit,
    trailingActions: @Composable () -> Unit = {},
) {
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onBack, modifier = Modifier.size(40.dp)) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "목록")
            }
            Column(Modifier.weight(1f)) {
                Text(
                    title,
                    style = DenebType.rowTitleStrong,
                    color = MaterialTheme.colorScheme.onBackground,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    meta,
                    style = DenebType.rowSubtitle,
                    color = denebHint(),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        if (description.isNotBlank()) {
            Text(
                description,
                style = DenebType.body,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        OutlinedTextField(
            value = draft,
            onValueChange = onDraft,
            readOnly = readOnly || saving,
            label = { Text("내용") },
            textStyle = DenebType.snippet.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 360.dp),
        )

        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                error ?: notice ?: "${draft.length}자",
                style = DenebType.meta,
                color = when {
                    error != null -> MaterialTheme.colorScheme.error
                    notice != null -> MaterialTheme.colorScheme.primary
                    else -> denebHint()
                },
                modifier = Modifier.weight(1f),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            trailingActions()
            Button(
                onClick = onSave,
                enabled = canSave && !saving,
            ) {
                Icon(Icons.Outlined.Save, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text(if (saving) "저장 중" else "저장")
            }
        }
    }
}

private fun promptRowSubtitle(row: PromptRow): String {
    val status = if (row.overridden) "수정됨" else "기본값"
    return listOf(row.description, status).filter { it.isNotBlank() }.joinToString(" · ")
}

private fun promptDetailMeta(detail: PromptDetailOut): String {
    val status = if (detail.overridden) "수정됨" else "기본값"
    return listOf(detail.category, status, detail.id).filter { it.isNotBlank() }.joinToString(" · ")
}

private fun topicDocRowSubtitle(doc: TopicDocOut): String {
    val state = if (doc.content.isBlank()) "비어 있음" else "${doc.content.length}자"
    return listOf("시스템 프롬프트 주입", state).joinToString(" · ")
}

// Editor meta line for the topic doc: warns as the live byte count nears the gateway
// injection cap (the save itself is blocked over-cap), otherwise a plain byte readout.
private fun topicDocMeta(doc: TopicDocOut?, draft: String): String {
    val bytes = draft.encodeToByteArray().size
    val cap = when {
        bytes > TOPIC_DOC_MAX_BYTES -> "한도 초과 ($bytes/${TOPIC_DOC_MAX_BYTES}B)"
        bytes > TOPIC_DOC_MAX_BYTES * 9 / 10 -> "한도 임박 ($bytes/${TOPIC_DOC_MAX_BYTES}B)"
        else -> "$bytes/${TOPIC_DOC_MAX_BYTES}B"
    }
    return listOf(doc?.key.orEmpty(), cap).filter { it.isNotBlank() }.joinToString(" · ")
}
