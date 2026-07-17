package ai.deneb.deneb

import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.ui.DenebOutlinedTextField
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.denebPressable
import ai.deneb.ui.handCursor
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
import ai.deneb.ui.icons.outlined.Article
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.MailOutline
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

// Stateless org-chart body: tree rows, search, contact actions — split from
// DenebOrgChartScreen.kt (pure move). Previewed by the render harness.

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
    onOpenPersonWiki: (String) -> Unit = {}, // open a searched member's 인물 page (no-op for previews)
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
            OrgSearchResults(hits = hits, onPick = { hit -> revealNode(hit.node) }, onOpenWiki = onOpenPersonWiki)
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
    onOpenWiki: (String) -> Unit,
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
                // Inline call/email + 위키 shortcuts when the gateway enriched this member
                // — search is people-centric, so let the operator reach the person (or open
                // their 인물 knowledge page) straight from the result, no editor detour.
                OrgContactActions(
                    member = hit.member,
                    glyphSize = 18.dp,
                    leadingGap = 2.dp,
                    onOpenWiki = { onOpenWiki(hit.member.personPath) },
                )
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
 * Design (see docs/agent-rules/native-design-system.md): these are *functional* icons
 * (phone/mail) — allowed; placed as small, restrained glyph buttons, not decoration.
 */
@Composable
internal fun OrgContactActions(
    member: MemberOut,
    glyphSize: Dp,
    leadingGap: Dp,
    onOpenWiki: (() -> Unit)? = null,
) {
    val phone = member.phones.firstOrNull { it.isNotBlank() }
    val email = member.emails.firstOrNull { it.isNotBlank() }
    // The wiki glyph appears only when the gateway resolved this member to a 인물
    // page (personPath) AND the caller wired navigation — the search strip does,
    // the editor does not (navigating away mid-edit would be jarring).
    val wikiOpen = onOpenWiki?.takeIf { member.personPath.isNotBlank() }
    if (phone == null && email == null && wikiOpen == null) return

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
    if (wikiOpen != null) {
        IconButton(
            onClick = wikiOpen,
            modifier = Modifier.size(buttonSize),
        ) {
            Icon(Icons.Outlined.Article, contentDescription = "${member.name} 위키 열기", tint = accent, modifier = Modifier.size(glyphSize))
        }
    }
}
