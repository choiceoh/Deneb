package ai.deneb.deneb

import ai.deneb.contacts.ContactData
import ai.deneb.ui.chat.History
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

/**
 * Capture surface of [DenebGatewayClient] (`miniapp.capture.*`): the native
 * "share to Deneb" paths — image OCR, audio transcription, and address-book
 * sync — each running one agent turn whose result lands in the chat transcript.
 * Extensions so the gateway client stays one facade while each RPC domain lives
 * in its own file.
 */

/**
 * OCR a shared image on the gateway and run one agent turn over the extracted
 * text, showing the result in the chat. The native client's "share an image to
 * Deneb" path — the gateway uses the PaddleOCR sidecar (tesseract fallback).
 */
@OptIn(ExperimentalEncodingApi::class)
suspend fun DenebGatewayClient.captureImage(bytes: ByteArray, mimeType: String, caption: String = ""): Boolean {
    if (clientToken.isEmpty() || bytes.isEmpty()) return false
    val trimmedCaption = caption.trim()
    val label = if (trimmedCaption.isNotBlank()) {
        trimmedCaption + "\n📷 이미지 OCR 분석 중…"
    } else {
        "📷 이미지 공유됨 (OCR 분석 중…)"
    }
    val pending = History(role = History.Role.USER, content = label)
    _chatHistory.update { it + pending }
    val reply = try {
        val payload = callRpc<CaptureImagePayload>(
            "miniapp.capture.image",
            buildJsonObject {
                put("image", Base64.encode(bytes))
                put("mimeType", mimeType)
                put("sessionKey", sessionKey)
                // Source context the image alone lacks (originating app/sender/
                // notification text); the gateway prepends it to the OCR turn.
                if (trimmedCaption.isNotBlank()) put("caption", trimmedCaption)
            },
        )
        payload?.text?.ifBlank { null } ?: "이미지에서 텍스트를 찾지 못했거나 분석에 실패했습니다."
    } catch (c: CancellationException) {
        _chatHistory.update { history -> history.filterNot { it.id == pending.id } }
        throw c
    } catch (e: Exception) {
        "⚠️ ${e.message ?: "이미지 캡처 실패"}"
    }
    _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    syncNativeStateAsync()
    return true
}

/**
 * Transcribe a shared audio recording (voice memo, meeting audio) via the
 * gateway's VibeVoice-ASR sidecar and run one agent turn over the diarized
 * transcript (speaker labels + timestamps). This is the native client's
 * "share a recording to Deneb" path.
 */
@OptIn(ExperimentalEncodingApi::class)
suspend fun DenebGatewayClient.captureAudio(bytes: ByteArray, mimeType: String) {
    if (clientToken.isEmpty() || bytes.isEmpty()) return
    val pending = History(role = History.Role.USER, content = "🎙️ 녹음 공유됨 (전사·회의록 분석 중…)")
    _chatHistory.update { it + pending }
    val reply = try {
        val payload = callRpc<CaptureAudioPayload>(
            "miniapp.capture.audio",
            buildJsonObject {
                put("audio", Base64.encode(bytes))
                put("mimeType", mimeType)
                put("sessionKey", sessionKey)
            },
        )
        payload?.text?.ifBlank { null } ?: "녹음에서 음성을 인식하지 못했거나 전사에 실패했습니다."
    } catch (c: CancellationException) {
        _chatHistory.update { history -> history.filterNot { it.id == pending.id } }
        throw c
    } catch (e: Exception) {
        "⚠️ ${e.message ?: "녹음 캡처 실패"}"
    }
    _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    syncNativeStateAsync()
}

/**
 * Extract text from a shared document (pdf / Word / Excel / PowerPoint / CSV /
 * plain text) on the gateway and run one agent turn over it, showing the result
 * in the chat. The native client's "attach a document to Deneb" path — the
 * gateway uses the in-house extractor with a scanned-PDF / image OCR fallback.
 */
@OptIn(ExperimentalEncodingApi::class)
suspend fun DenebGatewayClient.captureDocument(
    bytes: ByteArray,
    filename: String,
    mimeType: String,
    caption: String = "",
): Boolean {
    if (clientToken.isEmpty() || bytes.isEmpty()) return false
    val name = filename.trim().ifBlank { "문서" }
    val trimmedCaption = caption.trim()
    val label = if (trimmedCaption.isNotBlank()) {
        trimmedCaption + "\n📄 $name 분석 중…"
    } else {
        "📄 문서 공유됨: $name (분석 중…)"
    }
    val pending = History(role = History.Role.USER, content = label)
    _chatHistory.update { it + pending }
    val reply = try {
        val payload = callRpc<CaptureDocumentPayload>(
            "miniapp.capture.document",
            buildJsonObject {
                put("document", Base64.encode(bytes))
                put("filename", filename)
                put("mimeType", mimeType)
                put("sessionKey", sessionKey)
                // The text the user typed alongside the attachment becomes source
                // context the gateway prepends to the extraction turn.
                if (trimmedCaption.isNotBlank()) put("caption", trimmedCaption)
            },
        )
        payload?.text?.ifBlank { null } ?: "문서에서 텍스트를 추출하지 못했거나 분석에 실패했습니다."
    } catch (c: CancellationException) {
        _chatHistory.update { history -> history.filterNot { it.id == pending.id } }
        throw c
    } catch (e: Exception) {
        "⚠️ ${e.message ?: "문서 캡처 실패"}"
    }
    _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    syncNativeStateAsync()
    return true
}

