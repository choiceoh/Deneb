package ai.deneb.deneb

import ai.deneb.contacts.ContactData
import ai.deneb.ui.chat.History
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
    _chatHistory.update { it + History(role = History.Role.USER, content = label) }
    val reply = runCatching {
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
    }.getOrElse { "⚠️ ${it.message ?: "이미지 캡처 실패"}" }
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
    _chatHistory.update { it + History(role = History.Role.USER, content = "🎙️ 녹음 공유됨 (전사·회의록 분석 중…)") }
    val reply = runCatching {
        val payload = callRpc<CaptureAudioPayload>(
            "miniapp.capture.audio",
            buildJsonObject {
                put("audio", Base64.encode(bytes))
                put("mimeType", mimeType)
                put("sessionKey", sessionKey)
            },
        )
        payload?.text?.ifBlank { null } ?: "녹음에서 음성을 인식하지 못했거나 전사에 실패했습니다."
    }.getOrElse { "⚠️ ${it.message ?: "녹음 캡처 실패"}" }
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
    _chatHistory.update { it + History(role = History.Role.USER, content = label) }
    val reply = runCatching {
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
    }.getOrElse { "⚠️ ${it.message ?: "문서 캡처 실패"}" }
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
    _chatHistory.update { it + History(role = History.Role.USER, content = "📇 주소록 ${contacts.size}개 동기화 중…") }
    val reply = runCatching {
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
    }.getOrElse { "⚠️ ${it.message ?: "주소록 동기화 실패"}" }
    _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    syncNativeStateAsync()
}
