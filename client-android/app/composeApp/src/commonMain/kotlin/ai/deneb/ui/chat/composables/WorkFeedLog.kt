package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.chat.isLogCard

/** Which list the 피드|결재|로그 pivot is showing. */
internal enum class FeedLane { Work, Log }

/**
 * System/agent cards belong on 로그, not the 업무 피드. Keep this aligned with
 * `workfeed.IsLogCard` in gateway-go — source first, then a tight title heuristic
 * for older proactive cards that were stored as `proactive`.
 */
internal fun List<WorkFeedItem>.forFeedLane(lane: FeedLane): List<WorkFeedItem> = when (lane) {
    FeedLane.Work -> filterNot { it.isLogCard }
    FeedLane.Log -> filter { it.isLogCard }
}
