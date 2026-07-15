package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals

// stripFrontmatter feeds DenebSkillScreen's 문서 section: the YAML fence is
// meta the header already renders, so it must vanish — but never at the cost
// of eating document content on malformed input.
class SkillFrontmatterTest {

    private fun frontmatterCases(): List<() -> Unit> = listOf(
        {
            val body = "---\nname: x\ndescription: 설명\n---\n\n# 제목\n\n본문"
            assertEquals("# 제목\n\n본문", stripFrontmatter(body))
        },
        {
            val body = "# 제목\n\n--- 가운데 구분선은 그대로 ---"
            assertEquals(body, stripFrontmatter(body))
        },
        {
            val body = "---\nname: x\n본문이 fence 없이 끝남"
            assertEquals(body, stripFrontmatter(body))
        },
        {
            // "----" first line is a rule, not a frontmatter fence.
            val body = "----\n본문"
            assertEquals(body, stripFrontmatter(body))
        },
        {
            val body = "---\nname: x\n---\n본문"
            assertEquals("본문", stripFrontmatter(body))
        },
    )

    @Test
    fun skillFrontmatterPreservesBodyWhenFenceMissingOrMalformed() {
        frontmatterCases().forEach { it() }
    }
}
