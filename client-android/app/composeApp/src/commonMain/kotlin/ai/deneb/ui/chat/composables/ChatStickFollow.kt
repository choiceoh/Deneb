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
 * viewport shrinks (IME opening) or the last row grows (streaming tokens).
 *
 * 0 when the user has scrolled up, on the first layout, or when the last row
 * is a newly inserted item — send/install already lands that separately.
 */
internal fun chatStickFollowScrollPx(
    previousViewportHeight: Int,
    viewportHeight: Int,
    previousLastKey: Int,
    lastKey: Int,
    previousLastSize: Int,
    lastSize: Int,
    stickToBottom: Boolean,
): Int {
    if (!stickToBottom) return 0
    var px = 0
    if (previousViewportHeight > 0 && viewportHeight > 0) {
        val shrink = previousViewportHeight - viewportHeight
        if (shrink > 0) px += shrink
    }
    if (lastKey == previousLastKey && previousLastSize > 0 && lastSize > previousLastSize) {
        px += lastSize - previousLastSize
    }
    return px
}
