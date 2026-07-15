package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

private data class ErpArea(val key: String, val label: String, val searchable: Boolean = false, val hint: String = "")

private val erpAreas = listOf(
    ErpArea("stock", "재고", searchable = true, hint = "품목·코드"),
    ErpArea("po", "발주"),
    ErpArea("receive", "입고"),
    ErpArea("ship", "출고"),
    ErpArea("price", "단가"),
    ErpArea("sales", "매출"),
    ErpArea("people", "사원", searchable = true, hint = "이름·부서"),
    ErpArea("board", "게시판"),
)

// 매출 기간 (reader sales folder → 요약 집계; 빈 값 = 최근 목록).
private val salesPeriods = listOf(
    "" to "최근",
    "today" to "오늘",
    "month" to "이번 달",
    "ytd" to "연초부터",
    "year" to "올해",
    "last_year" to "작년",
)

/** Promote the first summary line to a markdown heading (Andromeda parity). */
internal fun erpTextToMarkdown(raw: String): String {
    val lines = raw.replace("\r\n", "\n").trim().lines()
    if (lines.isEmpty() || (lines.size == 1 && lines[0].isBlank())) return ""
    val out = ArrayList<String>(lines.size)
    var headed = false
    for (line in lines) {
        val t = line.trimEnd()
        if (!headed && t.isNotBlank()) {
            out += "## ${t.trim()}"
            headed = true
            continue
        }
        out += t
    }
    return out.joinToString("\n")
}

/**
 * 그룹웨어 ERP 스냅샷 (`miniapp.groupware.erp.list`) — 영역 칩 + 마크다운 결과.
 */
@Composable
fun DenebGroupwareERPScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var selected by remember { mutableStateOf(erpAreas.first()) }
    var query by remember { mutableStateOf("") }
    var salesPeriod by remember { mutableStateOf("") }
    var text by remember { mutableStateOf<String?>(null) }
    var failed by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    // 사원 is a directory lookup — the reader requires a name/부서 query, so an
    // unqueried auto-fetch would just surface a dependency error.
    fun needsQuery() = selected.key == "people" && query.isBlank()

    suspend fun load() {
        failed = false
        text = null
        if (needsQuery()) return
        val q = if (selected.searchable) query.trim().ifBlank { null } else null
        val folder = if (selected.key == "sales") salesPeriod.ifBlank { null } else null
        val resp = client.fetchERP(area = selected.key, folder = folder, query = q)
        if (resp == null) {
            failed = true
        } else {
            text = resp.text.ifBlank { "(데이터 없음)" }
        }
    }

    LaunchedEffect(selected.key) {
        query = ""
        salesPeriod = ""
        load()
    }

    DenebScreenScaffold(title = "그룹웨어", onBack = onBack, tabBar = navigationTabBar) {
        Column(Modifier.fillMaxSize()) {
            Text(
                "Amaranth ERP 조회 · 결재는 「결재」에서",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
            Row(
                Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                erpAreas.forEach { area ->
                    DenebChip(
                        selected = selected.key == area.key,
                        onClick = {
                            haptics.tap()
                            selected = area
                        },
                    ) { Text(area.label, style = DenebType.button) }
                }
            }
            if (selected.key == "sales") {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .horizontalScroll(rememberScrollState())
                        .padding(horizontal = 16.dp, vertical = 4.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    salesPeriods.forEach { (key, label) ->
                        DenebChip(
                            selected = salesPeriod == key,
                            onClick = {
                                haptics.tap()
                                salesPeriod = key
                                scope.launch { load() }
                            },
                        ) { Text(label, style = DenebType.button) }
                    }
                }
            }
            if (selected.searchable) {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    OutlinedTextField(
                        value = query,
                        onValueChange = { query = it },
                        placeholder = { Text(selected.hint.ifBlank { "검색어" }) },
                        modifier = Modifier.weight(1f),
                        singleLine = true,
                    )
                    TextButton(
                        onClick = {
                            haptics.tap()
                            scope.launch { load() }
                        },
                    ) { Text("검색") }
                }
            }
            Spacer(Modifier.height(8.dp))
            Box(Modifier.fillMaxWidth().weight(1f)) {
                val snapshot = text
                when {
                    failed -> DenebError(
                        "ERP 데이터를 불러오지 못했습니다.",
                        onRetry = { scope.launch { load() } },
                    )

                    needsQuery() && snapshot == null -> DenebEmpty("이름이나 부서로 검색하세요")

                    snapshot == null -> DenebLoading()

                    snapshot == "(데이터 없음)" -> DenebEmpty("결과 없음")

                    else -> ErpSnapshotContent(snapshot)
                }
            }
        }
    }
}

// Structured render of the reader snapshot: summary card, section labels, native
// rows. Falls back to markdown for shapes the parser doesn't recognize.
@Composable
private fun ErpSnapshotContent(snapshot: String) {
    val blocks = remember(snapshot) { parseErpSnapshot(snapshot) }
    if (blocks.isEmpty()) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp),
        ) {
            SelectionContainer {
                MarkdownContent(
                    content = erpTextToMarkdown(snapshot),
                    modifier = Modifier.fillMaxWidth(),
                    baseStyle = DenebType.body,
                )
            }
            Spacer(Modifier.height(24.dp))
        }
        return
    }
    LazyColumn(Modifier.fillMaxSize()) {
        itemsIndexed(blocks) { i, block ->
            when (block) {
                is ErpBlock.Summary -> Column(
                    Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 4.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f))
                        .padding(horizontal = 14.dp, vertical = 10.dp),
                ) {
                    block.lines.forEachIndexed { j, line ->
                        Text(
                            line,
                            style = if (j == 0) DenebType.rowTitleStrong else DenebType.meta,
                            color = if (j == 0) MaterialTheme.colorScheme.onSurface else denebHint(),
                        )
                        if (j == 0 && block.lines.size > 1) Spacer(Modifier.height(3.dp))
                    }
                }

                is ErpBlock.Section -> DenebSectionLabel(
                    block.label,
                    modifier = Modifier.padding(horizontal = 16.dp),
                    topPadding = 14.dp,
                )

                is ErpBlock.Row -> {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
                        SelectionContainer {
                            Column {
                                Text(
                                    block.title,
                                    style = DenebType.rowTitle,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                if (block.meta.isNotBlank()) {
                                    Spacer(Modifier.height(3.dp))
                                    Text(
                                        block.meta,
                                        style = DenebType.meta,
                                        color = denebHint(),
                                        maxLines = 3,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                            }
                        }
                    }
                    if (i < blocks.lastIndex && blocks[i + 1] is ErpBlock.Row) {
                        HorizontalDivider(
                            color = denebHairline(),
                            thickness = 0.5.dp,
                            modifier = Modifier.padding(horizontal = 16.dp),
                        )
                    }
                }
            }
        }
        item { Spacer(Modifier.height(24.dp)) }
    }
}
