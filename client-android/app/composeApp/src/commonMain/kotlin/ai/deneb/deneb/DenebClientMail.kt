package ai.deneb.deneb

import ai.deneb.deneb.generated.MailAnalysisOut
import ai.deneb.deneb.generated.MailMessageOut
import ai.deneb.deneb.generated.QATurn
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.http.encodeURLParameter
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import kotlinx.coroutines.flow.update

/**
 * Mail surface of [DenebGatewayClient] (`miniapp.gmail.*`): inbox list +
 * pagination, message detail, read/archive/trash mutations, AI analysis, Q&A,
 * attachments, and sender context. Extensions so the gateway client stays one
 * facade while each RPC domain lives in its own file.
 */

/** Refresh the mail list. With a [query] it runs a full-mailbox Gmail search
 *  (any Gmail syntax: keywords, `from:`, `has:attachment`, …); null/blank falls
 *  back to the server's default recent-inbox view. Returns false on a fetch
 *  failure so the screen can show a retry instead of a misleading empty state. */
suspend fun DenebGatewayClient.refreshMail(query: String? = null): Boolean {
    val q = query?.trim()?.ifBlank { null }
    val payload = callRpc<MailListPayload>(
        "miniapp.gmail.list_recent",
        buildJsonObject {
            put("limit", 25)
            q?.let { put("query", it) }
        },
    ) ?: return false
    denebMailActiveQuery = q
    _denebMail.value = payload.messages
        .filter { it.id.isNotBlank() }
        .map { MailMessage(it.id, it.from, it.subject, it.snippet, it.date, it.isUnread, it.priority, it.priorityHint) }
    _denebMailNextToken.value = payload.nextPageToken.ifBlank { null }
    return true
}

/** Append the next page of the current view (inbox or active search) to the list. */
suspend fun DenebGatewayClient.loadMoreMail() {
    val token = _denebMailNextToken.value ?: return
    val payload = callRpc<MailListPayload>(
        "miniapp.gmail.list_recent",
        buildJsonObject {
            put("limit", 25)
            put("pageToken", token)
            denebMailActiveQuery?.let { put("query", it) }
        },
    ) ?: return
    val seen = _denebMail.value.mapTo(HashSet()) { it.id }
    _denebMail.value = _denebMail.value + payload.messages
        .filter { it.id.isNotBlank() && it.id !in seen }
        .map { MailMessage(it.id, it.from, it.subject, it.snippet, it.date, it.isUnread, it.priority, it.priorityHint) }
    _denebMailNextToken.value = payload.nextPageToken.ifBlank { null }
}

suspend fun DenebGatewayClient.fetchMailDetail(id: String, full: Boolean = false): MailDetail? {
    val row = callRpc<MailMessageOut>(
        "miniapp.gmail.get",
        buildJsonObject {
            put("id", id)
            // full=true asks for the untruncated body (still server-bounded);
            // the default keeps the light 3000-char cap for the list→detail flow.
            if (full) put("full", true)
        },
    ) ?: return null
    return MailDetail(
        id = row.id,
        from = row.from,
        to = row.to,
        cc = row.cc,
        subject = row.subject,
        date = row.date,
        body = row.body,
        bodyTotal = row.bodyTotal,
        attachments = row.attachments
            .filter { it.id.isNotBlank() }
            .map { MailAttachment(it.id, it.filename.ifBlank { it.mimeType }, it.mimeType, it.size) },
    )
}

/** Mark read on the server and optimistically clear the unread dot in the list. */
suspend fun DenebGatewayClient.markMailRead(id: String): Boolean {
    val ok = callRpc<OkPayload>("miniapp.gmail.mark_read", buildJsonObject { put("id", id) })?.ok == true
    if (ok) {
        _denebMail.update { list -> list.map { if (it.id == id) it.copy(unread = false) else it } }
    }
    return ok
}

/** Archive (drop from inbox); optimistically removes the row from the list. */
suspend fun DenebGatewayClient.archiveMail(id: String): Boolean {
    val ok = callRpc<OkPayload>("miniapp.gmail.archive", buildJsonObject { put("id", id) })?.ok == true
    if (ok) _denebMail.update { list -> list.filterNot { it.id == id } }
    return ok
}

/** Move to Trash; optimistically removes the row from the list. */
suspend fun DenebGatewayClient.trashMail(id: String): Boolean {
    val ok = callRpc<OkPayload>("miniapp.gmail.trash", buildJsonObject { put("id", id) })?.ok == true
    if (ok) _denebMail.update { list -> list.filterNot { it.id == id } }
    return ok
}

