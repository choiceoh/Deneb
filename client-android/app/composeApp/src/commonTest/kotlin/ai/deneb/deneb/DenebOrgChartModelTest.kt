package ai.deneb.deneb

import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertSame
import kotlin.test.assertTrue

class DenebOrgChartModelTest {

    private fun node(
        id: String,
        parentId: String = "",
        members: List<MemberOut> = emptyList(),
    ) = OrgNodeOut(id = id, name = id, parentId = parentId, members = members)

    @Test
    fun splitCsvTrimsAndDropsEmptyValues() {
        assertEquals(listOf("인허가", "발전사업허가", "RE100"), splitCsv(" 인허가, , 발전사업허가,RE100 "))
        assertEquals(emptyList(), splitCsv(" ,  ,"))
    }

    @Test
    fun orgTypeLabelsKnownAndFallbackValues() {
        assertEquals("그룹", orgTypeLabel("group"))
        assertEquals("회사", orgTypeLabel("company"))
        assertEquals("본부/실", orgTypeLabel("division"))
        assertEquals("팀", orgTypeLabel("team"))
        assertEquals("파트", orgTypeLabel("파트"))
        assertEquals("조직", orgTypeLabel(""))
    }

    @Test
    fun nodeLeaderReturnsFirstDepartmentLeaderOnly() {
        val representative = MemberOut(name = "대표", position = "대표")
        val leader = MemberOut(name = "팀장", position = "팀장")
        val secondLeader = MemberOut(name = "실장", position = "실장")
        val org = node("team", members = listOf(representative, leader, secondLeader))

        assertSame(leader, nodeLeader(org))
        assertNull(nodeLeader(node("no-leader", members = listOf(representative))))
    }

    @Test
    fun removeSubtreeDeletesEveryDescendantRegardlessOfInputOrder() {
        val nodes = listOf(
            node("grandchild", parentId = "child"),
            node("unrelated"),
            node("child", parentId = "root"),
            node("root"),
            node("great-grandchild", parentId = "grandchild"),
        )

        assertEquals(listOf("unrelated"), removeSubtree(nodes, "root").map { it.id })
        assertEquals(nodes, removeSubtree(nodes, "missing"))
    }

    @Test
    fun searchMembersIgnoresCaseAndSpacesAndKeepsConcurrentAppointments() {
        val planningMember = MemberOut(name = "Kim Min Jun", position = "실장")
        val teamMember = MemberOut(name = "KIMMINJUN", position = "팀장")
        val nodes = listOf(
            node("planning", members = listOf(planningMember, MemberOut(name = "이서연"))),
            node("team", parentId = "planning", members = listOf(teamMember)),
        )

        val hits = searchMembers(nodes, " kim minjun ")

        assertEquals(listOf("planning", "team"), hits.map { it.node.id })
        assertEquals(listOf(planningMember, teamMember), hits.map { it.member })
        assertTrue(searchMembers(nodes, "  ").isEmpty())
        assertTrue(searchMembers(nodes, "없는 사람").isEmpty())
    }
}
