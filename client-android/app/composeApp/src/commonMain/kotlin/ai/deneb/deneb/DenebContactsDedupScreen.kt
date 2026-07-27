package ai.deneb.deneb

import ai.deneb.contacts.ContactsReader
import ai.deneb.contacts.ContactParty
import ai.deneb.contacts.ContactsWriter
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.tools.SetupContactsPermissionHandler
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.icons.outlined.AutoAwesome
import ai.deneb.ui.icons.outlined.Contacts
import ai.deneb.ui.icons.outlined.Link
import ai.deneb.ui.icons.outlined.Restore
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/** Ambiguous pairs per adjudicate call. Small (≤ the gateway cap of 32) so the AI
 *  review reports back often — the progress bar advances every ~chunk instead of
 *  sitting still for a minutes-long request. 8 also matches the adapter's internal
 *  model batch, so one RPC ≈ one model call. */
private const val adjudicateChunk = 8

/**
 * The apply flow made legible. Each phase is its own state so the (multi-minute) AI
 * review renders as a live step with a moving bar and a climbing count — the user
 * can see it working and that the AI pass really ran, instead of one static line.
 */
internal sealed interface ContactsApplyState {
    data object Idle : ContactsApplyState
    data object NeedPermission : ContactsApplyState
    data class RuleMerging(val done: Int, val total: Int) : ContactsApplyState
    data class AiReviewing(val done: Int, val total: Int, val same: Int) : ContactsApplyState
    data object Syncing : ContactsApplyState
    data class Done(val ruleMerged: Int, val aiMerged: Int, val synced: Boolean) : ContactsApplyState
}

/**
 * 연락처 정리 — the address-book dedup ([miniapp.contacts.dedup]) plus a one-tap
 * apply that LINKS the safe duplicate groups AND runs the AI over the ambiguous
 * pairs, then re-syncs so the gateway mirror (and the 인물 위키) heal. All links are
 * reversible Android AggregationExceptions (no deletion). Reached from 더보기.
 * Stateful shell (load + apply + phase state); previewable body is [ContactsDedupContent].
 */
