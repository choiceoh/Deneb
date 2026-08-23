package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import kotlin.test.Test
import kotlin.test.assertEquals

class WorkFeedChipActionsTest {
    @Test
    fun chipActionsPromoteCardSpecificOpsOnly() {
        val item = WorkFeedItem(
            id = "wf1",
            source = "dream",
            actions = listOf(
                WorkFeedAction(id = "dream:ack", kind = "ack", label = "확인"),
                WorkFeedAction(id = "dream:revert:abc", kind = "mark", label = "전체 되돌리기"),
                WorkFeedAction(id = "dream:revert-page:abc:0", kind = "mark", label = "↩ 학습 · 대표"),
                // Done operations drop off the chip row.
                WorkFeedAction(id = "dream:revert-page:abc:1", kind = "mark", label = "↩ 학습 · 로그", status = "done"),
            ),
        )

        val chips = item.chipActions()

        assertEquals(listOf("전체 되돌리기", "↩ 학습 · 대표"), chips.map { it.label })
    }

    @Test
    fun lifecycleKindsStayHiddenEvenWithCustomIds() {
        val item = WorkFeedItem(
            id = "wf2",
            source = "capture_image",
            actions = listOf(
                WorkFeedAction(id = "open", kind = "open", label = "열기"),
                WorkFeedAction(id = "followup", kind = "followup", label = "문서화"),
                WorkFeedAction(id = "snooze", kind = "snooze", label = "나중에"),
                WorkFeedAction(id = "ack", kind = "ack", label = "완료"),
            ),
        )

        assertEquals(emptyList(), item.chipActions())
    }
}
