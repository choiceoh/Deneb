package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.FilterChip
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

private data class ErpArea(val key: String, val label: String, val searchable: Boolean = false)

private val erpAreas = listOf(
    ErpArea("stock", "재고", searchable = true),
    ErpArea("po", "발주"),
    ErpArea("receive", "입고"),
    ErpArea("ship", "출고"),
    ErpArea("price", "단가"),
    ErpArea("sales", "매출"),
    ErpArea("people", "사원", searchable = true),
)

/**
 * 그룹웨어 ERP 스냅샷 (`miniapp.groupware.erp.list`) — 영역별 텍스트 조회.
 */
@OptIn(ExperimentalLayoutApi::class)
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
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp),
        ) {
            Spacer(Modifier.height(4.dp))
            FlowRow(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                erpAreas.forEach { area ->
                    FilterChip(
                        selected = selected.key == area.key,
                        onClick = {
                            haptics.tap()
                            selected = area
                        },
                        label = { Text(area.label) },
                    )
                }
            }
            if (selected.searchable) {
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    placeholder = { Text("검색어") },
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true,
                )
                TextButton(
                    onClick = {
                        haptics.tap()
                        scope.launch { load() }
                    },
                    modifier = Modifier.align(Alignment.End),
                ) { Text("조회") }
            }
            Spacer(Modifier.height(12.dp))
            when {
                failed -> DenebError("ERP 데이터를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })

                text == null -> DenebLoading()

                else -> SelectionContainer {
                    Text(
                        text = text.orEmpty(),
                        style = DenebType.body.copy(fontFamily = FontFamily.Monospace),
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}