@Composable
fun DenebContactsDedupScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var payload by remember { mutableStateOf<ContactsDedupPayload?>(null) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    val writer = remember { ContactsWriter() }
    val reader = remember { ContactsReader() }
    val perms = remember { ContactsPermissionController() }
    SetupContactsPermissionHandler(perms)
    var applyState by remember { mutableStateOf<ContactsApplyState>(ContactsApplyState.Idle) }

    suspend fun load() {
        failed = false
        val fetched = client.fetchContactsDedup()
        if (fetched == null) failed = true else payload = fetched
    }
    LaunchedEffect(Unit) { load() }

    // One tap → the whole cleanup, driving [applyState] through each phase so the UI
    // stays visibly alive: (1) link the deterministic safe groups, (2) let the AI
    // rule on the ambiguous pairs in small chunks (SAME → link), (3) re-sync the
    // now-merged book so the mirror/wiki heal. Requests WRITE the first time.
    suspend fun applyAll(data: ContactsDedupPayload) {
        if (!writer.hasAccess()) perms.requestPermission()
        if (!writer.hasAccess()) {
            applyState = ContactsApplyState.NeedPermission
            return
        }
        // 1) deterministic safe merges — the clear duplicates, counted as we go.
        var ruleMerged = 0
        applyState = ContactsApplyState.RuleMerging(done = 0, total = data.merges.size)
        data.merges.forEachIndexed { i, m ->
            val members = mergeMembers(m)
            if (members.size >= 2 && writer.linkMergeMembers(members) >= 2) ruleMerged++
            applyState = ContactsApplyState.RuleMerging(done = i + 1, total = data.merges.size)
        }
        // 2) AI adjudication of the ambiguous pairs. SAME → link; diff/unsure left be.
        val pairs = data.ambiguousPairs
        var aiMerged = 0
        var reviewed = 0
        applyState = ContactsApplyState.AiReviewing(done = 0, total = pairs.size, same = 0)
        for (chunk in pairs.chunked(adjudicateChunk)) {
            val verdicts = client.fetchContactsAdjudicate(chunk).orEmpty()
            chunk.forEachIndexed { i, pair ->
                if (verdicts.getOrNull(i) == "same" &&
                    writer.linkMergeMembers(
                        listOf(
                            ContactParty(pair.a.name, pair.a.phones, pair.a.emails),
                            ContactParty(pair.b.name, pair.b.phones, pair.b.emails),
                        ),
                    ) >= 2
                ) {
                    aiMerged++
                }
            }
            reviewed = (reviewed + chunk.size).coerceAtMost(pairs.size)
            applyState = ContactsApplyState.AiReviewing(done = reviewed, total = pairs.size, same = aiMerged)
        }
        // 3) close the loop back to the knowledge source (mirror ReplaceAll → 인물 위키).
        var synced = false
        if (ruleMerged + aiMerged > 0) {
            applyState = ContactsApplyState.Syncing
            val merged = reader.readAll()
            if (merged.isNotEmpty()) {
                client.captureContacts(merged)
                synced = true
            }
        }
        applyState = ContactsApplyState.Done(ruleMerged = ruleMerged, aiMerged = aiMerged, synced = synced)
    }

    DenebScreenScaffold(title = "연락처 정리", onBack = onBack, tabBar = navigationTabBar) {
        val p = payload
        when {
            failed -> DenebError("정리 결과를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })

            p == null -> DenebLoading()

            p.total == 0 -> DenebEmpty("주소록이 비어 있습니다.", icon = Icons.Outlined.Contacts, hint = "주소록이 동기화되면 여기에 나타납니다")

            else -> ContactsDedupContent(
                payload = p,
                canApply = writer.isSupported(),
                applyState = applyState,
                onApply = { scope.launch { applyAll(p) } },
                onRescan = {
                    scope.launch {
                        applyState = ContactsApplyState.Idle
                        load()
                    }
                },
            )
        }
    }
}

/** Stateless over [payload] + [applyState]. Idle shows the plan (what tapping does)
 *  and a preview of the safe merges; while applying it shows a live phase stepper;
 *  when done, a plain result. No boxed tiles — inline typographic hierarchy. */
@Composable
internal fun ContactsDedupContent(
    payload: ContactsDedupPayload,
    canApply: Boolean = false,
    applyState: ContactsApplyState = ContactsApplyState.Idle,
    onApply: () -> Unit = {},
    onRescan: () -> Unit = {},
) {
    val idle = applyState is ContactsApplyState.Idle || applyState is ContactsApplyState.NeedPermission
    LazyColumn(Modifier.fillMaxSize()) {
        item {
            Column(Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 16.dp)) {
                when (val s = applyState) {
                    is ContactsApplyState.Done -> DedupResult(s, onRescan)

                    is ContactsApplyState.RuleMerging,
                    is ContactsApplyState.AiReviewing,
                    ContactsApplyState.Syncing,
                    -> {
                        Text("정리하는 중…", style = DenebType.cardTitle, color = MaterialTheme.colorScheme.onBackground)
                        Spacer(Modifier.height(16.dp))
                        DedupProgress(s)
                    }

                    ContactsApplyState.Idle,
                    ContactsApplyState.NeedPermission,
                    -> {
                        DedupHeader(payload)
                        Spacer(Modifier.height(20.dp))
                        DedupPlan(
                            payload = payload,
                            canApply = canApply,
                            needPermission = applyState is ContactsApplyState.NeedPermission,
                            onApply = onApply,
                        )
                    }
                }
            }
        }
        if (idle && payload.merges.isNotEmpty()) {
            item { DenebSectionLabel("이렇게 합쳐집니다 (${payload.merges.size}군)") }
            itemsIndexed(payload.merges, key = { i, _ -> i }) { _, merge ->
                DedupMergeRowItem(merge)
            }
        }
    }
}

