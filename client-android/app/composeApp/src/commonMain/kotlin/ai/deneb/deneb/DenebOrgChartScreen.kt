package ai.deneb.deneb

import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.ui.DenebOutlinedTextField
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.denebPressable
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Call
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.MailOutline
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlin.random.Random
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

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
 * Design split (see .claude/rules/native-design-system.md): the frame + type are the
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
) {
    // The working tree (mutated by edits) and the baseline loaded from the gateway
    // (for the dirty check + the save target). null baseline = not loaded yet.
    var nodes by remember { mutableStateOf<List<OrgNodeOut>>(emptyList()) }
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

    suspend fun load() {
        val fetched = client.fetchOrg()
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
                    scope.launch {
                        refreshing = true
                        load()
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
                    haptics.tap()
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

// --- domain: enums + helpers -------------------------------------------------

/** Node type tags (group|company|division|team) with Korean display labels. */
internal val orgTypes = listOf("group", "company", "division", "team")

/** Parse a comma-separated input into a trimmed, non-blank list — the editing
 *  form for a 파트 node's keywords / 거래처 classification lists. */
internal fun splitCsv(s: String): List<String> = s.split(",").map { it.trim() }.filter { it.isNotEmpty() }

internal fun orgTypeLabel(type: String): String = when (type) {
    "group" -> "그룹"
    "company" -> "회사"
    "division" -> "본부/실"
    "team" -> "팀"
    else -> type.ifBlank { "조직" }
}

/** 직급 (rank), top → bottom. Empty = unset. */
internal val orgRanks = listOf("회장", "사장", "부사장", "전무", "상무", "이사", "부장", "차장", "과장", "대리", "주임", "사원")

/** 직책 (position). 본부장/실장/팀장 are leader roles (mark a node's 부서장). */
internal val orgPositions = listOf("회장", "대표", "본부장", "실장", "팀장", "팀원")

/** Positions that make a member a node's leader (부서장), derived not stored. */
private val leaderPositions = setOf("본부장", "실장", "팀장")

/** A node's leader (부서장): the first member whose position is a leader role, else null. */
internal fun nodeLeader(node: OrgNodeOut): MemberOut? = node.members.firstOrNull { it.position in leaderPositions }

/** Build a fresh node with a unique id under [parentId]. New nodes default to 팀
 *  (the most common leaf the operator adds); type is editable in the sheet. The
 *  id carries a random suffix on top of the millisecond clock so two nodes added
 *  within the same millisecond can't collide — a duplicate id makes the gateway
 *  reject the whole save (Validate), which is opaque to fix. */
@OptIn(ExperimentalTime::class)
internal fun newNode(parentId: String): OrgNodeOut = OrgNodeOut(
    id = "n${Clock.System.now().toEpochMilliseconds()}-${Random.nextInt(0, 1_000_000)}",
    name = "",
    type = if (parentId.isEmpty()) "company" else "team",
    parentId = parentId,
)

/**
 * Remove a node and its whole subtree from the flat list (deleting a parent must not
 * orphan children — the gateway would reject the dangling parentId). Walks the
 * parentId graph collecting descendants, then filters them all out.
 */
internal fun removeSubtree(nodes: List<OrgNodeOut>, id: String): List<OrgNodeOut> {
    val doomed = mutableSetOf(id)
    var changed = true
    while (changed) {
        changed = false
        for (n in nodes) {
            if (n.parentId in doomed && n.id !in doomed) {
                doomed.add(n.id)
                changed = true
            }
        }
    }
    return nodes.filterNot { it.id in doomed }
}

// --- people search -----------------------------------------------------------

/** One hit of a people search: the node carrying a matching member + the member. */
internal data class OrgSearchHit(val node: OrgNodeOut, val member: MemberOut)

/**
 * Find every member whose name contains [query] (case/space-insensitive), across the
 * whole tree. A 겸직 (the same name in several nodes) yields one hit per node, so the
 * caller can show the count and let the operator pick which node to jump to. Blank
 * query = no hits (search is idle, not "everyone").
 */
internal fun searchMembers(nodes: List<OrgNodeOut>, query: String): List<OrgSearchHit> {
    val needle = query.trim().replace(" ", "")
    if (needle.isEmpty()) return emptyList()
    val hits = mutableListOf<OrgSearchHit>()
    for (node in nodes) {
        for (m in node.members) {
            if (m.name.replace(" ", "").contains(needle, ignoreCase = true)) {
                hits.add(OrgSearchHit(node, m))
            }
        }
    }
    return hits
}

// --- chart geometry ----------------------------------------------------------

/** Indent applied per hierarchy depth. The list reads as a 그룹 → 회사 → 본부/실 → 팀 tree. */
private val OrgIndentStep: Dp = 16.dp

// --- stateless body (previewable) -------------------------------------------

/**
 * The org chart as an indented list + people search. Each node is a row indented by its
 * depth so the 그룹 → 회사 → 본부/실 → 팀 hierarchy reads top-down; the row shows the name, a
 * type badge, a lane (파트) chip if tagged, and a member-count line. Tapping a row opens its
 * editor; a per-node ＋ adds a child; a leading caret folds its subtree. The list scrolls
 * vertically. The search bar finds people by name and highlights / expands the matching
 * node(s). Roots (empty parentId) are the top level. Pure presentation — the shell owns the
 * tree + edits.
 */
@Composable
internal fun OrgChartContent(
    nodes: List<OrgNodeOut>,
    notice: String?,
    error: String?,
    onEditNode: (String) -> Unit,
    onAddChild: (String) -> Unit,
    onAddRoot: () -> Unit,
    editMode: Boolean = false, // false = read-only view (default); true reveals edit affordances
    initialQuery: String = "", // seeds the search box (for the render harness; "" at runtime)
) {
    // Group children by parent once so render is O(n) not O(n^2).
    val childrenOf = remember(nodes) { nodes.groupBy { it.parentId } }
    // Collapse state per node id; default expanded so the whole chart reads at a glance
    // (a hand-maintained chart is small). Survives edits via the id key.
    var collapsed by remember { mutableStateOf(setOf<String>()) }

    // People search. A non-blank query computes hits; the set of hit node ids drives
    // box highlighting, and expanding the ancestors of a hit makes it visible even when
    // its branch was folded.
    var query by remember { mutableStateOf(initialQuery) }
    val hits = remember(nodes, query) { searchMembers(nodes, query) }
    val hitNodeIds = remember(hits) { hits.map { it.node.id }.toSet() }

    // Jump-to-node request from a search result: clear the collapse on every ancestor so
    // the target is rendered. (We can't auto-scroll without measured coords; expanding +
    // highlighting is the reliable, layout-free affordance.)
    fun revealNode(node: OrgNodeOut) {
        val parentById = nodes.associateBy({ it.id }, { it.parentId })
        val toOpen = mutableSetOf<String>()
        var pid = node.parentId
        var guard = 0
        while (pid.isNotEmpty() && guard < nodes.size + 1) {
            toOpen.add(pid)
            pid = parentById[pid] ?: ""
            guard++
        }
        collapsed = collapsed - toOpen
    }

    Column(Modifier.fillMaxSize()) {
        // Search bar — finds people by name. A trailing clear (×) appears once typed.
        OrgSearchBar(
            query = query,
            onQueryChange = { query = it },
            hitCount = hits.size,
        )
        // Search results strip: each matching member as a tappable chip ("이름 · 노드").
        // Tapping reveals (expands ancestors of) that node. 겸직 shows once per node.
        if (query.isNotBlank()) {
            OrgSearchResults(hits = hits, onPick = { hit -> revealNode(hit.node) })
        }

        // The chart as an indented list: each node is a row indented by its depth, so the
        // 그룹 → 회사 → 본부/실 → 팀 hierarchy reads top-down. Vertical scroll only (no panning);
        // a folded branch hides its descendants but advertises the hidden count.
        Box(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState()),
        ) {
            Column(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                val roots = childrenOf[""].orEmpty()
                roots.forEach { root ->
                    OrgListRows(
                        node = root,
                        depth = 0,
                        childrenOf = childrenOf,
                        collapsed = collapsed,
                        editMode = editMode,
                        onToggle = { id ->
                            collapsed = if (id in collapsed) collapsed - id else collapsed + id
                        },
                        onEditNode = onEditNode,
                        onAddChild = onAddChild,
                        hitNodeIds = hitNodeIds,
                    )
                }
            }
        }

        // Add another root node — edit mode only (a clean view has no add affordances).
        if (editMode) {
            Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp)) {
                OutlinedButton(onClick = onAddRoot) {
                    Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("최상위 조직 추가")
                }
            }
        }
        // Save feedback toast-line: shown under the chart (a Snackbar would float over the
        // bottom bar). Cleared on the next edit/save.
        if (notice != null) {
            Text(
                notice,
                style = DenebType.meta,
                color = denebInsight(),
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
        if (error != null) {
            Text(
                error,
                style = DenebType.rowSubtitle,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
        Spacer(Modifier.height(8.dp))
    }
}

/**
 * One node and its subtree as indented rows: the node's own row, then — when expanded and it
 * has children — each child subtree one indent level deeper. Recursion handles arbitrary
 * depth; a collapsed node prunes its branch from the list.
 */
@Composable
private fun OrgListRows(
    node: OrgNodeOut,
    depth: Int,
    childrenOf: Map<String, List<OrgNodeOut>>,
    collapsed: Set<String>,
    editMode: Boolean,
    onToggle: (String) -> Unit,
    onEditNode: (String) -> Unit,
    onAddChild: (String) -> Unit,
    hitNodeIds: Set<String>,
) {
    val kids = childrenOf[node.id].orEmpty()
    val isCollapsed = node.id in collapsed
    OrgNodeRow(
        node = node,
        depth = depth,
        childCount = kids.size,
        isCollapsed = isCollapsed,
        highlighted = node.id in hitNodeIds,
        editMode = editMode,
        onToggle = { onToggle(node.id) },
        onEdit = { onEditNode(node.id) },
        onAddChild = { onAddChild(node.id) },
    )
    if (kids.isNotEmpty() && !isCollapsed) {
        kids.forEach { kid ->
            OrgListRows(
                node = kid,
                depth = depth + 1,
                childrenOf = childrenOf,
                collapsed = collapsed,
                editMode = editMode,
                onToggle = onToggle,
                onEditNode = onEditNode,
                onAddChild = onAddChild,
                hitNodeIds = hitNodeIds,
            )
        }
    }
}

/**
 * A single node as an indented list row: a leading caret (or an aligning spacer) that folds
 * the subtree, then the name, a type badge, an optional 파트 chip, and a member-count line;
 * the add-child + edit glyphs appear in edit mode only. A row tap folds/expands in view mode or
 * opens the editor in edit mode (so just browsing never edits); a hairline underlines it. A
 * search hit tints the row with the cool interactive accent.
 */
@Composable
private fun OrgNodeRow(
    node: OrgNodeOut,
    depth: Int,
    childCount: Int,
    isCollapsed: Boolean,
    highlighted: Boolean,
    editMode: Boolean,
    onToggle: () -> Unit,
    onEdit: () -> Unit,
    onAddChild: () -> Unit,
) {
    val accent = MaterialTheme.colorScheme.primary
    val rowBg = if (highlighted) Modifier.background(accent.copy(alpha = 0.10f)) else Modifier
    val indent = 12.dp + OrgIndentStep * depth
    // View mode: a row tap folds/expands (childless rows do nothing) — it never edits. Edit
    // mode: a row tap opens the editor.
    val rowTap = when {
        editMode -> Modifier.denebPressable(onClick = onEdit).handCursor()
        childCount > 0 -> Modifier.denebPressable(onClick = onToggle).handCursor()
        else -> Modifier
    }
    Column(
        Modifier
            .fillMaxWidth()
            .then(rowBg)
            .then(rowTap),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = indent, end = 6.dp, top = 8.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // Leading caret folds the subtree; childless rows get an aligning spacer.
            if (childCount > 0) {
                Icon(
                    imageVector = if (isCollapsed) Icons.AutoMirrored.Filled.KeyboardArrowRight else Icons.Filled.KeyboardArrowDown,
                    contentDescription = if (isCollapsed) "펼치기" else "접기",
                    tint = denebHint(),
                    modifier = Modifier
                        .size(20.dp)
                        .denebPressable(onClick = onToggle)
                        .handCursor(),
                )
            } else {
                Spacer(Modifier.width(20.dp))
            }
            Spacer(Modifier.width(6.dp))
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = node.name.ifBlank { "(이름 없음)" },
                        style = DenebType.rowTitleStrong,
                        color = if (node.name.isBlank()) denebHint() else MaterialTheme.colorScheme.onBackground,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Spacer(Modifier.width(8.dp))
                    OrgTypeBadge(node.type)
                    if (node.lane.isNotBlank()) {
                        Spacer(Modifier.width(6.dp))
                        OrgLaneChip()
                    }
                    if (isCollapsed && childCount > 0) {
                        Spacer(Modifier.width(6.dp))
                        Text("하위 $childCount", style = DenebType.sectionLabel, color = denebHint())
                    }
                }
                val summary = nodeMemberSummary(node)
                if (summary.isNotEmpty()) {
                    Text(
                        text = summary,
                        style = DenebType.snippet,
                        color = denebHint(),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
            }
            // Edit affordances — edit mode only; a read-only view stays clean.
            if (editMode) {
                IconButton(onClick = onAddChild, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.Outlined.Add, contentDescription = "하위 조직 추가", tint = denebHint(), modifier = Modifier.size(16.dp))
                }
                IconButton(onClick = onEdit, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.Outlined.Edit, contentDescription = "편집", tint = accent, modifier = Modifier.size(16.dp))
                }
            }
        }
        // Row hairline, indented to align with the row content.
        Box(
            Modifier
                .fillMaxWidth()
                .padding(start = indent)
                .height(1.dp)
                .background(denebHairline()),
        )
    }
}

// --- small node-box parts ----------------------------------------------------

/** Type badge — a small tracked-caps label (그룹/회사/본부·실/팀) in hint color. */
@Composable
private fun OrgTypeBadge(type: String) {
    Text(
        text = orgTypeLabel(type),
        style = DenebType.sectionLabel,
        color = denebHint(),
    )
}

/** Lane chip — the warm-apricot 파트 tag marking a node as a dashboard part. The
 *  lane *key* is an internal id, so the chip shows a fixed "파트" label (the part's
 *  column title is the node name). */
@Composable
private fun OrgLaneChip() {
    Box(
        Modifier
            .background(denebInsightContainer(), RoundedCornerShape(6.dp))
            .padding(horizontal = 6.dp, vertical = 1.dp),
    ) {
        Text("파트", style = DenebType.sectionLabel, color = denebInsight())
    }
}

/** Box member-count line: leader (부서장) + count, e.g. "김철수 외 1명" / "3명" / "".
 *  Blank for a bare node (the box just shows its name + type). Keyword/company counts
 *  stay in the editor so the box stays scannable. */
internal fun nodeMemberSummary(node: OrgNodeOut): String {
    val leader = nodeLeader(node)
    return when {
        leader != null && node.members.size > 1 -> "${leader.name} 외 ${node.members.size - 1}명"
        leader != null -> leader.name
        node.members.size == 1 -> node.members.first().name
        node.members.size > 1 -> "${node.members.size}명"
        else -> ""
    }
}

// --- people search UI --------------------------------------------------------

/** The search bar above the chart — a Material text field (Deneb-skinned) with a
 *  leading magnifier, a trailing clear (×) once typed, and a hit-count suffix. */
@Composable
private fun OrgSearchBar(
    query: String,
    onQueryChange: (String) -> Unit,
    hitCount: Int,
) {
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 4.dp)) {
        DenebOutlinedTextField(
            value = query,
            onValueChange = onQueryChange,
            placeholder = { Text("이름으로 사람 찾기") },
            singleLine = true,
            trailingIcon = {
                if (query.isNotBlank()) {
                    IconButton(onClick = { onQueryChange("") }) {
                        Icon(Icons.Outlined.Close, contentDescription = "지우기", tint = denebHint(), modifier = Modifier.size(18.dp))
                    }
                } else {
                    Icon(Icons.Outlined.Search, contentDescription = null, tint = denebHint(), modifier = Modifier.size(18.dp))
                }
            },
            modifier = Modifier.fillMaxWidth(),
        )
        if (query.isNotBlank()) {
            Text(
                text = if (hitCount == 0) "일치하는 사람이 없습니다." else "$hitCount 곳에서 찾음",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(top = 4.dp),
            )
        }
    }
}

