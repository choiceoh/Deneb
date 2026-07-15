package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class WorkFeedSourcePresentationTest {
    @Test
    fun groupwareApprovalUsesDistinctCheckedDocumentSemantics() {
        val presentation = workFeedSourcePresentation("groupware-approval")

        assertEquals(WorkFeedSourceIcon.APPROVAL, presentation.icon)
        assertEquals("전자결재", presentation.label)
    }

    @Test
    fun unknownSourceKeepsGenericDocumentFallback() {
        val presentation = workFeedSourcePresentation("future-source")

        assertEquals(WorkFeedSourceIcon.DOCUMENT, presentation.icon)
        assertNull(presentation.label)
    }
}
