package ai.deneb.deneb

import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import kotlin.random.Random
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

// Org-chart domain helpers (tree edits, search, labels) — split from
// DenebOrgChartScreen.kt (pure move).

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