/**
 * Search-results strip: each matching member rendered as a tappable chip ("이름 · 노드
 * 이름") with inline call/email shortcuts when the gateway enriched that member with
 * contact info. A 겸직 (same name in several nodes) yields one chip per node, so the
 * operator picks which posting to jump to. Tapping the chip reveals (expands ancestors
 * of) that node and highlights it; the phone/mail glyphs dial/compose directly.
 * Horizontally scrollable so many hits don't wrap into a wall.
 */
@Composable
private fun OrgSearchResults(
    hits: List<OrgSearchHit>,
    onPick: (OrgSearchHit) -> Unit,
) {
    if (hits.isEmpty()) return
    Row(
        Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        hits.forEach { hit ->
            // The chip (label → reveals the node) and the contact glyphs are SIBLINGS in
            // this row, not nested: a glyph tap must fire tel:/mailto: only, never also the
            // chip's reveal. Keeping them adjacent (not inside the clickable chip) gives each
            // its own distinct, non-overlapping tap target.
            Row(verticalAlignment = Alignment.CenterVertically) {
                DenebChip(onClick = { onPick(hit) }) {
                    val rank = hit.member.rank.ifBlank { "" }
                    val label = buildString {
                        append(hit.member.name.ifBlank { "(이름 없음)" })
                        if (rank.isNotBlank()) {
                            append(" ")
                            append(rank)
                        }
                        append(" · ")
                        append(hit.node.name.ifBlank { "(이름 없음)" })
                    }
                    Text(label, style = DenebType.rowSubtitle, color = MaterialTheme.colorScheme.onBackground, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                // Inline call/email shortcuts when the gateway enriched this member with
                // contact info — search is people-centric, so let the operator reach the
                // person straight from the result (no need to open the editor first).
                OrgContactActions(member = hit.member, glyphSize = 18.dp, leadingGap = 2.dp)
            }
        }
    }
}

// --- member contact actions --------------------------------------------------

/**
 * Call/email shortcuts for a member, shown only when the gateway enriched them with
 * contact info (phones/emails — read-only, name-matched against the contacts store;
 * see the gateway's MemberOut). A member with neither renders nothing, so unmatched
 * people stay clean. The first phone / first email get a glyph each; tapping fires the
 * platform's dialer (`tel:`) or mail composer (`mailto:`) via [LocalUriHandler] — the
 * same common-safe URI path the mail/files screens use (no new expect/actual). When a
 * person has several numbers/addresses we wire the first (the contacts store lists the
 * primary first); the rest live in the 사람 detail screen.
 *
 * Design (see .claude/rules/native-design-system.md): these are *functional* icons
 * (phone/mail) — allowed; placed as small, restrained glyph buttons, not decoration.
 */
@Composable
internal fun OrgContactActions(
    member: MemberOut,
    glyphSize: Dp,
    leadingGap: Dp,
) {
    val phone = member.phones.firstOrNull { it.isNotBlank() }
    val email = member.emails.firstOrNull { it.isNotBlank() }
    if (phone == null && email == null) return

    val uriHandler = LocalUriHandler.current
    val accent = MaterialTheme.colorScheme.primary
    val buttonSize = glyphSize + 14.dp // glyph + touch padding (keeps a ~comfortable target)

    Spacer(Modifier.width(leadingGap))
    if (phone != null) {
        IconButton(
            onClick = { uriHandler.openUri("tel:${phone.trim()}") },
            modifier = Modifier.size(buttonSize),
        ) {
            Icon(Icons.Filled.Call, contentDescription = "전화 $phone", tint = accent, modifier = Modifier.size(glyphSize))
        }
    }
    if (email != null) {
        IconButton(
            onClick = { uriHandler.openUri("mailto:${email.trim()}") },
            modifier = Modifier.size(buttonSize),
        ) {
            Icon(Icons.Outlined.MailOutline, contentDescription = "메일 $email", tint = accent, modifier = Modifier.size(glyphSize))
        }
    }
}

// --- node editor (bottom sheet body) ----------------------------------------

/**
 * The node editor: rename + type picker + dashboard-part (lane) toggle + member CRUD
 * (each with 직급/직책 dropdowns, plus read-only call/email shortcuts when the member is
 * matched in the contacts store) + delete. Every editable control writes back through
 * [onChange] with the updated node, so the parent's working tree stays the single
 * source of truth (no local mirror to desync); the contact shortcuts are display-only
 * (numbers live in the contacts store, never in org.json). Stateless over its node —
 * previewable.
 */
@Composable
internal fun OrgNodeEditor(
    node: OrgNodeOut,
    onChange: (OrgNodeOut) -> Unit,
    onDelete: () -> Unit,
    onDone: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(start = 20.dp, end = 20.dp, bottom = 24.dp),
    ) {
        Text("조직 편집", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
        Spacer(Modifier.height(12.dp))

        // Name.
        OrgFieldLabel("이름")
        DenebOutlinedTextField(
            value = node.name,
            onValueChange = { onChange(node.copy(name = it)) },
            placeholder = { Text("예: 기획조정실 1팀") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(14.dp))

        // Type picker.
        OrgFieldLabel("종류")
        OrgEnumDropdown(
            value = orgTypeLabel(node.type),
            options = orgTypes,
            optionLabel = ::orgTypeLabel,
            placeholder = "종류 선택",
            onSelect = { onChange(node.copy(type = it)) },
        )
        Spacer(Modifier.height(14.dp))

        // Dashboard-part (lane) toggle. A tagged node becomes a 파트별 업무 현황 column.
        // The lane *key* is an internal id (we seed it from the node id); the operator
        // only chooses on/off, so no raw key is ever shown.
        //
        // Toggling off clears the lane to ""; toggling back on must restore the SAME
        // key it had, not re-seed from the node id — otherwise a hand-edited
        // meaningful key (e.g. a chart authored off-app with lane "sales") is lost on
        // an off→on round-trip. Remember the last non-blank lane (per node id) and
        // prefer it when re-enabling, falling back to the node id only when there was
        // never a prior key.
        var lastLane by remember(node.id) { mutableStateOf(node.lane) }
        if (node.lane.isNotBlank()) lastLane = node.lane
        OrgPartToggle(
            on = node.lane.isNotBlank(),
            onToggle = { on ->
                onChange(node.copy(lane = if (on) lastLane.ifBlank { node.id } else ""))
            },
        )
        Spacer(Modifier.height(18.dp))

        // Classification rules — shown only for a 파트 node. Keywords (도메인 용어) and
        // 거래처 (counterparty names) route work items to this part's dashboard column;
        // member names are the strong signal, these are weak/medium. Edited as
        // comma-separated lists so the operator never sees a raw key or array syntax.
        if (node.lane.isNotBlank()) {
            OrgFieldLabel("분류 키워드 (쉼표로 구분)")
            DenebOutlinedTextField(
                value = node.keywords.joinToString(", "),
                onValueChange = { onChange(node.copy(keywords = splitCsv(it))) },
                placeholder = { Text("예: 태양광, 모듈, 인버터") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))
            OrgFieldLabel("분류 거래처 (쉼표로 구분)")
            DenebOutlinedTextField(
                value = node.companies.joinToString(", "),
                onValueChange = { onChange(node.copy(companies = splitCsv(it))) },
                placeholder = { Text("예: 트리나솔라, 한화") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(18.dp))
        }

        // Members.
        OrgFieldLabel("구성원")
        if (node.members.isEmpty()) {
            Text("아직 구성원이 없습니다.", style = DenebType.rowSubtitle, color = denebHint(), modifier = Modifier.padding(vertical = 4.dp))
        }
        node.members.forEachIndexed { idx, member ->
            OrgMemberEditor(
                member = member,
                onChange = { updated ->
                    onChange(node.copy(members = node.members.toMutableList().also { it[idx] = updated }))
                },
                onRemove = {
                    onChange(node.copy(members = node.members.toMutableList().also { it.removeAt(idx) }))
                },
            )
            Spacer(Modifier.height(8.dp))
        }
        OutlinedButton(
            onClick = { onChange(node.copy(members = node.members + MemberOut(name = ""))) },
            modifier = Modifier.fillMaxWidth(),
        ) {
            Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(6.dp))
            Text("구성원 추가")
        }
        Spacer(Modifier.height(22.dp))

        // Actions: delete (left) + done (right). Delete drops the node and its subtree
        // from the working tree (parent's onDelete); 저장 is the screen-level header
        // button — done just closes the sheet so the edits stay in the working tree.
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onDelete) {
                Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("조직 삭제", color = MaterialTheme.colorScheme.error)
            }
            FilledTonalButton(onClick = onDone) { Text("완료") }
        }
    }
}

/** One member's editable row: name field + 직급 dropdown + 직책 dropdown + remove. */
@Composable
private fun OrgMemberEditor(
    member: MemberOut,
    onChange: (MemberOut) -> Unit,
    onRemove: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            DenebOutlinedTextField(
                value = member.name,
                onValueChange = { onChange(member.copy(name = it)) },
                placeholder = { Text("이름") },
                singleLine = true,
                modifier = Modifier.weight(1f),
            )
            IconButton(onClick = onRemove, modifier = Modifier.size(40.dp)) {
                Icon(Icons.Outlined.Delete, contentDescription = "구성원 삭제", tint = denebHint(), modifier = Modifier.size(18.dp))
            }
        }
        Spacer(Modifier.height(6.dp))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Box(Modifier.weight(1f)) {
                OrgEnumDropdown(
                    value = member.rank,
                    options = orgRanks,
                    optionLabel = { it },
                    placeholder = "직급",
                    allowClear = true,
                    onSelect = { onChange(member.copy(rank = it)) },
                )
            }
            Box(Modifier.weight(1f)) {
                OrgEnumDropdown(
                    value = member.position,
                    options = orgPositions,
                    optionLabel = { it },
                    placeholder = "직책",
                    allowClear = true,
                    onSelect = { onChange(member.copy(position = it)) },
                )
            }
        }
        // Contact row: call/email shortcuts from the gateway's read-only enrichment.
        // Only renders for members the contacts store matched (phones/emails present),
        // so the editor stays uncluttered for the rest. Labelled so its purpose is clear
        // next to the editable name/직급/직책 fields above (which never store numbers).
        if (member.phones.any { it.isNotBlank() } || member.emails.any { it.isNotBlank() }) {
            Spacer(Modifier.height(6.dp))
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text("연락처", style = DenebType.sectionLabel, color = denebHint())
                OrgContactActions(member = member, glyphSize = 18.dp, leadingGap = 6.dp)
            }
        }
    }
}

