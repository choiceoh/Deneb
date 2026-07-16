package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.WorkFeedItem
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class FeedApprovalDeeplinkTest {
    @Test
    fun canOpenApprovalDetail_requiresSourceAndRef() {
        assertTrue(
            canOpenApprovalDetail(
                WorkFeedItem(source = "groupware-approval", refId = "99291", title = "금전대여"),
            ),
        )
        assertFalse(canOpenApprovalDetail(WorkFeedItem(source = "groupware-approval", refId = "")))
        assertFalse(canOpenApprovalDetail(WorkFeedItem(source = "mail_report", refId = "99291")))
        assertFalse(canOpenApprovalDetail(WorkFeedItem(source = "proactive", refId = "groupware-radar-list")))
    }
}
