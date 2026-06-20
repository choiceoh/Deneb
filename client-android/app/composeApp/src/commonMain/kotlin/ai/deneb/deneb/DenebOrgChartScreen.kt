package ai.deneb.deneb

import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebOutlinedTextField
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import androidx.compose.foundation.background
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
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
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
 * Editing model: the whole tree is one editable document. The shell holds the full
 * node list in state, all edits mutate that local list, and 저장 sends the whole
 * tree (`saveOrg`) which the gateway validates + persists wholesale. A discard guard
 * compares the working tree to the loaded baseline so a stray back can't lose edits.
 *
 * Design split (see .claude/rules/native-design-system.md): the frame + type are the
 * Deneb skin (DenebScreenScaffold + DenebType + grouped DenebGroup card + hairlines);
 * the controls (back, save button, member pickers, bottom sheet) are Material. The
 * tree itself is a stateless body ([OrgTreeContent]) the render harness previews with
 * mock data; this composable is the stateful shell (fetch + edit + save).
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
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                when {
                    loadOk == null && nodes.isEmpty() -> DenebLoading()

                    loadOk == false && nodes.isEmpty() -> DenebError(
                        "조직도를 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    nodes.isEmpty() -> {
                        // An empty chart is a valid starting state (no org.json yet): guide
                        // the operator to seed the first (root) node instead of looking broken.
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

                    else -> {
                        OrgTreeContent(
                            nodes = nodes,
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
                        )
                        Spacer(Modifier.height(12.dp))
                        // Add another root node (a second company/group under no parent).
                        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                            OutlinedButton(onClick = {
                                haptics.tap()
                                val node = newNode(parentId = "")
                                nodes = nodes + node
                                editingId = node.id
                            }) {
                                Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                                Spacer(Modifier.width(6.dp))
                                Text("최상위 조직 추가")
                            }
                        }
                    }
                }
                // Save feedback toast-line: shown under the tree (a Snackbar would float
                // over the bottom bar). Cleared on the next edit/save.
                if (notice != null) {
                    Text(
                        notice!!,
                        style = DenebType.meta,
                        color = denebInsight(),
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
                    )
                }
                if (error != null) {
                    Text(
                        error!!,
                        style = DenebType.rowSubtitle,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
                    )
                }
                Spacer(Modifier.height(24.dp))
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
 *  (the most common leaf the operator adds); type is editable in the sheet. */
@OptIn(ExperimentalTime::class)
internal fun newNode(parentId: String): OrgNodeOut = OrgNodeOut(
    id = "n${Clock.System.now().toEpochMilliseconds()}",
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

// --- stateless body (previewable) -------------------------------------------

/**
 * The org chart tree: nodes laid out as an indented expand/collapse hierarchy joined
 * by parentId. Each node renders its name, a type badge, a lane (대시보드 파트) chip
 * if tagged, its leader + member count, and — when expanded — its member list. Tapping
 * a node opens its editor; a per-node ＋ adds a child. Roots (empty parentId) are the
 * top level. Pure presentation — the shell owns the tree state + edits.
 */
@Composable
internal fun OrgTreeContent(
    nodes: List<OrgNodeOut>,
    onEditNode: (String) -> Unit,
    onAddChild: (String) -> Unit,
) {
    // Group children by parent once so render is O(n) not O(n^2).
    val childrenOf = remember(nodes) { nodes.groupBy { it.parentId } }
    // Expand/collapse state per node id; default expanded so the whole chart reads at
    // a glance (a hand-maintained chart is small). Survives edits via the id key.
    val collapsed = remember { mutableStateOf(setOf<String>()) }
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        DenebGroup {
            val roots = childrenOf[""].orEmpty()
            roots.forEachIndexed { i, root ->
                OrgNodeRows(
                    node = root,
                    depth = 0,
                    childrenOf = childrenOf,
                    collapsed = collapsed.value,
                    onToggle = { id ->
                        collapsed.value = if (id in collapsed.value) collapsed.value - id else collapsed.value + id
                    },
                    onEditNode = onEditNode,
                    onAddChild = onAddChild,
                    isLastRoot = i == roots.lastIndex,
                )
            }
        }
    }
}

/**
 * One node row plus (recursively) its descendants. Indentation encodes depth; an
 * expand caret shows only when the node has children. The row body is a single
 * hairline row inside the group card — leading caret/spacer, name + type badge, then
 * a trailing ＋ (add child) and ✎ (edit). Expanded nodes list their members beneath.
 */
