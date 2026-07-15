package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals

class WorkFeedApprovalCommentTest {
    @Test
    fun limitApprovalCommentCapsKoreanCharacters() {
        val limited = limitApprovalComment("한".repeat(MaxApprovalCommentCharacters + 20))

        assertEquals(MaxApprovalCommentCharacters, approvalCommentCharacterCount(limited))
        assertEquals("한".repeat(MaxApprovalCommentCharacters), limited)
    }

    @Test
    fun limitApprovalCommentKeepsSurrogatePairsWhole() {
        val limited = limitApprovalComment("🙂".repeat(MaxApprovalCommentCharacters + 20))

        assertEquals(MaxApprovalCommentCharacters, approvalCommentCharacterCount(limited))
        assertEquals("🙂".repeat(MaxApprovalCommentCharacters), limited)
    }
}
