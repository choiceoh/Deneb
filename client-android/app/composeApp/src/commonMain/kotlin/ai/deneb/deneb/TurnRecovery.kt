// TurnRecovery.kt — pure classification for stream-failure recovery.
//
// On mobile the chat SSE socket often dies HALF-OPEN: the gateway never
// notices, keeps running the turn, and persists an answer the phone never
// received. ask() recovers by polling the session transcript (the canonical
// record) and classifying what it shows for the turn it just sent. The
// classification is kept pure so it can be unit-tested without a network.
package ai.deneb.deneb

import ai.deneb.ui.chat.History

/** Outcome of probing a fetched transcript for one sent turn. */
internal sealed interface TurnProbe {
    /** The user message is there and an assistant reply follows it. */
    data class Answered(val text: String) : TurnProbe

    /** The user message is there but no assistant reply yet — turn in flight. */
    object StillRunning : TurnProbe

    /** The user message never made it into the transcript. */
    object NotArrived : TurnProbe
}

/**
 * Classify a transcript probe, treating a persisted assistant row as still
 * in-flight while the gateway reports the session run active. Without this,
 * a mid-turn SSE drop after a short preamble is accepted as the final answer
 * once the text stabilizes — the real wrap-up is never adopted.
 */
internal fun effectiveTurnProbe(probe: TurnProbe, turnRunning: Boolean): TurnProbe =
    if (turnRunning && probe is TurnProbe.Answered) TurnProbe.StillRunning else probe

/**
 * Locate [sentText] in a display transcript and classify the turn. Matches the
 * LAST user row equal to the sent text (the gateway display-strips its
 * timestamp prefix, so rows compare against what was actually sent — and an
 * impatient duplicate resend resolves to the newest copy); the reply is the
 * last non-blank assistant row after it, so a tool-looping turn resolves to
 * its final wrap-up text.
 */
internal fun probeTranscriptForTurn(transcript: List<History>, sentText: String): TurnProbe {
    val sent = sentText.trim()
    if (sent.isEmpty()) return TurnProbe.NotArrived
    val idx = transcript.indexOfLast { it.role == History.Role.USER && it.content.trim() == sent }
    if (idx < 0) return TurnProbe.NotArrived
    val answer = transcript.drop(idx + 1)
        .lastOrNull {
            it.role == History.Role.ASSISTANT &&
                it.content.isNotBlank() &&
                !it.isThinking &&
                !it.isStatusMessage
        }
        ?: return TurnProbe.StillRunning
    return TurnProbe.Answered(answer.content)
}
