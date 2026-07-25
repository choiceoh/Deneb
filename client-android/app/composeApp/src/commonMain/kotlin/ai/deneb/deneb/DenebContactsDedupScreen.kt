package ai.deneb.deneb

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

/**
 * 연락처 정리 — a preview of the deterministic address-book dedup (`miniapp.contacts.dedup`).
 * The device address book triple-syncs (local/Samsung/Google) and balloons past the real
 * headcount; this shows how many entries safely collapse to how many people, and the merge
 * groups behind that. Read-only preview: it never mutates the book (applying and the AI pass
 * for ambiguous pairs are separate steps). Reached from 더보기. Stateful shell (load + states);
 * the previewable body is [ContactsDedupContent].
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

    suspend fun load() {
        failed = false
        val fetched = client.fetchContactsDedup()
        if (fetched == null) failed = true else payload = fetched
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "연락처 정리", onBack = onBack, tabBar = navigationTabBar) {
        val p = payload
        when {
            failed -> DenebError("정리 결과를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
            p == null -> DenebLoading()
            p.total == 0 -> DenebEmpty("주소록이 비어 있습니다.", icon = Icons.Outlined.Person, hint = "주소록이 동기화되면 여기에 나타납니다")
            else -> ContactsDedupContent(p)
        }
    }
}

/** Stateless over [payload] so the render harness can drive it: a typographic summary
 *  (총 N → 정리 M명) followed by the safe merge groups. No boxed KPI tiles — inline
 *  hierarchy, per the design system. */
@Composable
internal fun ContactsDedupContent(payload: ContactsDedupPayload) {
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