@Composable
private fun DedupHeader(payload: ContactsDedupPayload) {
    Text(
        "주소록 ${payload.total.grouped()}개",
        style = DenebType.cardTitle,
        color = MaterialTheme.colorScheme.onBackground,
    )
    Spacer(Modifier.height(4.dp))
    Text(
        "정리하면 약 ${payload.distinct.grouped()}명이 됩니다",
        style = DenebType.sectionLabel,
        color = denebHint(),
    )
}

/** The plan: what one tap actually does, so the AI pass isn't a hidden surprise. */
@Composable
private fun DedupPlan(
    payload: ContactsDedupPayload,
    canApply: Boolean,
    needPermission: Boolean,
    onApply: () -> Unit,
) {
    PlanRow(Icons.Outlined.Link, MaterialTheme.colorScheme.primary, "확실한 중복 ${payload.merges.size}군", "바로 합칩니다")
    PlanRow(Icons.Outlined.AutoAwesome, denebInsight(), "애매한 연락처 ${payload.ambiguous}쌍", "AI가 한 명씩 검토해요 · 몇 분 걸릴 수 있어요")
    PlanRow(Icons.Outlined.Restore, denebHint(), "삭제가 아니라 링크", "언제든 되돌릴 수 있어요")
    if (needPermission) {
        Spacer(Modifier.height(12.dp))
        Text(
            "연락처 쓰기 권한이 필요합니다 (설정 → 권한 → 연락처)",
            style = DenebType.rowSubtitle,
            color = denebInsight(),
        )
    }
    if (canApply && (payload.merges.isNotEmpty() || payload.ambiguous > 0)) {
        Spacer(Modifier.height(20.dp))
        Button(onClick = onApply, modifier = Modifier.fillMaxWidth()) {
            Text("정리 시작", style = DenebType.button)
        }
    }
}

@Composable
private fun PlanRow(icon: ImageVector, tint: androidx.compose.ui.graphics.Color, title: String, detail: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text(title, style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onBackground)
            Text(detail, style = DenebType.rowSubtitle, color = denebHint())
        }
    }
}

/** Live phase stepper. A spinner keeps a step visibly "working" between the periodic
 *  count updates, so the minutes-long AI review never looks frozen. */
@Composable
private fun DedupProgress(state: ContactsApplyState) {
    val ruleActive = state is ContactsApplyState.RuleMerging
    StepRow(
        status = if (ruleActive) StepStatus.Active else StepStatus.Complete,
        label = "확실한 중복 합치기",
        trailing = if (state is ContactsApplyState.RuleMerging) "${state.done} / ${state.total}" else "완료",
    )

    val aiStatus = when (state) {
        is ContactsApplyState.RuleMerging -> StepStatus.Pending
        is ContactsApplyState.AiReviewing -> StepStatus.Active
        else -> StepStatus.Complete
    }
    StepRow(
        status = aiStatus,
        label = "AI가 애매한 연락처 검토",
        trailing = when (state) {
            is ContactsApplyState.AiReviewing -> "${state.done} / ${state.total}"
            is ContactsApplyState.RuleMerging -> null
            else -> "완료"
        },
    )
    if (state is ContactsApplyState.AiReviewing) {
        Spacer(Modifier.height(8.dp))
        LinearProgressIndicator(
            progress = { if (state.total == 0) 0f else state.done.toFloat() / state.total },
            modifier = Modifier.fillMaxWidth().padding(start = 28.dp),
            color = denebInsight(),
        )
        Spacer(Modifier.height(6.dp))
        Text(
            "지금까지 같은 사람 ${state.same}쌍 발견",
            style = DenebType.rowSubtitle,
            color = denebHint(),
            modifier = Modifier.padding(start = 28.dp),
        )
    }

    StepRow(
        status = if (state is ContactsApplyState.Syncing) StepStatus.Active else StepStatus.Pending,
        label = "지식 미러 동기화",
        trailing = null,
    )
}

