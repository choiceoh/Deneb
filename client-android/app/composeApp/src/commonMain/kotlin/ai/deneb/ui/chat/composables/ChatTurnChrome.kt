package ai.deneb.ui.chat.composables

/**
 * The trailing "생각 중" row. Hide it once tokens are on screen — the growing
 * answer is the status — but keep it for in-flight tools. A pending deneb-ui
 * submit already pulses on the pressed button, so that path stays quiet.
 */
internal fun chatShowWaitingRow(
    isLoading: Boolean,
    isResponseStreaming: Boolean,
    hasExecutingTools: Boolean,
    hasPendingUiSubmission: Boolean,
): Boolean {
    if (!isLoading) return false
    if (hasExecutingTools) return true
    if (hasPendingUiSubmission) return false
    return !isResponseStreaming
}

/**
 * Speak only when a turn we started actually finished with an answer.
 * Session switch / cancel / empty reply / error must not read an old bubble,
 * and we never start on the first streaming chunk (history.size bump).
 */
internal fun chatShouldSpeakFinishedReply(
    speechEnabled: Boolean,
    loadingUserId: String?,
    lastUserId: String?,
    lastAssistantId: String?,
    lastAssistantHasText: Boolean,
    stoppedMessageId: String?,
    hasError: Boolean,
    unanswered: Boolean,
): Boolean {
    if (!speechEnabled || hasError || unanswered) return false
    if (loadingUserId == null || lastUserId == null || loadingUserId != lastUserId) return false
    if (lastAssistantId == null || !lastAssistantHasText) return false
    if (lastAssistantId == stoppedMessageId) return false
    return true
}
