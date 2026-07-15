package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
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
    var text by remember { mutableStateOf<String?>(null) }
    var failed by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        failed = false
        text = null
        val q = if (selected.searchable) query.trim().ifBlank { null } else null
        val resp = client.fetchERP(area = selected.key, query = q)
        if (resp == null) {
            failed = true
        } else {
            text = resp.text.ifBlank { "(데이터 없음)" }
        }
    }

    LaunchedEffect(selected.key) {
        query = ""
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
            Column(
                Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp),
            ) {
                when {
                    failed -> DenebError(
                        "ERP 데이터를 불러오지 못했습니다.",
                        onRetry = { scope.launch { load() } },
                    )

                    text == null -> DenebLoading()

                    text == "(데이터 없음)" -> DenebEmpty("결과 없음")

                    else -> SelectionContainer {
                        MarkdownContent(
                            content = erpTextToMarkdown(text.orEmpty()),
                            modifier = Modifier.fillMaxWidth(),
                            baseStyle = DenebType.body,
                        )
                    }
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}