/**
 * One attachment in a [captureBatch]: raw bytes plus the filename and MIME the
 * gateway needs to dispatch its extractor. Not a data class — a ByteArray field
 * would make structural equality meaningless (and trips detekt's ArrayInDataClass).
 */
class DenebAttachment(val bytes: ByteArray, val filename: String, val mimeType: String)

/**
 * Attach N files in ONE turn. The gateway (`miniapp.capture.batch`) materializes
 * each file to its agent-readable memory store and runs a single agent turn over a
 * pointer list — the agent reads whichever files it needs and cross-references them.
 * So six shared files land as one context to analyze together, not six separate
 * turns. The typed text rides as the batch caption.
 */
@OptIn(ExperimentalEncodingApi::class)
suspend fun DenebGatewayClient.captureBatch(files: List<DenebAttachment>, caption: String = ""): Boolean {
    if (clientToken.isEmpty()) return false
    val usable = files.filter { it.bytes.isNotEmpty() }
    if (usable.isEmpty()) return false
    val trimmedCaption = caption.trim()
    val label = buildString {
        append("📎 첨부 ${usable.size}개")
        usable.forEach { append("\n• ${it.filename.trim().ifBlank { "첨부" }}") }
        if (trimmedCaption.isNotBlank()) {
            append("\n\n")
            append(trimmedCaption)
        }
    }
    val pending = History(role = History.Role.USER, content = label)
    _chatHistory.update { it + pending }
    val reply = try {
        val payload = callRpc<CaptureBatchPayload>(
            "miniapp.capture.batch",
            buildJsonObject {
                putJsonArray("files") {
                    usable.forEach { file ->
                        addJsonObject {
                            put("data", Base64.encode(file.bytes))
                            put("mimeType", file.mimeType)
                            put("filename", file.filename)
                        }
                    }
                }
                put("sessionKey", sessionKey)
                if (trimmedCaption.isNotBlank()) put("caption", trimmedCaption)
            },
        )
        payload?.text?.ifBlank { null } ?: "첨부에서 내용을 추출하지 못했거나 분석에 실패했습니다."
    } catch (c: CancellationException) {
        _chatHistory.update { history -> history.filterNot { it.id == pending.id } }
        throw c
    } catch (e: Exception) {
        "⚠️ ${e.message ?: "첨부 캡처 실패"}"
    }
    _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    syncNativeStateAsync()
    return true
}

/**
 * Sync the device address book into the gateway. The gateway enriches ONLY the
 * people already in its wiki (it creates no pages) with phone/email/org — so a
 * sync both sharpens ASR proper-noun bias and powers "whose number is this?"
 * lookups, without uploading the whole phone book as new entries. Runs one
 * gateway turn and shows the Korean summary in the chat transcript.
 */
suspend fun DenebGatewayClient.captureContacts(contacts: List<ContactData>) {
    if (clientToken.isEmpty() || contacts.isEmpty()) return
    val pending = History(role = History.Role.USER, content = "📇 주소록 ${contacts.size}개 동기화 중…")
    _chatHistory.update { it + pending }
    val reply = try {
        val payload = callRpc<CaptureContactsPayload>(
            "miniapp.capture.contacts",
            buildJsonObject {
                putJsonArray("contacts") {
                    contacts.forEach { contact ->
                        addJsonObject {
                            put("name", contact.name)
                            putJsonArray("phones") { contact.phones.forEach { add(it) } }
                            putJsonArray("emails") { contact.emails.forEach { add(it) } }
                            put("org", contact.org)
                        }
                    }
                }
                put("sessionKey", sessionKey)
            },
        )
        payload?.text?.ifBlank { null } ?: "주소록 동기화에 실패했습니다."
    } catch (c: CancellationException) {
        _chatHistory.update { history -> history.filterNot { it.id == pending.id } }
        throw c
    } catch (e: Exception) {
        "⚠️ ${e.message ?: "주소록 동기화 실패"}"
    }
    _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    syncNativeStateAsync()
}
