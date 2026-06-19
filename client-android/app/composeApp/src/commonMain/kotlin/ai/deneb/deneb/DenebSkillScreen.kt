package ai.deneb.deneb

import ai.deneb.deneb.generated.SkillDetailResponse
import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Instant

/**
 * Skill detail (`miniapp.skills.detail`): the full catalog meta the list row
 * truncates, the SKILL.md document itself, and the skill's own slice of the
 * Propus timeline (`miniapp.skills.lifecycle` filtered by skillName).
 * Mutable local skills can be edited/deleted here; protected bundled/plugin
 * skills stay view-only and the gateway enforces the same guard.
 */
@Composable
fun DenebSkillScreen(
    client: DenebGatewayClient,
    skillName: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var detail by remember(skillName) { mutableStateOf<SkillDetailResponse?>(null) }
    var events by remember(skillName) { mutableStateOf<List<SkillLifecycleEvent>?>(null) }
    var loadFailed by remember(skillName) { mutableStateOf(false) }
    var editMode by remember(skillName) { mutableStateOf(false) }
    var draftBody by remember(skillName) { mutableStateOf("") }
    var actionBusy by remember(skillName) { mutableStateOf(false) }
    var actionMessage by remember(skillName) { mutableStateOf("") }
    var actionIsError by remember(skillName) { mutableStateOf(false) }
    var confirmDelete by remember(skillName) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    fun setActionMessage(message: String, error: Boolean = false) {
        actionMessage = message
        actionIsError = error
    }

    suspend fun reload() {
        loadFailed = false
        detail = null
        val d = client.fetchSkillDetail(skillName)
        detail = d
        if (!editMode) draftBody = d?.body.orEmpty()
        loadFailed = d == null
        // Timeline is enrichment: a transport failure here degrades to an
        // empty section instead of failing the whole screen.
        events = if (d != null) client.fetchSkillLifecycle(limit = 30, skillName = skillName) else null
    }
    LaunchedEffect(skillName) { reload() }

    DenebScreenScaffold(title = "스킬", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(8.dp))
            val d = detail
            if (d == null) {
                if (loadFailed) {
                    DenebError("스킬을 불러오지 못했습니다.", onRetry = { scope.launch { reload() } })
                } else {
                    DenebLoading()
                }
            } else {
                SkillDetailContent(
                    detail = d,
                    events = events.orEmpty(),
                    editMode = editMode,
                    draftBody = draftBody,
                    actionBusy = actionBusy,
                    actionMessage = actionMessage,
                    actionIsError = actionIsError,
                    onDraftChange = {
                        draftBody = it
                        if (actionMessage.isNotBlank()) actionMessage = ""
                    },
                    onStartEdit = {
                        haptics.tap()
                        if (d.bodyTruncated) {
                            setActionMessage("문서가 길어 일부만 표시되어 수정할 수 없습니다.", error = true)
                        } else if (d.body.isBlank()) {
                            setActionMessage("SKILL.md 본문을 읽을 수 없어 수정할 수 없습니다.", error = true)
                        } else {
                            draftBody = d.body
                            editMode = true
                            setActionMessage("")
                        }
                    },
                    onCancelEdit = {
                        haptics.tap()
                        draftBody = d.body
                        editMode = false
                        setActionMessage("")
                    },
                    onSave = {
                        haptics.tap()
                        scope.launch {
                            actionBusy = true
                            val err = client.updateSkill(skillName, draftBody)
                            if (err == null) {
                                val refreshed = client.fetchSkillDetail(skillName)
                                if (refreshed != null) {
                                    detail = refreshed
                                    draftBody = refreshed.body
                                    editMode = false
                                    events = client.fetchSkillLifecycle(limit = 30, skillName = skillName)
                                    setActionMessage("저장됨")
                                } else {
                                    setActionMessage("저장됐지만 상세를 다시 불러오지 못했습니다.", error = true)
                                }
                            } else {
                                setActionMessage(err, error = true)
                            }
                            actionBusy = false
                        }
                    },
                    onRequestDelete = {
                        haptics.reject()
                        confirmDelete = true
                    },
                )
            }
            Spacer(Modifier.height(24.dp))
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { if (!actionBusy) confirmDelete = false },
            title = { Text("스킬 삭제") },
            text = { Text("$skillName 스킬 디렉터리를 삭제합니다. 되돌릴 수 없습니다.") },
            confirmButton = {
                TextButton(
                    enabled = !actionBusy,
                    onClick = {
                        haptics.reject()
                        scope.launch {
                            actionBusy = true
                            val err = client.deleteSkill(skillName)
                            if (err == null) {
                                confirmDelete = false
                                onBack()
                            } else {
                                setActionMessage(err, error = true)
                                confirmDelete = false
                                actionBusy = false
                            }
                        }
                    },
                ) { Text("삭제", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(enabled = !actionBusy, onClick = { confirmDelete = false }) { Text("취소") }
            },
        )
    }
}

