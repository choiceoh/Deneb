package ai.deneb.deneb

import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Coding mode (`miniapp.code.*`) — the native rail of git-worktree sessions and
 * their actions, so coding work can be started and *resumed* from the phone. The
 * server persists each session (one worktree/branch) across app and gateway
 * restarts; this screen lists them, and the real editing happens by handing off
 * to the chat bound to `code:<id>` (via [onOpenChat]). That hand-off is what
 * makes the work continuous: leave, come back, reopen, keep going.
 *
 * Three views in one screen: the rail (list), one session's detail + actions,
 * and the start form. Mirrors the [DenebNotebooksScreen] shell pattern.
 */
@Composable
fun DenebCodeModeScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    // Open the chat bound to this session's key (e.g. "code:abc123") and jump to
    // the chat screen, where the turns edit the worktree.
    onOpenChat: (sessionKey: String) -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var sessions by remember { mutableStateOf<List<CodeSession>?>(null) }
    var listFailed by remember { mutableStateOf(false) }
    // The opened session is held as a captured object (not derived from the list)
    // so a background rail reload — which briefly nulls `sessions` — can't blank
    // the open detail.
    var selected by remember { mutableStateOf<CodeSession?>(null) }
    var showStart by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun loadList() {
        listFailed = false
        sessions = null
        val list = client.fetchCodeSessions()
        sessions = list
        listFailed = list == null
    }

    LaunchedEffect(Unit) { loadList() }

    // START FORM ----------------------------------------------------------------
    if (showStart) {
        CodeStartForm(
            client = client,
            onBack = { showStart = false },
            onStarted = { session ->
                showStart = false
                scope.launch { loadList() }
                onOpenChat(session.chatSessionKey.ifBlank { "code:${session.id}" })
            },
            navigationTabBar = navigationTabBar,
        )
        return
    }

    // DETAIL --------------------------------------------------------------------
    val sel = selected
    if (sel != null) {
        CodeSessionDetail(
            client = client,
            session = sel,
            onBack = { selected = null },
            onOpenChat = onOpenChat,
            onChanged = { scope.launch { loadList() } },
            onGone = {
                selected = null
                scope.launch { loadList() }
            },
            navigationTabBar = navigationTabBar,
        )
        return
    }

    // RAIL (LIST) ---------------------------------------------------------------
    DenebScreenScaffold(
        title = "코드모드",
        onBack = onBack,
        tabBar = navigationTabBar,
        actions = {
            TextButton(onClick = { showStart = true }) { Text("새 세션") }
        },
    ) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 8.dp),
        ) {
            val list = sessions
            if (list == null) {
                if (listFailed) {
                    DenebError("코드 세션을 불러오지 못했습니다.", onRetry = { scope.launch { loadList() } })
                } else {
                    DenebLoading()
                }
                return@Column
            }
            if (list.isEmpty()) {
                DenebEmpty(
                    "진행 중인 코드 세션이 없습니다.",
                    actionLabel = "새 세션 시작",
                    onAction = { showStart = true },
                )
                return@Column
            }
            DenebSectionLabel("진행 중 ${list.size}건")
            list.forEach { s ->
                val sub = buildString {
                    append("${s.repo.owner}/${s.repo.name}")
                    append(" · ${codeStatusLabel(s.status)}")
                    if (s.checkpoints.isNotEmpty()) append(" · 체크포인트 ${s.checkpoints.size}")
                }
                DenebRow(onClick = { selected = s }) {
                    Text(s.title.ifBlank { s.id }, style = DenebType.rowTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(sub, style = DenebType.rowSubtitle)
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Detail + actions for one session.
// ---------------------------------------------------------------------------

@Composable
private fun CodeSessionDetail(
    client: DenebGatewayClient,
    session: CodeSession,
    onBack: () -> Unit,
    onOpenChat: (String) -> Unit,
    onChanged: () -> Unit, // a checkpoint/verify/undo updated the session
    onGone: () -> Unit, // the session was closed or discarded
    navigationTabBar: (@Composable () -> Unit)?,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    // Local working copy so action results reflect immediately without a list reload.
    var live by remember(session.id) { mutableStateOf(session) }
    var busy by remember(session.id) { mutableStateOf(false) }
    var actionError by remember(session.id) { mutableStateOf<String?>(null) }
    var verify by remember(session.id) { mutableStateOf<CodeVerify?>(null) }
    var prUrl by remember(session.id) { mutableStateOf<String?>(null) }
    var summary by remember(session.id) { mutableStateOf("") }
    var pendingClose by remember(session.id) { mutableStateOf(false) }
    var pendingDiscard by remember(session.id) { mutableStateOf(false) }

    // Refresh from the server on open: an agent turn in the bound chat may have
    // advanced the session (new checkpoint, flipped status) since the rail loaded.
    // miniapp.code.status is a side-effect-free read, so this is safe.
    LaunchedEffect(session.id) {
        val fresh = client.fetchCodeStatus(session.id)
        if (fresh != null) live = fresh
    }

    DenebScreenScaffold(title = "코드모드", onBack = onBack, tabBar = navigationTabBar) {
        val s = live
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 8.dp),
        ) {
            Text(s.title.ifBlank { s.id }, style = DenebType.subject)
            Text(
                "${s.repo.owner}/${s.repo.name} · ${s.branch}",
                style = DenebType.meta,
                color = denebHint(),
            )
            Text("상태 · ${codeStatusLabel(s.status)}", style = DenebType.meta, color = denebHint())

            // Primary: continue the work in chat.
            Spacer(Modifier.height(16.dp))
            Button(
                onClick = {
                    haptics.confirm()
                    onOpenChat(s.chatSessionKey.ifBlank { "code:${s.id}" })
                },
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text("채팅에서 작업 이어가기") }

            actionError?.let {
                Spacer(Modifier.height(8.dp))
                Text(it, style = DenebType.hint, color = MaterialTheme.colorScheme.error)
            }

            // Change controls.
            DenebSectionLabel("변경 관리")
            OutlinedTextField(
                value = summary,
                onValueChange = { summary = it },
                label = { Text("저장 요약 (선택)") },
                singleLine = true,
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                OutlinedButton(
                    onClick = {
                        scope.launch {
                            busy = true
                            actionError = null
                            val updated = client.checkpointCodeSession(s.id, summary.trim())
                            if (updated != null) {
                                live = updated
                                summary = ""
                                // The prior verify result no longer matches the new commit.
                                verify = null
                                onChanged()
                            } else {
                                actionError = "변경을 저장하지 못했습니다."
                            }
                            busy = false
                        }
                    },
                    enabled = !busy,
                    modifier = Modifier.weight(1f),
                ) { Text("변경 저장") }
                OutlinedButton(
                    onClick = {
                        scope.launch {
                            busy = true
                            actionError = null
                            val updated = client.undoCodeSession(s.id)
                            if (updated != null) {
                                live = updated
                                // The prior verify result no longer matches the rolled-back tree.
                                verify = null
                                onChanged()
                            } else {
                                actionError = "되돌리지 못했습니다."
                            }
                            busy = false
                        }
                    },
                    enabled = !busy,
                    modifier = Modifier.weight(1f),
                ) { Text("되돌리기") }
            }

            if (s.checkpoints.isNotEmpty()) {
                DenebSectionLabel("체크포인트 ${s.checkpoints.size}")
                s.checkpoints.asReversed().forEach { cp ->
                    DenebRow {
                        Text(cp.summary.ifBlank { "변경 저장" }, style = DenebType.rowTitle)
                        Text("${cp.sha.take(8)} · ${cp.at}", style = DenebType.snippet, color = denebHint())
                    }
                }
            }

            // Verify.
            DenebSectionLabel("검증")
            OutlinedButton(
                onClick = {
                    scope.launch {
                        busy = true
                        actionError = null
                        val v = client.verifyCodeSession(s.id)
                        if (v != null) {
                            verify = v
                            live = v.session
                            onChanged()
                        } else {
                            actionError = "검증을 실행하지 못했습니다."
                        }
                        busy = false
                    }
                },
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text("빌드/테스트 검증") }
            verify?.let { v ->
                Spacer(Modifier.height(8.dp))
                val head = when {
                    v.result.kind == "unknown" -> "인식된 빌드 도구가 없습니다 (검증 생략)"
                    v.result.passed -> "검증 통과 (${v.result.kind})"
                    else -> "검증 실패 (${v.result.kind})"
                }
                Text(head, style = DenebType.rowTitleStrong)
                v.result.steps.forEach { step ->
                    DenebRow {
                        Text("${if (step.ok) "✓" else "✗"} ${step.label} · ${step.cmd}", style = DenebType.rowSubtitle)
                        if (step.output.isNotBlank()) {
                            Text(step.output, style = DenebType.snippet, color = denebHint(), maxLines = 6, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            }

            // Ship.
            DenebSectionLabel("공유")
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                OutlinedButton(
                    onClick = {
                        scope.launch {
                            busy = true
                            actionError = null
                            val err = client.pushCodeSession(s.id)
                            if (err == null) {
                                // "" (not null) so the "아직 PR이 없습니다" hint shows even if
                                // the follow-up PR lookup transport-fails, rather than nothing.
                                prUrl = client.fetchCodePrUrl(s.id) ?: ""
                            } else {
                                actionError = err
                            }
                            busy = false
                        }
                    },
                    enabled = !busy,
                    modifier = Modifier.weight(1f),
                ) { Text("푸시") }
                OutlinedButton(
                    onClick = {
                        scope.launch {
                            busy = true
                            actionError = null
                            prUrl = client.fetchCodePrUrl(s.id) ?: ""
                            busy = false
                        }
                    },
                    enabled = !busy,
                    modifier = Modifier.weight(1f),
                ) { Text("PR 보기") }
            }
            prUrl?.let { url ->
                Spacer(Modifier.height(8.dp))
                if (url.isBlank()) {
                    Text("아직 PR이 없습니다. 푸시 후 채팅에서 PR을 생성하세요.", style = DenebType.hint, color = denebHint())
                } else {
                    Text(url, style = DenebType.snippet)
                }
            }

            // Archive / delete.
            DenebSectionLabel("정리")
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = { pendingClose = true }, enabled = !busy, modifier = Modifier.weight(1f)) {
                    Text("닫기 (보관)")
                }
                TextButton(onClick = { pendingDiscard = true }, enabled = !busy, modifier = Modifier.weight(1f)) {
                    Text("삭제", color = MaterialTheme.colorScheme.error)
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }

    if (pendingClose) {
        AlertDialog(
            onDismissRequest = { pendingClose = false },
            title = { Text("세션 닫기") },
            text = { Text("목록에서 숨깁니다. 워크트리와 브랜치(있다면 PR)는 그대로 보존됩니다.") },
            confirmButton = {
                TextButton(onClick = {
                    haptics.confirm()
                    pendingClose = false
                    scope.launch {
                        busy = true
                        val err = client.closeCodeSession(live.id)
                        busy = false
                        if (err == null) onGone() else actionError = err
                    }
                }) { Text("닫기") }
            },
            dismissButton = { TextButton(onClick = { pendingClose = false }) { Text("취소") } },
        )
    }
    if (pendingDiscard) {
        AlertDialog(
            onDismissRequest = { pendingDiscard = false },
            title = { Text("세션 삭제") },
            text = { Text("워크트리와 브랜치를 영구 삭제합니다. 되돌릴 수 없습니다.") },
            confirmButton = {
                TextButton(onClick = {
                    haptics.reject()
                    pendingDiscard = false
                    scope.launch {
                        busy = true
                        val err = client.discardCodeSession(live.id)
                        busy = false
                        if (err == null) onGone() else actionError = err
                    }
                }) { Text("삭제", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { pendingDiscard = false }) { Text("취소") } },
        )
    }
}

// ---------------------------------------------------------------------------
// Start form: repo picker (+ manual fallback) → start a worktree session.
// ---------------------------------------------------------------------------

@Composable
private fun CodeStartForm(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onStarted: (CodeSession) -> Unit,
    navigationTabBar: (@Composable () -> Unit)?,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    var repos by remember { mutableStateOf<List<CodeRepo>?>(null) }
    var owner by remember { mutableStateOf("") }
    var name by remember { mutableStateOf("") }
    var title by remember { mutableStateOf("") }
    var starting by remember { mutableStateOf(false) }
    var startError by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(Unit) { repos = client.fetchCodeRepos() }

    DenebScreenScaffold(title = "새 코드 세션", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 8.dp),
        ) {
            val list = repos
            if (!list.isNullOrEmpty()) {
                DenebSectionLabel("저장소 선택")
                list.forEach { r ->
                    val selected = owner == r.owner && name == r.name
                    DenebRow(
                        onClick = {
                            owner = r.owner
                            name = r.name
                            startError = null
                        },
                    ) {
                        Text(
                            "${if (selected) "● " else ""}${r.owner}/${r.name}",
                            style = if (selected) DenebType.rowTitleStrong else DenebType.rowTitle,
                        )
                    }
                }
            }

            DenebSectionLabel("저장소 직접 입력")
            OutlinedTextField(
                value = owner,
                onValueChange = {
                    owner = it
                    startError = null
                },
                label = { Text("Owner") },
                placeholder = { Text("choiceoh") },
                singleLine = true,
                enabled = !starting,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = name,
                onValueChange = {
                    name = it
                    startError = null
                },
                label = { Text("Repository") },
                placeholder = { Text("deneb") },
                singleLine = true,
                enabled = !starting,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = title,
                onValueChange = { title = it },
                label = { Text("제목 (선택)") },
                singleLine = true,
                enabled = !starting,
                modifier = Modifier.fillMaxWidth(),
            )

            startError?.let {
                Spacer(Modifier.height(8.dp))
                Text(it, style = DenebType.hint, color = MaterialTheme.colorScheme.error)
            }

            Spacer(Modifier.height(16.dp))
            Button(
                onClick = {
                    haptics.confirm()
                    scope.launch {
                        starting = true
                        startError = null
                        val session = client.startCodeSession(owner.trim(), name.trim(), title = title.trim())
                        starting = false
                        if (session != null) {
                            onStarted(session)
                        } else {
                            startError = "세션을 시작하지 못했습니다. 저장소와 권한을 확인해 주세요."
                        }
                    }
                },
                enabled = !starting && owner.isNotBlank() && name.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
            ) { Text(if (starting) "시작 중…" else "워크트리 시작") }
            Text(
                "시작하면 워크트리가 만들어지고, 채팅에서 작업을 지시하면 그 워크트리를 편집합니다.",
                style = DenebType.hint,
                color = denebHint(),
                modifier = Modifier.padding(top = 8.dp),
            )
            Spacer(Modifier.height(24.dp))
        }
    }
}

private fun codeStatusLabel(status: String): String = when (status) {
    "working" -> "작업 중"
    "passed" -> "검증 통과"
    "failed" -> "검증 실패"
    "missing" -> "워크트리 없음"
    "closed" -> "보관됨"
    else -> status
}
