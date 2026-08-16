package ai.deneb.ui.chat.composables

internal data class ChatStickSnapshot(
    val viewportHeight: Int,
    val lastIndex: Int,
    val lastSize: Int,
    val lastBottom: Int,
    val viewportEnd: Int,
    val total: Int,
)

/**
 * Extra px to scroll so a bottom-pinned conversation stays pinned when the
 * viewport shrinks (IME) or content extends downward (streaming tokens).
 *
 * Streaming often grows the *second-to-last* row (the answer) while the last
 * visible row is the waiting chip — so we follow [lastBottom] (offset+size),
 * not the last row's own height. 0 when the user has scrolled up, on the
 * first layout, or when the last row is a newly inserted item.
 */
internal fun chatStickFollowScrollPx(
    previousViewportHeight: Int,
    viewportHeight: Int,
    previousLastKey: Int,
    lastKey: Int,
    previousLastBottom: Int,
    lastBottom: Int,
    stickToBottom: Boolean,
): Int {
    if (!stickToBottom) return 0
    var px = 0
    if (previousViewportHeight > 0 && viewportHeight > 0) {
        val shrink = previousViewportHeight - viewportHeight
        if (shrink > 0) px += shrink
    }
    if (lastKey == previousLastKey && previousLastBottom > 0 && lastBottom > previousLastBottom) {
        px += lastBottom - previousLastBottom
    }
    return px
}

/**
 * A follow-frame must not look like "user scrolled up" — unless they actually did.
 *
 * Drag wins: once the user pulls the list, stay detached until they return to
 * the bottom ([nearBottom]). List growth (waiting chip / error row) and IME /
 * token frames keep the previous pin so a new trailing row doesn't look like
 * a deliberate scroll-away.
 */
internal fun chatStickKeepPinned(
    wasPinned: Boolean,
    nearBottom: Boolean,
    viewportShrunk: Boolean,
    contentGrew: Boolean,
    userDragging: Boolean = false,
    listGrew: Boolean = false,
): Boolean = when {
    userDragging -> false
    viewportShrunk || contentGrew || listGrew -> wasPinned
    else -> nearBottom
}

/**
 * Structural catch-up: the last row isn't composed yet, a trailing row was
 * just appended, or the viewport grew (IME close) while we were pinned.
 * Token-by-token leftover pixels are [chatStickFollowScrollPx]'s job.
 */
internal fun chatStickNeedsSnap(
    stickToBottom: Boolean,
    lastIndex: Int,
    total: Int,
    listGrew: Boolean,
    viewportGrew: Boolean,
): Boolean = stickToBottom && total > 0 && (lastIndex < total - 1 || listGrew || viewportGrew)