private enum class StepStatus { Pending, Active, Complete }

@Composable
private fun StepRow(status: StepStatus, label: String, trailing: String?) {
    Row(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        StepDot(status)
        Spacer(Modifier.width(12.dp))
        Text(
            label,
            style = DenebType.rowTitle,
            color = if (status == StepStatus.Pending) denebHint() else MaterialTheme.colorScheme.onBackground,
            modifier = Modifier.weight(1f),
        )
        if (trailing != null) {
            Text(
                trailing,
                style = DenebType.meta,
                color = if (status == StepStatus.Active) denebInsight() else denebHint(),
            )
        }
    }
}

@Composable
private fun StepDot(status: StepStatus) {
    when (status) {
        StepStatus.Active -> CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp, color = denebInsight())
        StepStatus.Complete -> Spacer(Modifier.size(16.dp).clip(CircleShape).background(denebInsight()))
        StepStatus.Pending -> Spacer(Modifier.size(16.dp).clip(CircleShape).border(1.5.dp, denebHint(), CircleShape))
    }
}

/** The unambiguous "it worked" state: what changed, that the mirror healed, that it's
 *  reversible, and a way to re-check (which shows the now-smaller count). */
@Composable
private fun DedupResult(state: ContactsApplyState.Done, onRescan: () -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Icon(Icons.Outlined.AutoAwesome, contentDescription = null, tint = denebInsight(), modifier = Modifier.size(32.dp))
        Spacer(Modifier.height(12.dp))
        Text("정리 완료", style = DenebType.cardTitle, color = MaterialTheme.colorScheme.onBackground)
        Spacer(Modifier.height(4.dp))
        Text(
            "총 ${(state.ruleMerged + state.aiMerged).grouped()}건을 하나로 합쳤어요",
            style = DenebType.rowTitleStrong,
            color = MaterialTheme.colorScheme.onBackground,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            "규칙으로 ${state.ruleMerged}건 · AI 검토로 ${state.aiMerged}건",
            style = DenebType.rowSubtitle,
            color = denebHint(),
        )
        if (state.synced) {
            Spacer(Modifier.height(4.dp))
            Text(
                "지식 미러도 갱신돼 인물 정보가 정확해졌어요.",
                style = DenebType.rowSubtitle,
                color = denebHint(),
            )
        }
        Spacer(Modifier.height(8.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.Restore, contentDescription = null, tint = denebHint(), modifier = Modifier.size(14.dp))
            Spacer(Modifier.width(6.dp))
            Text("잘못 합쳐진 건 언제든 되돌릴 수 있어요.", style = DenebType.meta, color = denebHint())
        }
        Spacer(Modifier.height(20.dp))
        Button(onClick = onRescan, modifier = Modifier.fillMaxWidth()) {
            Text("다시 검사", style = DenebType.button)
        }
    }
}

@Composable
private fun DedupMergeRowItem(merge: DedupMergeRow) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 24.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                merge.canonical.ifBlank { "(이름 없음)" },
                style = DenebType.rowTitle,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                "${merge.names.size}개 합침",
                style = DenebType.meta,
                color = denebHint(),
            )
        }
        val variants = merge.names.filter { it.isNotBlank() && it != merge.canonical }.joinToString(" · ")
        if (variants.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(
                variants,
                style = DenebType.rowSubtitle,
                color = denebHint(),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** 2,819 not 2819 — grouped thousands so the headline counts read at a glance. */
private fun Int.grouped(): String = toString().reversed().chunked(3).joinToString(",").reversed()

private fun mergeMembers(row: DedupMergeRow): List<ContactParty> =
    if (row.members.isNotEmpty()) {
        row.members.map { ContactParty(it.name, it.phones, it.emails) }
    } else {
        row.names.map { ContactParty(it, row.phones, row.emails) }
    }
