package ai.deneb.deneb

import ai.deneb.data.UiSubmission

/** Prefix the client puts on a deneb-ui card interaction when it sends it as a turn. */
private const val UI_CALLBACK_MARKER = "[deneb-ui] event="

/**
 * Reads back a card interaction the client itself sent.
 *
 * The wire form (`[deneb-ui] event=confirm values={qty=3}`) is written for the
 * agent, not for a reader. It is stored verbatim in the transcript, so on reload —
 * app restart, session switch, change-feed refresh — a tap on a card came back as
 * that raw line sitting in the conversation like something the user had typed.
 * Recognising it lets the list restore the interaction to what it was: the card
 * above, frozen on the answer.
 *
 * [sourceContent] is the assistant text the card was rendered from; the list pairs
 * the submission back to that message.
 *
 * Returns null for anything that is not a callback line, including a message that
 * merely mentions the marker mid-sentence — only a line that starts with it counts.
 */
internal fun parseUiCallback(content: String, sourceContent: String): UiSubmission? {
    if (!content.startsWith(UI_CALLBACK_MARKER)) return null
    val rest = content.removePrefix(UI_CALLBACK_MARKER)
    val valuesAt = rest.indexOf(" values={")
    val event = (if (valuesAt < 0) rest else rest.take(valuesAt)).trim()
    if (event.isEmpty()) return null
    val values = if (valuesAt < 0) {
        emptyMap()
    } else {
        rest.substring(valuesAt + " values={".length)
            .substringBeforeLast('}')
            .split(", ")
            .mapNotNull { pair ->
                val at = pair.indexOf('=')
                if (at <= 0) null else pair.take(at) to pair.substring(at + 1)
            }
            .toMap()
    }
    return UiSubmission(sourceContent = sourceContent, values = values, pressedEvent = event)
}