@Composable
private fun OrgNodeRows(
    node: OrgNodeOut,
    depth: Int,
    childrenOf: Map<String, List<OrgNodeOut>>,
    collapsed: Set<String>,
    onToggle: (String) -> Unit,
    onEditNode: (String) -> Unit,
    onAddChild: (String) -> Unit,
    isLastRoot: Boolean,
) {
    val kids = childrenOf[node.id].orEmpty()
    val isCollapsed = node.id in collapsed
    val hairline = denebHairline()
    val indent = (depth * 16).dp

    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 16.dp + indent, end = 8.dp, top = 12.dp, bottom = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // Expand caret (only when there are children); otherwise a small spacer so
            // names still align.
            if (kids.isNotEmpty()) {
                IconButton(onClick = { onToggle(node.id) }, modifier = Modifier.size(28.dp)) {
                    Icon(
                        imageVector = if (isCollapsed) Icons.AutoMirrored.Filled.KeyboardArrowRight else Icons.Filled.KeyboardArrowDown,
                        contentDescription = if (isCollapsed) "펼치기" else "접기",
                        tint = denebHint(),
                        modifier = Modifier.size(20.dp),
                    )
                }
                Spacer(Modifier.width(2.dp))
            } else {
                Spacer(Modifier.width(30.dp))
            }
            // Name + type badge + optional lane chip + leader/member summary.
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = node.name.ifBlank { "(이름 없음)" },
                        style = DenebType.rowTitleStrong,
                        color = if (node.name.isBlank()) denebHint() else MaterialTheme.colorScheme.onBackground,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                    Spacer(Modifier.width(8.dp))
                    OrgTypeBadge(node.type)
                    if (node.lane.isNotBlank()) {
                        Spacer(Modifier.width(6.dp))
                        OrgLaneChip()
                    }
                }
                // When the node is expanded its members list right below, so the
                // summary drops the people part (it'd just repeat) and keeps only the
                // keyword/company counts. Collapsed, it shows the full glance.
                val summary = nodeSummary(node, membersShown = !isCollapsed && node.members.isNotEmpty())
                if (summary.isNotEmpty()) {
                    Text(
                        text = summary,
                        style = DenebType.snippet,
                        color = denebHint(),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 3.dp),
                    )
                }
            }
            // Add-child + edit affordances.
            IconButton(onClick = { onAddChild(node.id) }, modifier = Modifier.size(34.dp)) {
                Icon(Icons.Outlined.Add, contentDescription = "하위 조직 추가", tint = denebHint(), modifier = Modifier.size(18.dp))
            }
            IconButton(onClick = { onEditNode(node.id) }, modifier = Modifier.size(34.dp)) {
                Icon(Icons.Outlined.Edit, contentDescription = "편집", tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
            }
        }

        // Member lines under an expanded node (indented past the name column).
        if (!isCollapsed && node.members.isNotEmpty()) {
            Column(Modifier.fillMaxWidth().padding(start = 16.dp + indent + 30.dp, end = 16.dp, bottom = 10.dp)) {
                node.members.forEach { m ->
                    Text(
                        text = memberLine(m),
                        style = DenebType.rowSubtitle,
                        color = denebHint(),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(vertical = 1.dp),
                    )
                }
            }
        }

        // Children, recursively, when expanded.
        if (!isCollapsed) {
            kids.forEach { kid ->
                OrgNodeRows(
                    node = kid,
                    depth = depth + 1,
                    childrenOf = childrenOf,
                    collapsed = collapsed,
                    onToggle = onToggle,
                    onEditNode = onEditNode,
                    onAddChild = onAddChild,
                    isLastRoot = isLastRoot,
                )
            }
        }
    }
    // Faint divider between top-level subtrees (not after the very last) so multiple
    // roots/companies read as separated blocks inside the one group card.
    if (depth == 0 && !isLastRoot) {
        Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp).height(1.dp).background(hairline))
    }
}

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

/**
 * One-line node summary: leader (부서장) / member count + keyword + company counts,
 * e.g. "김철수 외 1명 · 키워드 2 · 거래처 1". Blank for a bare node. When
 * [membersShown] (the node is expanded and its members are listed below), the people
 * part is dropped to avoid repeating the member lines — only the keyword/company
 * counts remain.
 */
internal fun nodeSummary(node: OrgNodeOut, membersShown: Boolean = false): String {
    val parts = mutableListOf<String>()
    if (!membersShown) {
        val leader = nodeLeader(node)
        when {
            leader != null && node.members.size > 1 -> parts.add("${leader.name} 외 ${node.members.size - 1}명")
            leader != null -> parts.add(leader.name)
            node.members.size == 1 -> parts.add(node.members.first().name)
            node.members.size > 1 -> parts.add("${node.members.size}명")
        }
    }
    if (node.keywords.isNotEmpty()) parts.add("키워드 ${node.keywords.size}")
    if (node.companies.isNotEmpty()) parts.add("거래처 ${node.companies.size}")
    return parts.joinToString(" · ")
}

/** Member display line: "이름 직급·직책" (omitting blanks), e.g. "김철수 전무·팀장". */
internal fun memberLine(m: MemberOut): String {
    val tail = listOf(m.rank, m.position).filter { it.isNotBlank() }.joinToString("·")
    return if (tail.isEmpty()) m.name.ifBlank { "(이름 없음)" } else "${m.name.ifBlank { "(이름 없음)" }}  $tail"
}

// --- node editor (bottom sheet body) ----------------------------------------

/**
 * The node editor: rename + type picker + dashboard-part (lane) toggle + member CRUD
 * (each with 직급/직책 dropdowns) + delete. Every control writes back through
 * [onChange] with the updated node, so the parent's working tree stays the single
 * source of truth (no local mirror to desync). Stateless over its node — previewable.
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
        OrgPartToggle(
            on = node.lane.isNotBlank(),
            onToggle = { on ->
                onChange(node.copy(lane = if (on) node.lane.ifBlank { node.id } else ""))
            },
        )
        Spacer(Modifier.height(18.dp))

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
