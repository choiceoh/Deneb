package ai.deneb.deneb

import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.components.rememberHaptics
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import kotlinx.coroutines.launch

/**
 * 조직도 — the group org chart editor (`miniapp.org.*`). The chart is the MASTER
 * source for the 파트별 업무 현황 dashboard: a node tagged with a lane becomes a
 * dashboard part, and its members / keywords / companies seed that part's
 * classification rules. So this screen is where the operator both *sees* the
 * structure (group → company → division → team, with each node's members) and
 * *edits* it — add/rename/delete nodes, manage members (name + 직급/직책 picker),
 * and tag nodes as dashboard parts.
 *
 * View model: the structure is an **indented list** — each node is a row indented by
 * its depth (group → company → division → team), with a per-node expand caret to fold
 * a branch. A search bar finds people by name across the whole tree (겸직 = a name in
 * several nodes is surfaced once per node) and highlights/expands the match.
 *
 * Contacts: the gateway enriches each member on GET with phones/emails name-matched
 * from the contacts store (read-only — never written back on save). Where members are
 * shown (the editor's member rows and the search-result chips), a matched member gets
 * call/email glyphs that dial/compose directly via the platform URI handler; unmatched
 * members show nothing extra.
 *
 * Editing model: the whole tree is one editable document. The shell holds the full
 * node list in state, all edits mutate that local list, and 저장 sends the whole
 * tree (`saveOrg`) which the gateway validates + persists wholesale. A discard guard
 * compares the working tree to the loaded baseline so a stray back can't lose edits.
 *
 * Design split (see docs/agent-rules/native-design-system.md): the frame + type are the
 * Deneb skin (DenebScreenScaffold + DenebType + indented rows + hairlines); the
 * controls (back, save button, search field, member pickers, bottom
 * sheet) are Material. The chart itself is a stateless body ([OrgChartContent]) the
 * render harness previews with mock data; this composable is the stateful shell
 * (fetch + edit + save).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebOrgChartScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
    onOpenPersonWiki: (String) -> Unit = {}, // open a searched member's 인물 wiki page
) {
    // The working tree (mutated by edits) and the baseline loaded from the gateway
    // (for the dirty check + the save target). null baseline = not loaded yet.
    // Disk/session snapshot paints instantly (view-only: edit affordances stay
    // gated on loadOk == true, so stale data can't be edited and saved back).
    var nodes by remember { mutableStateOf(client.sectionCaches.org.peek()?.nodes ?: emptyList()) }
    var baseline by remember { mutableStateOf<List<OrgNodeOut>?>(null) }
    // null = load in flight, true = loaded ok, false = fetch failed.
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    // The node being edited in the bottom sheet (its id), or null when closed.
    var editingId by remember { mutableStateOf<String?>(null) }
    // View-first: the chart opens read-only (tap a row to fold, no edit affordances).
    // 편집 in the header flips this on to reveal the +/pencil glyphs + add-root + tap-to-edit.
    var editMode by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun load(force: Boolean = false) {
        val fetched = client.fetchOrg(force)
        if (fetched == null) {
            loadOk = false
        } else {
            nodes = fetched.nodes
            baseline = fetched.nodes
            loadOk = true
        }
    }
    LaunchedEffect(Unit) { load() }

    val dirty = baseline != null && nodes != baseline
    val requestBack = rememberDiscardGuard(dirty, onBack)

    fun save() {
        notice = null
        error = null
        scope.launch {
            saving = true
            val err = client.saveOrg(nodes)
            saving = false
            if (err == null) {
                baseline = nodes // commit: the working tree is now the saved state
                notice = "저장됨"
            } else {
                error = err
            }
        }
    }

    DenebScreenScaffold(
        title = "조직도",
        onBack = requestBack,
        tabBar = navigationTabBar,
        actions = {
            // 편집/완료 flips edit mode. The chart defaults to a clean read-only view so
            // casually opening it never lands in an editable state.
            if (loadOk == true) {
                TextButton(onClick = { editMode = !editMode }) {
                    Text(if (editMode) "완료" else "편집")
                }
            }
            // Save is only meaningful with pending edits; a saving spinner reads as the
            // label going quiet. Kept in the scaffold header so it is reachable on both
            // phone and desktop without a floating button.
            if (dirty || saving) {
                TextButton(onClick = { if (!saving) save() }, enabled = !saving) {
                    Text(if (saving) "저장 중…" else "저장")
                }
            }
        },
    ) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                // A refresh discards uncommitted edits, so guard it: silently re-fetch
                // only when clean (a dirty refresh would surprise-drop edits).
                if (!dirty) {
                    haptics.refresh()
                    scope.launch {
                        refreshing = true
                        load(force = true)
                        refreshing = false
                    }
                }
            },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            when {
                loadOk == null && nodes.isEmpty() ->
                    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) { DenebLoading() }

                loadOk == false && nodes.isEmpty() ->
                    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                        DenebError(
                            "조직도를 불러오지 못했습니다.",
                            onRetry = {
                                scope.launch {
                                    loadOk = null
                                    load()
                                }
                            },
                        )
                    }

                nodes.isEmpty() ->
                    // An empty chart is a valid starting state (no org.json yet): guide
                    // the operator to seed the first (root) node instead of looking broken.
                    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                        DenebEmpty(
                            "아직 조직도가 없습니다.",
                            actionLabel = "최상위 조직 추가",
                            onAction = {
                                haptics.tap()
                                val node = newNode(parentId = "")
                                nodes = nodes + node
                                editingId = node.id
                            },
                        )
                    }

                else ->
                    // The chart owns its own scroll (the diagram pans both ways), so it is
                    // NOT wrapped in the outer verticalScroll the state cases use.
                    OrgChartContent(
                        nodes = nodes,
                        notice = notice,
                        error = error,
                        editMode = editMode,
                        onEditNode = { id ->
                            haptics.tap()
                            editingId = id
                        },
                        onAddChild = { parentId ->
                            haptics.tap()
                            val node = newNode(parentId = parentId)
                            nodes = nodes + node
                            editingId = node.id
                        },
                        onAddRoot = {
                            haptics.tap()
                            val node = newNode(parentId = "")
                            nodes = nodes + node
                            editingId = node.id
                        },
                        onOpenPersonWiki = onOpenPersonWiki,
                    )
            }
        }
    }

    // Node editor sheet — rename / type / lane / members / delete. Edits the working
    // list in place (replace the node by id, or drop it + its subtree on delete).
    val editing = editingId?.let { id -> nodes.firstOrNull { it.id == id } }
    if (editing != null) {
        val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
        ModalBottomSheet(
            onDismissRequest = { editingId = null },
            sheetState = sheetState,
        ) {
            OrgNodeEditor(
                node = editing,
                onChange = { updated ->
                    nodes = nodes.map { if (it.id == updated.id) updated else it }
                    notice = null
                },
                onDelete = {
                    haptics.reject()
                    nodes = removeSubtree(nodes, editing.id)
                    editingId = null
                    notice = null
                },
                onDone = {
                    scope.launch {
                        sheetState.hide()
                        editingId = null
                    }
                },
            )
        }
    }
}
