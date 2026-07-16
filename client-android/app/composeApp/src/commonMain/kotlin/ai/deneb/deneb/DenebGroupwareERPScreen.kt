package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebUnderlineSearchField
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
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
 * 그룹웨어 ERP 스냅샷 (`miniapp.groupware.erp.list`) — 영역 칩 + 네이티브 행,
 * 게시판 글 탭 → 본문 시트, 당겨서 새로고침.
 */
@OptIn(ExperimentalMaterial3Api::class)
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
    var refreshing by remember { mutableStateOf(false) }
    // Board post sheet: query being fetched → text (null while loading).
    var boardQuery by remember { mutableStateOf<String?>(null) }
    var boardText by remember { mutableStateOf<String?>(null) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    // 사원 is a directory lookup — the reader requires a name/부서 query, so an
    // unqueried auto-fetch would just surface a dependency error.
    fun needsQuery() = selected.key == "people" && query.isBlank()

    suspend fun load(clear: Boolean = true) {
        failed = false
        if (clear) text = null
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

    fun openBoardPost(row: ErpBlock.Row) {
        val q = row.refId.ifBlank { row.title }
        if (q.isBlank()) return
        haptics.tap()
        boardQuery = q
        boardText = null
        scope.launch {
            val resp = client.fetchBoardPost(q)
            if (boardQuery == q) {
                boardText = resp?.text ?: "게시글을 불러오지 못했습니다."
            }
        }
    }

    DenebScreenScaffold(title = "그룹웨어", onBack = onBack, tabBar = navigationTabBar) {
        Column(Modifier.fillMaxSize()) {
            // Flat text switcher (design-system view transition, mail 정리본·원문
            // parity) — a row of bordered 48dp chips read as buttons, not tabs.
            Row(
                Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 24.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(18.dp),
            ) {
                erpAreas.forEach { area ->
                    ErpFlatTab(
                        label = area.label,
                        active = selected.key == area.key,
                        onClick = {
                            haptics.tap()
                            selected = area
                        },
                    )
                }
            }
            if (selected.key == "sales") {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .horizontalScroll(rememberScrollState())
                        .padding(horizontal = 24.dp, vertical = 2.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    salesPeriods.forEach { (key, label) ->
                        ErpFlatTab(
                            label = label,
                            active = salesPeriod == key,
                            compact = true,
                            onClick = {
                                haptics.tap()
                                salesPeriod = key
                                scope.launch { load() }
                            },
                        )
                    }
                }
            }
            if (selected.searchable) {
                // Flat underline search (mail parity) — the boxed OutlinedTextField +
                // TextButton pair looked like a form dropped into the list.
                DenebUnderlineSearchField(
                    query = query,
                    onQueryChange = { query = it },
                    placeholder = selected.hint.ifBlank { "검색어" },
                    textStyle = DenebType.rowTitle,
                    clearable = true,
                    onSearch = {
                        haptics.tap()
                        scope.launch { load() }
                    },
                    modifier = Modifier.padding(horizontal = 24.dp, vertical = 6.dp),
                )
            }
            Spacer(Modifier.height(8.dp))
            PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = {
                    haptics.refresh()
                    scope.launch {
                        refreshing = true
                        load(clear = false)
                        refreshing = false
                    }
                },
                modifier = Modifier.fillMaxWidth().weight(1f),
            ) {
                val snapshot = text
                when {
                    failed -> DenebError(
                        "ERP 데이터를 불러오지 못했습니다.",
                        onRetry = { scope.launch { load() } },
                    )

                    needsQuery() && snapshot == null -> DenebEmpty("이름이나 부서로 검색하세요")

                    snapshot == null -> DenebLoading()

                    snapshot == "(데이터 없음)" -> DenebEmpty("결과 없음")

                    else -> ErpSnapshotContent(
                        snapshot = snapshot,
                        onOpenRow = if (selected.key == "board") ::openBoardPost else null,
                    )
                }
            }
        }
    }

    // 게시판 글 본문 시트 — 목록만 보이고 열 수 없던 화면을 완결시킨다.
    boardQuery?.let {
        ModalBottomSheet(onDismissRequest = { boardQuery = null }) {
            Column(
                Modifier
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 20.dp),
            ) {
                val post = boardText
                if (post == null) {
                    DenebLoading()
                } else {
                    val sections = remember(post) { parseApprovalDocBody(post) }
                    Text(
                        sections.title.ifBlank { "게시글" },
                        style = DenebType.cardTitle,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Spacer(Modifier.height(10.dp))
                    SelectionContainer {
                        MarkdownContent(
                            content = sections.body.ifBlank { post },
                            modifier = Modifier.fillMaxWidth(),
                            baseStyle = DenebType.body,
                        )
                    }
                }
                Spacer(Modifier.height(32.dp))
            }
        }
    }
}

// Flat text tab (design-system view switcher, mail 정리본·원문 parity): active in
// the cool primary accent with a short underline, inactive muted — no borders.
@Composable
private fun ErpFlatTab(
    label: String,
    active: Boolean,
    onClick: () -> Unit,
    compact: Boolean = false,
) {
    Column(
        Modifier
            .clip(RoundedCornerShape(6.dp))
            .clickable(onClickLabel = "$label 조회", role = Role.Button, onClick = onClick)
            .handCursor()
            .padding(vertical = 6.dp, horizontal = 2.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            label,
            style = (if (compact) DenebType.meta else DenebType.rowTitle).copy(
                fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
            ),
            color = if (active) MaterialTheme.colorScheme.primary else denebHint(),
        )
        Spacer(Modifier.height(3.dp))
        Box(
            Modifier
                .width(if (compact) 14.dp else 18.dp)
                .height(2.dp)
                .clip(RoundedCornerShape(1.dp))
                .background(
                    if (active) MaterialTheme.colorScheme.primary else Color.Transparent,
                ),
        )
    }
}

// Structured render of the reader snapshot: summary card, section labels, native
// rows. Falls back to markdown for shapes the parser doesn't recognize.
@Composable
private fun ErpSnapshotContent(snapshot: String, onOpenRow: ((ErpBlock.Row) -> Unit)? = null) {
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
                    val rowModifier = if (onOpenRow != null) {
                        Modifier
                            .fillMaxWidth()
                            .clickable(onClickLabel = "게시글 열기", role = Role.Button) { onOpenRow(block) }
                            .handCursor()
                            .padding(horizontal = 16.dp, vertical = 10.dp)
                    } else {
                        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)
                    }
                    Column(rowModifier) {
                        val rowContent: @Composable () -> Unit = {
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
                        // Tappable rows skip SelectionContainer — it swallows taps.
                        if (onOpenRow != null) rowContent() else SelectionContainer { rowContent() }
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
