package ai.deneb.deneb

import ai.deneb.contacts.ContactsWriter
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.tools.SetupContactsPermissionHandler
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.Button
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/** Ambiguous pairs sent per adjudicate call — must stay ≤ the gateway's cap
 *  (maxAdjudicatePairs=32) so one request never runs for minutes. */
private const val adjudicateChunk = 32

/**
 * 연락처 정리 — the address-book dedup ([miniapp.contacts.dedup]) plus a one-tap
 * apply that LINKS the safe duplicate groups on the device (Android
 * AggregationExceptions: no deletion, reversible). The phone triple-syncs and
 * balloons past the real headcount; this collapses the clear duplicates. Reached
 * from 더보기. Stateful shell (load + apply + states); the previewable body is
 * [ContactsDedupContent].
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
    val perms = remember { ContactsPermissionController() }
    SetupContactsPermissionHandler(perms)
    var applying by remember { mutableStateOf(false) }
    var applyMsg by remember { mutableStateOf<String?>(null) }

    suspend fun load() {
        failed = false
        val fetched = client.fetchContactsDedup()
        if (fetched == null) failed = true else payload = fetched
    }
    LaunchedEffect(Unit) { load() }

    // One-tap autonomous cleanup: first link every deterministic safe group, then
    // let the AI rule on the ambiguous pairs in bounded chunks and link the ones it
    // calls the same person. All links are reversible AggregationExceptions (no
    // deletion). Requests WRITE the first time; no-op without it.
    suspend fun applyAll(data: ContactsDedupPayload) {
        applying = true
        applyMsg = null
        if (!writer.hasAccess()) perms.requestPermission()
        if (!writer.hasAccess()) {
            applyMsg = "연락처 쓰기 권한이 필요합니다 (설정 → 권한 → 연락처)"
            applying = false
            return
        }
        // 1) deterministic safe merges — the clear duplicates.
        var ruleMerged = 0
        for (m in data.merges) {
            if (writer.linkByIdentity(m.phones, m.emails) >= 2) ruleMerged++
        }
        // 2) AI adjudication of the ambiguous pairs, ≤32 per request so no single
        //    call runs for minutes. SAME → link; diff/unsure left untouched.
        var aiMerged = 0
        val pairs = data.ambiguousPairs
        var done = 0
        for (chunk in pairs.chunked(adjudicateChunk)) {
            applyMsg = "AI 검토 중… $done/${pairs.size}"
            val verdicts = client.fetchContactsAdjudicate(chunk).orEmpty()
            chunk.forEachIndexed { i, pair ->
                if (verdicts.getOrNull(i) == "same" &&
                    writer.linkByIdentity(pair.a.phones + pair.b.phones, pair.a.emails + pair.b.emails) >= 2
                ) {
                    aiMerged++
                }
            }
            done += chunk.size
        }
        applyMsg = "폰에서 ${ruleMerged + aiMerged}개 합침 (규칙 $ruleMerged · AI $aiMerged)"
        applying = false
    }

    DenebScreenScaffold(title = "연락처 정리", onBack = onBack, tabBar = navigationTabBar) {
        val p = payload
        when {
            failed -> DenebError("정리 결과를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })

            p == null -> DenebLoading()

            p.total == 0 -> DenebEmpty("주소록이 비어 있습니다.", icon = Icons.Outlined.Person, hint = "주소록이 동기화되면 여기에 나타납니다")

            else -> ContactsDedupContent(
                payload = p,
                canApply = writer.isSupported(),
                applying = applying,
                applyMsg = applyMsg,
                onApply = { scope.launch { applyAll(p) } },
            )
        }
    }
}

/** Stateless over [payload]: a typographic summary (총 N → 정리 M명), a one-tap 정리
 *  action, then the safe merge groups. No boxed KPI tiles — inline hierarchy. The
 *  action params default off so the render harness drives the pure layout. */
@Composable
internal fun ContactsDedupContent(
    payload: ContactsDedupPayload,
    canApply: Boolean = false,
    applying: Boolean = false,
    applyMsg: String? = null,
    onApply: () -> Unit = {},
) {
    LazyColumn(Modifier.fillMaxSize()) {
        item {
            Column(Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 16.dp)) {
                Text(
                    "주소록 ${payload.total}개 → 정리 후 ${payload.distinct}명",
                    style = DenebType.cardTitle,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                Spacer(Modifier.height(4.dp))
                Text(
                    "안전 병합 ${payload.merges.size}군 · AI 검토 필요 ${payload.ambiguous}쌍",
                    style = DenebType.sectionLabel,
                    color = denebHint(),
                )
                if (canApply && (payload.merges.isNotEmpty() || payload.ambiguous > 0)) {
                    Spacer(Modifier.height(16.dp))
                    Button(onClick = onApply, enabled = !applying) {
                        Text(if (applying) "정리 중…" else "폰에 정리 적용", style = DenebType.button)
                    }
                }
                if (applyMsg != null) {
                    Spacer(Modifier.height(8.dp))
                    Text(applyMsg, style = DenebType.rowSubtitle, color = denebHint())
                }
            }
        }
        if (payload.merges.isEmpty()) {
            item {
                Text(
                    "안전하게 합칠 중복이 없습니다.",
                    style = DenebType.body,
                    color = denebHint(),
                    modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
                )
            }
        } else {
            item { DenebSectionLabel("안전 병합 (${payload.merges.size}군)") }
            itemsIndexed(payload.merges, key = { i, _ -> i }) { _, merge ->
                DedupMergeRowItem(merge)
            }
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
