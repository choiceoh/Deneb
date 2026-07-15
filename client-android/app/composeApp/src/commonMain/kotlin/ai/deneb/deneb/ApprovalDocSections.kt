package ai.deneb.deneb

/**
 * Split Amaranth approval reader text into meta / 결재선 / 본문 / 첨부.
 * Mirrors andromeda/src/approvalBody.ts — formatDocRead concatenates these
 * with bare section headers. 문서번호/id are agent plumbing and dropped; the
 * useful meta (양식·기안·기안일) is lifted into structured fields for the header.
 */
data class ApprovalDocSections(
    val title: String = "",
    val form: String = "",
    val drafter: String = "",
    val draftedAt: String = "",
    val line: String = "",
    val lineCount: Int = 0,
    val body: String = "",
    val attachments: String = "",
    val attachmentHeader: String = "",
    val attachmentCount: Int = 0,
)

private val approvalNumberedRow = Regex("""^\s*\d+\.\s""")
private val approvalAttachHeader = Regex("""^첨부(\s*\(.*\))?\s*$""")

fun parseApprovalDocBody(raw: String): ApprovalDocSections {
    val text = raw.replace("\r\n", "\n")
    if (text.isBlank()) return ApprovalDocSections()

    val lines = text.split('\n')
    var lineStart = -1
    var bodyStart = -1
    var attachStart = -1
    for (i in lines.indices) {
        val t = lines[i].trimEnd()
        when {
            lineStart < 0 && t == "결재선" -> lineStart = i
            bodyStart < 0 && t == "본문" -> bodyStart = i
            attachStart < 0 && approvalAttachHeader.matches(t) -> attachStart = i
        }
    }

    if (lineStart < 0 && bodyStart < 0 && attachStart < 0) {
        return ApprovalDocSections(body = text.trim())
    }

    fun endOf(start: Int, vararg stops: Int): Int = stops.filter { it > start }.minOrNull() ?: lines.size

    fun sliceBlock(headerIdx: Int, end: Int): String = lines.subList(headerIdx + 1, end).joinToString("\n").trim()

    var metaEnd = lines.size
    if (lineStart >= 0) metaEnd = minOf(metaEnd, lineStart)
    if (bodyStart >= 0) metaEnd = minOf(metaEnd, bodyStart)
    if (attachStart >= 0) metaEnd = minOf(metaEnd, attachStart)

    var title = ""
    var form = ""
    var drafter = ""
    var draftedAt = ""
    for (raw in lines.subList(0, metaEnd)) {
        val t = raw.trim()
        when {
            t.startsWith("제목:") -> title = t.removePrefix("제목:").trim()
            t.startsWith("양식:") -> form = t.removePrefix("양식:").trim()
            t.startsWith("기안:") -> drafter = t.removePrefix("기안:").trim()
            t.startsWith("기안일:") -> draftedAt = t.removePrefix("기안일:").trim()
        }
    }

    val line = if (lineStart >= 0) {
        sliceBlock(lineStart, endOf(lineStart, bodyStart, attachStart))
    } else {
        ""
    }
    val body = when {
        bodyStart >= 0 -> sliceBlock(bodyStart, endOf(bodyStart, attachStart))
        lineStart < 0 && attachStart < 0 -> text.trim()
        else -> ""
    }
    val attachmentHeader = if (attachStart >= 0) lines[attachStart].trim() else ""
    val attachments = if (attachStart >= 0) sliceBlock(attachStart, lines.size) else ""

    fun countRows(block: String): Int = block.lineSequence().count { approvalNumberedRow.containsMatchIn(it) }

    return ApprovalDocSections(
        title = title,
        form = form,
        drafter = drafter,
        draftedAt = draftedAt,
        line = line,
        lineCount = countRows(line),
        body = body,
        attachments = attachments,
        attachmentHeader = attachmentHeader,
        attachmentCount = countRows(attachments),
    )
}