/** A small tracked-caps field label above an editor control. */
@Composable
private fun OrgFieldLabel(text: String) {
    Text(text, style = DenebType.sectionLabel, color = denebHint(), modifier = Modifier.padding(bottom = 6.dp))
}

/**
 * A friendly enum picker (Material ExposedDropdownMenuBox) — never exposes the raw
 * value. Shows [value] (already a display label or a plain enum string), lists
 * [options] rendered through [optionLabel], and reports the chosen *raw* option via
 * [onSelect]. With [allowClear] a "(없음)" item clears to empty (for optional fields
 * like 직급/직책).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun OrgEnumDropdown(
    value: String,
    options: List<String>,
    optionLabel: (String) -> String,
    placeholder: String,
    onSelect: (String) -> Unit,
    allowClear: Boolean = false,
) {
    var expanded by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { expanded = it },
    ) {
        DenebOutlinedTextField(
            value = value,
            onValueChange = {},
            readOnly = true,
            placeholder = { Text(placeholder) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable),
        )
        ExposedDropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            if (allowClear) {
                DropdownMenuItem(
                    text = { Text("(없음)", color = denebHint()) },
                    onClick = {
                        onSelect("")
                        expanded = false
                    },
                )
            }
            options.forEach { opt ->
                DropdownMenuItem(
                    text = { Text(optionLabel(opt)) },
                    onClick = {
                        onSelect(opt)
                        expanded = false
                    },
                )
            }
        }
    }
}

/** Dashboard-part toggle row — a chip the operator taps to mark this node a 파트별
 *  업무 현황 column. Selected = warm insight accent (the dashboard's color). */
@Composable
private fun OrgPartToggle(on: Boolean, onToggle: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text("대시보드 파트", style = DenebType.rowTitleStrong, color = MaterialTheme.colorScheme.onBackground)
            Text(
                "켜면 ‘파트별 업무 현황’에 이 조직의 칸이 생깁니다.",
                style = DenebType.snippet,
                color = denebHint(),
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        Spacer(Modifier.width(10.dp))
        DenebChip(selected = on, onClick = { onToggle(!on) }) {
            Text(if (on) "파트로 사용 중" else "파트 아님")
        }
    }
}
