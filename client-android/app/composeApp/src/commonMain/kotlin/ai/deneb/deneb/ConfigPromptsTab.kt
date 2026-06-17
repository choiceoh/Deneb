package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.deneb.generated.PromptDetailOut
import ai.deneb.deneb.generated.PromptRow
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
import androidx.compose.material.icons.outlined.Restore
import androidx.compose.material.icons.outlined.Save
import androidx.compose.material.icons.outlined.TextSnippet
import androidx.compose.material3.Button
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

// Settings hub "프롬프트 코너" tab: native-editable prompt templates that tools and
// automations read at runtime. The first editable prompt is automatic mail
// analysis; the RPC shape is generic so future tool prompts can join the list
// without another native settings surface.
@Composable
internal fun PromptsTab(client: DenebGatewayClient) {
    var prompts by remember { mutableStateOf<List<PromptRow>?>(null) }
    var listLoading by remember { mutableStateOf(true) }
    var listFailed by remember { mutableStateOf(false) }
    var reloadListSeq by remember { mutableIntStateOf(0) }
    var selectedId by rememberSaveable { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun loadList() {
        listLoading = true
        listFailed = false
        val fetched = client.fetchPrompts()
        prompts = fetched
        listFailed = fetched == null
        listLoading = false
    }

    LaunchedEffect(reloadListSeq) { loadList() }

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

        prompts.orEmpty().isEmpty() -> EmptyTab("편집할 프롬프트가 없습니다.")

        else -> PromptList(prompts.orEmpty(), onOpen = { selectedId = it })
    }
}

@Composable
private fun PromptList(prompts: List<PromptRow>, onOpen: (String) -> Unit) {
    val haptics = rememberHaptics()
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = 12.dp, bottom = 24.dp),
    ) {
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

        current != null -> PromptEditor(
            detail = current,
            draft = draft,
            onDraft = {
                draft = it
                error = null
                notice = null
            },
            saving = saving,
            error = error,
            notice = notice,
            onBack = requestBack,
            onSave = { save() },
            onReset = { reset() },
        )
    }
}

@Composable
private fun PromptEditor(
    detail: PromptDetailOut,
    draft: String,
    onDraft: (String) -> Unit,
    saving: Boolean,
    error: String?,
    notice: String?,
    onBack: () -> Unit,
    onSave: () -> Unit,
    onReset: () -> Unit,
) {
    val changed = draft != detail.text
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
                    detail.title.ifBlank { detail.id },
                    style = DenebType.rowTitleStrong,
                    color = MaterialTheme.colorScheme.onBackground,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    promptDetailMeta(detail),
                    style = DenebType.rowSubtitle,
                    color = denebHint(),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        if (detail.description.isNotBlank()) {
            Text(
                detail.description,
                style = DenebType.body,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        OutlinedTextField(
            value = draft,
            onValueChange = onDraft,
            readOnly = !detail.editable || saving,
            label = { Text("프롬프트") },
            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
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
            OutlinedButton(
                onClick = onReset,
                enabled = detail.editable && detail.overridden && !saving,
            ) {
                Icon(Icons.Outlined.Restore, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("복구")
            }
            Button(
                onClick = onSave,
                enabled = detail.editable && changed && draft.isNotBlank() && !saving,
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