/** Instant cached analysis (no LLM call) if one was already produced on poll or earlier. */
suspend fun DenebGatewayClient.fetchCachedAnalysis(id: String): MailAnalysis? =
    callRpc<MailAnalysisOut>("miniapp.gmail.analysis_cached", buildJsonObject { put("id", id) })?.toAnalysis()

/** Run AI analysis; force=true reruns the LLM instead of returning the cached result. */
suspend fun DenebGatewayClient.analyzeMail(id: String, force: Boolean = false): MailAnalysis? =
    callRpc<MailAnalysisOut>(
        "miniapp.gmail.analyze",
        buildJsonObject {
            put("id", id)
            if (force) put("force", true)
        },
    )?.toAnalysis()

private fun MailAnalysisOut.toAnalysis(): MailAnalysis? =
    if (analysis.isBlank()) {
        null
    } else {
        MailAnalysis(
            text = analysis,
            related = relatedProjects.map { RelatedProject(it.path, it.title, it.summary) },
            cached = cached,
            createdAt = createdAt,
            durationMs = durationMs,
        )
    }

/** Ask a follow-up about a message; prior Q&A is sent as history for multi-turn context. */
suspend fun DenebGatewayClient.askMail(id: String, question: String, history: List<Pair<String, String>> = emptyList()): String? =
    callRpc<AskPayload>(
        "miniapp.gmail.ask",
        buildJsonObject {
            put("id", id)
            put("question", question)
            // History items use the generated QATurn wire shape (json q/a) so the
            // gateway's []QATurn binding actually receives them — the old hand-rolled
            // {question, answer} keys silently dropped all prior-turn context.
            putJsonArray("history") {
                history.forEach { (q, a) -> add(jsonCodec.encodeToJsonElement(QATurn.serializer(), QATurn(q = q, a = a))) }
            }
        },
    )?.answer?.ifBlank { null }

/**
 * Download an attachment's raw bytes for in-app rendering (inline image
 * previews). Reuses [attachmentUrl] so auth matches the browser download path
 * exactly. Returns null on any failure — callers fall back to the plain chip.
 */
suspend fun DenebGatewayClient.fetchAttachmentBytes(messageId: String, att: MailAttachment): ByteArray? =
    runCatching {
        http.get(attachmentUrl(messageId, att)) {
            timeout {
                requestTimeoutMillis = 30_000
                connectTimeoutMillis = 6_000
            }
        }.body<ByteArray>()
    }.getOrNull()

/**
 * Browser-openable attachment download URL. The download endpoint can't read
 * the X-Deneb-Client-Token header from a browser opening a link, so the token
 * rides in the query string (acceptable in this single-user local setup).
 */
fun DenebGatewayClient.attachmentUrl(messageId: String, att: MailAttachment): String {
    fun e(s: String) = s.encodeURLParameter()
    return "$gatewayUrl/api/v1/miniapp/gmail/attachment" +
        "?messageId=${e(messageId)}&attachmentId=${e(att.id)}" +
        "&filename=${e(att.filename)}&mimeType=${e(att.mimeType)}&clientToken=${e(clientToken)}"
}

/** Fetch wiki / relationship context for a message's sender. */
suspend fun DenebGatewayClient.fetchSenderContext(sender: String): SenderContext? {
    val p = callRpc<SenderContextPayload>(
        "miniapp.gmail.sender_context",
        buildJsonObject { put("sender", sender) },
    ) ?: return null
    return SenderContext(
        displayName = p.displayName.ifBlank { p.sender },
        email = p.email,
        recentCount = p.recent?.count ?: 0,
        windowDays = p.recent?.windowDays ?: 0,
        wikiHits = p.wikiHits.map { SenderWikiHit(it.title.ifBlank { it.path }, it.summary, it.category, it.path) },
        wikiFacts = p.wikiFacts,
    )
}

/** Recent messages from a specific sender (`list_recent` with a from: query). */
suspend fun DenebGatewayClient.fetchRecentFromSender(email: String, limit: Int = 15): List<MailMessage> {
    if (email.isBlank()) return emptyList()
    val payload = callRpc<MailListPayload>(
        "miniapp.gmail.list_recent",
        buildJsonObject {
            put("query", "from:\"$email\"")
            put("limit", limit)
        },
    ) ?: return emptyList()
    return payload.messages
        .filter { it.id.isNotBlank() }
        .map { MailMessage(it.id, it.from, it.subject, it.snippet, it.date, it.isUnread, it.priority, it.priorityHint) }
}
