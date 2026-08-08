package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class CategoryHierarchyTest {

    private fun category(name: String, pages: Int) = WikiCategory(name, pages)

    private val categories = listOf(
        category("프로젝트/영산고", 2),
        category("인물/김민준", 1),
        category("프로젝트/군산/인허가", 3),
        category("프로젝트/군산/계약", 4),
        category("회사", 5),
        category("인물/이서연", 2),
        category("프로젝트2/별도", 7),
    )

    @Test
    fun topLevelCategoriesCollapseEveryDescendantCount() {
        assertEquals(
            listOf(
                CategoryNode("프로젝트", 9),
                CategoryNode("인물", 3),
                CategoryNode("회사", 5),
                CategoryNode("프로젝트2", 7),
            ),
            topLevelCategories(categories),
        )
    }

    @Test
    fun topLevelOrderFollowsFirstAppearance() {
        val reversed = categories.reversed()

        assertEquals(
            listOf("프로젝트2", "인물", "회사", "프로젝트"),
            topLevelCategories(reversed).map { it.name },
        )
    }

    @Test
    fun duplicateRowsContributeToTheSameBucket() {
        val input = listOf(
            category("프로젝트/A", 2),
            category("프로젝트/A", 3),
            category("프로젝트/B", 4),
        )

        assertEquals(listOf(CategoryNode("프로젝트", 9)), topLevelCategories(input))
    }

    @Test
    fun immediateSubcategoriesAggregateDeeperDescendants() {
        assertEquals(
            listOf(CategoryNode("프로젝트/영산고", 2), CategoryNode("프로젝트/군산", 7)),
            subCategories("프로젝트", categories),
        )
    }

    @Test
    fun drilldownUsesFullParentPath() {
        assertEquals(
            listOf(CategoryNode("프로젝트/군산/인허가", 3), CategoryNode("프로젝트/군산/계약", 4)),
            subCategories("프로젝트/군산", categories),
        )
    }

    @Test
    fun siblingPrefixDoesNotLeakIntoParent() {
        val project = subCategories("프로젝트", categories)

        assertTrue(project.none { it.name.startsWith("프로젝트2") })
        assertEquals(listOf(CategoryNode("프로젝트2/별도", 7)), subCategories("프로젝트2", categories))
    }

    @Test
    fun leafAndMissingParentsHaveNoSubcategories() {
        assertEquals(emptyList(), subCategories("회사", categories))
        assertEquals(emptyList(), subCategories("없는분류", categories))
    }

    // Project folders are frozen codes, so the drill-down row must render the
    // gateway's Korean alias while `name` stays the navigation key. The alias
    // rides on every row under the project (the gateway resolves the OWNING
    // project), so a deeper slot row supplies it just as well as the folder row.
    @Test
    fun drilldownRendersKoreanAliasAndKeepsPathAsKey() {
        val coded = listOf(
            WikiCategory("프로젝트/pl2-kia-epc-001", 2, "기아 화성 국유지 태양광"),
            WikiCategory("프로젝트/pl2-kia-epc-001/메일분석", 9, "기아 화성 국유지 태양광"),
            WikiCategory("프로젝트/영산고", 2),
        )

        val nodes = subCategories("프로젝트", coded)

        assertEquals(listOf("프로젝트/pl2-kia-epc-001", "프로젝트/영산고"), nodes.map { it.name })
        assertEquals(listOf("기아 화성 국유지 태양광", "영산고"), nodes.map { it.label() })
        // Counts still roll up the whole subtree.
        assertEquals(11, nodes.first().pageCount)
    }

    @Test
    fun aliasSurvivesWhenOnlyADeeperRowCarriesIt() {
        val slotOnly = listOf(WikiCategory("프로젝트/nde-ztt-cbl-001/기자재", 4, "비금도 해저케이블"))

        assertEquals("비금도 해저케이블", subCategories("프로젝트", slotOnly).single().label())
    }

    @Test
    fun emptyInputProducesNoNavigationNodes() {
        assertEquals(emptyList(), topLevelCategories(emptyList()))
        assertEquals(emptyList(), subCategories("프로젝트", emptyList()))
    }
}
