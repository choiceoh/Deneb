package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

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

    @Test
    fun inlineApprovalEventsMapToGuardedWorkFeedActions() {
        assertEquals("approval:approve", approvalActionIdForUiEvent("approve_fuel_98269"))
        assertEquals("approval:reject", approvalActionIdForUiEvent("reject_fuel_98269"))
        assertNull(approvalActionIdForUiEvent("open_document_98269"))
    }

    @Test
    fun groupwareCardUsesInlineActionsOnlyWhenBothButtonsAreBound() {
        val actions = listOf(
            WorkFeedAction(id = "approval:approve", label = "승인"),
            WorkFeedAction(id = "approval:reject", label = "반려"),
        )
        val item = WorkFeedItem(
            source = "groupware-approval",
            actions = actions,
            body = """
                <row>
                  <button event="approve_fuel_98269" variant="tonal">승인</button>
                  <button event="reject_fuel_98269" variant="outlined">반려</button>
                </row>
            """.trimIndent(),
        )

        assertTrue(item.hasInlineApprovalActions())
        assertFalse(item.copy(body = item.body.substringBefore("<button event=\"reject")).hasInlineApprovalActions())
        assertFalse(item.copy(source = "proactive").hasInlineApprovalActions())
    }
}