// Stateless detail body — previewable without a gateway client.
@Composable
internal fun SkillDetailContent(
    detail: SkillDetailResponse,
    events: List<SkillLifecycleEvent>,
    editMode: Boolean = false,
    draftBody: String = detail.body,
    actionBusy: Boolean = false,
    actionMessage: String = "",
    actionIsError: Boolean = false,
    onDraftChange: (String) -> Unit = {},
    onStartEdit: () -> Unit = {},
    onCancelEdit: () -> Unit = {},
    onSave: () -> Unit = {},
    onRequestDelete: () -> Unit = {},
) {
    val skill = detail.skill

    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            skill.name,
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f, fill = false),
        )
        Spacer(Modifier.width(8.dp))
        SkillOriginBadge(skill.origin)
    }

    if (skill.description.isNotBlank()) {
        Spacer(Modifier.height(8.dp))
        Text(
            skill.description,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }

    Spacer(Modifier.height(8.dp))
    skillFactLines(skill = skill).forEach { line ->
        Text(
            line,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }

    val canEdit = skill.editable && detail.body.isNotBlank() && !detail.bodyTruncated
    if (skill.editable || skill.deletable) {
        Spacer(Modifier.height(10.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (editMode) {
                Button(enabled = !actionBusy && draftBody.isNotBlank(), onClick = onSave) {
                    Text(if (actionBusy) "저장 중…" else "저장")
                }
                OutlinedButton(enabled = !actionBusy, onClick = onCancelEdit) { Text("취소") }
            } else {
                if (skill.editable) {
                    OutlinedButton(enabled = !actionBusy && canEdit, onClick = onStartEdit) { Text("수정") }
                }
                if (skill.deletable) {
                    OutlinedButton(enabled = !actionBusy, onClick = onRequestDelete) {
                        Text("삭제", color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
    }
    if (actionMessage.isNotBlank()) {
        Spacer(Modifier.height(8.dp))
        Text(
            actionMessage,
            style = MaterialTheme.typography.bodySmall,
            color = if (actionIsError) MaterialTheme.colorScheme.error else denebHint(),
        )
    }
    if (skill.editable && !canEdit && !editMode) {
        Spacer(Modifier.height(6.dp))
        Text(
            if (detail.bodyTruncated) {
                "문서가 길어 일부만 표시되어 수정할 수 없습니다."
            } else {
                "SKILL.md 본문을 읽을 수 없어 수정할 수 없습니다."
            },
            style = MaterialTheme.typography.bodySmall,
            color = denebHint(),
        )
    }

    Spacer(Modifier.height(16.dp))
    HorizontalDivider(color = denebHairline())
    Spacer(Modifier.height(12.dp))
    Text("문서", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
    Spacer(Modifier.height(8.dp))
    if (editMode) {
        OutlinedTextField(
            value = draftBody,
            onValueChange = onDraftChange,
            enabled = !actionBusy,
            label = { Text("SKILL.md") },
            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth().heightIn(min = 360.dp),
        )
    } else {
        val body = stripFrontmatter(detail.body)
        if (body.isNotBlank()) {
            MarkdownContent(body, baseStyle = MaterialTheme.typography.bodyMedium)
            if (detail.bodyTruncated) {
                Spacer(Modifier.height(4.dp))
                Text("(문서가 길어 일부만 표시합니다)", style = MaterialTheme.typography.bodySmall, color = denebHint())
            }
        } else {
            Text("SKILL.md 본문을 읽을 수 없습니다.", style = MaterialTheme.typography.bodySmall, color = denebHint())
        }
    }

    Spacer(Modifier.height(16.dp))
    HorizontalDivider(color = denebHairline())
    Spacer(Modifier.height(12.dp))
    Text("Propus 로그", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
    if (events.isEmpty()) {
        Spacer(Modifier.height(8.dp))
        Text("이 스킬의 Propus 활동이 아직 없습니다.", style = MaterialTheme.typography.bodySmall, color = denebHint())
    } else {
        events.forEachIndexed { idx, event ->
            SkillLifecycleRow(event, showSkillName = false, horizontalPadding = 0.dp)
            if (idx < events.lastIndex) HorizontalDivider(color = denebHairline())
        }
    }

    if (detail.path.isNotBlank()) {
        Spacer(Modifier.height(16.dp))
        Text(detail.path, style = MaterialTheme.typography.labelSmall, color = denebHint())
    }
}

/** The meta the list row has no room for, one fact per line, blanks omitted. */
private fun skillFactLines(skill: ai.deneb.deneb.generated.SkillRow): List<String> {
    val identity = listOfNotNull(
        skill.category.takeIf { it.isNotBlank() },
        skillSourceLabel(skill.source).takeIf { it.isNotBlank() },
        skill.version.takeIf { it.isNotBlank() }?.let { "v$it" },
    ).joinToString(" · ")
    val homepage = skill.homepage.takeIf { it.isNotBlank() }?.let { "홈페이지 $it" }.orEmpty()
    val tags = skill.tags.takeIf { it.isNotEmpty() }?.joinToString(" · ")?.let { "태그 $it" }.orEmpty()
    val related = skill.relatedSkills.takeIf { it.isNotEmpty() }?.joinToString(" · ")?.let { "관련 스킬 $it" }.orEmpty()
    val dependencies = skill.dependencySummary.takeIf { it.isNotEmpty() }
        ?.joinToString(" · ")
        ?.let { "요구조건 $it" }
        .orEmpty()
    val installs = skill.installSummary.takeIf { it.isNotEmpty() }
        ?.joinToString(" · ")
        ?.let { "설치 힌트 $it" }
        .orEmpty()
    val mutability = when {
        skill.editable && skill.deletable -> "앱에서 수정/삭제 가능"
        skill.editable -> "앱에서 수정 가능"
        skill.deletable -> "앱에서 삭제 가능"
        else -> ""
    }
    val genesis = listOfNotNull(
        skill.createdAt.takeIf { it > 0 }?.let { "생성일 ${epochDate(it)}" },
        curatorStateLabel(skill.curatorState),
    ).joinToString(" · ")
    val usage = listOfNotNull(
        skill.totalUses.takeIf { it > 0 }?.let { "사용 ${it}회" },
        skill.lastUsedAt.takeIf { it > 0 }?.let { "마지막 사용 ${lifecycleTime(it)}" },
    ).joinToString(" · ")
    val evolve = listOfNotNull(
        skill.evolveCount.takeIf { it > 0 }?.let { "진화 ${it}회" },
        skill.lastEvolvedAt.takeIf { it > 0 }?.let { "마지막 진화 ${lifecycleTime(it)}" },
    ).joinToString(" · ")
    return listOf(identity, homepage, tags, related, dependencies, installs, mutability, genesis, usage, evolve).filter { it.isNotBlank() }
}

/** Curator state for agent-created skills ("상태 활성" 등); null for initial
 *  skills (no curator record) or states we don't recognize. */
private fun curatorStateLabel(state: String): String? = when (state) {
    "active" -> "상태 활성"
    "stale" -> "상태 정체"
    "archived" -> "상태 보관됨"
    else -> null
}

private fun epochDate(ms: Long): String = Instant.fromEpochMilliseconds(ms).toLocalDateTime(TimeZone.currentSystemDefault()).date.toString()

/** Drops the leading YAML frontmatter fence from a SKILL.md body — the name /
 *  description / version it carries are already rendered as the header above,
 *  so showing the raw `---` block would only duplicate them as noise. */
internal fun stripFrontmatter(body: String): String {
    val text = body.trimStart('\uFEFF')
    if (!text.startsWith("---")) return body
    val firstLineEnd = text.indexOf('\n')
    if (firstLineEnd < 0 || text.substring(0, firstLineEnd).trim() != "---") return body
    var idx = firstLineEnd + 1
    while (idx <= text.length) {
        val lineEnd = text.indexOf('\n', idx).let { if (it < 0) text.length else it }
        if (text.substring(idx, lineEnd).trim() == "---") {
            return text.substring(minOf(lineEnd + 1, text.length)).trimStart('\n')
        }
        idx = lineEnd + 1
    }
    return body // unterminated fence — show as-is rather than eat the document
}
