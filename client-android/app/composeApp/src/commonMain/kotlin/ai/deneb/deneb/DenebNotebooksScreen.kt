package ai.deneb.deneb

import ai.deneb.deneb.generated.NotebookOut
import ai.deneb.deneb.generated.NotebookSummaryOut
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Notebook viewer (`miniapp.notebook.*`) — deal-anchored source collections.
 * A single screen with two states: the list of notebooks, and (on tap) one
 * notebook's pinned sources with their citation tags. Read-only; pinning and
 * brief synthesis live in the chat path.
 */
@Composable
fun DenebNotebooksScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var notebooks by remember { mutableStateOf<List<NotebookSummaryOut>?>(null) }
    var listFailed by remember { mutableStateOf(false) }
    var openId by remember { mutableStateOf<String?>(null) }
    var detail by remember { mutableStateOf<NotebookOut?>(null) }
    var detailFailed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun loadList() {
        listFailed = false
        notebooks = null
        val n = client.fetchNotebooks()
        notebooks = n
        listFailed = n == null
    }

    suspend fun loadDetail(id: String) {
        openId = id
        detail = null
        detailFailed = false
        val nb = client.fetchNotebook(id)
        detail = nb
        detailFailed = nb == null
    }

    LaunchedEffect(Unit) { loadList() }

    val current = openId
    if (current != null) {
        val nb = detail
        DenebScreenScaffold(
            title = "노트북",
            onBack = {
                openId = null
                detail = null
            },
            tabBar = navigationTabBar,
        ) {
            Column(
                Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 24.dp, vertical = 8.dp),
            ) {
                if (nb == null) {
                    if (detailFailed) {
                        DenebError("노트북을 불러오지 못했습니다.", onRetry = { scope.launch { loadDetail(current) } })
                    } else {
                        DenebLoading()
                    }
                    return@Column
                }
                Text(nb.name, style = DenebType.subject)
                if (nb.description.isNotBlank()) {
                    Text(nb.description, style = DenebType.rowSubtitle)
                }
                DenebSectionLabel("자료 ${nb.sources.size}건")
                if (nb.sources.isEmpty()) {
                    DenebEmpty("아직 핀된 자료가 없습니다.")
                } else {
                    nb.sources.forEach { src ->
                        val label = src.title.ifBlank { src.ref.ifBlank { src.kind } }
                        DenebRow {
                            Text("[${src.cite}] $label", style = DenebType.rowTitle)
                            if (src.text.isNotBlank()) {
                                Text(src.text, style = DenebType.snippet)
                            }
                        }
                    }
                }
            }
        }
        return
    }

    DenebScreenScaffold(title = "노트북", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 8.dp),
        ) {
            val list = notebooks
            if (list == null) {
                if (listFailed) {
                    DenebError("노트북을 불러오지 못했습니다.", onRetry = { scope.launch { loadList() } })
                } else {
                    DenebLoading()
                }
                return@Column
            }
            if (list.isEmpty()) {
                DenebEmpty("노트북이 없습니다. 딜 메일이 분석되면 자동으로 생기고, 채팅에서도 만들 수 있습니다.")
                return@Column
            }
            list.forEach { item ->
                val sub = if (item.dealRef.isNotBlank()) {
                    "자료 ${item.sourceCount}건 · ${item.dealRef}"
                } else {
                    "자료 ${item.sourceCount}건"
                }
                DenebRow(onClick = { scope.launch { loadDetail(item.id) } }) {
                    Text(item.name, style = DenebType.rowTitle)
                    Text(sub, style = DenebType.rowSubtitle)
                }
            }
        }
    }
}
