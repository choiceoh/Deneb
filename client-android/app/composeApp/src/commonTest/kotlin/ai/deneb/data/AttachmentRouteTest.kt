package ai.deneb.data

import kotlin.test.Test
import kotlin.test.assertEquals

class AttachmentRouteTest {

    @Test
    fun imagesRouteToOcrWhenCapturesAvailable() {
        for (ext in listOf("jpg", "JPG", "jpeg", "png", "webp", "heic", "gif")) {
            assertEquals(AttachmentRoute.IMAGE_CAPTURE, routeAttachment(ext, capturesAvailable = true), ext)
        }
    }

    @Test
    fun audioRoutesToTranscriptionWhenCapturesAvailable() {
        for (ext in listOf("m4a", "MP3", "wav", "ogg", "oga", "opus", "aac", "flac")) {
            assertEquals(AttachmentRoute.AUDIO_CAPTURE, routeAttachment(ext, capturesAvailable = true), ext)
        }
    }

    @Test
    fun documentsAndUnknownAttach() {
        for (ext in listOf("pdf", "docx", "txt", "csv", "xlsx", "")) {
            assertEquals(AttachmentRoute.FILE_ATTACH, routeAttachment(ext, capturesAvailable = true), ext)
        }
    }

    @Test
    fun withoutCapturesEverythingAttaches() {
        // Desktop/iOS (no capture launchers): images and audio attach like any file.
        for (ext in listOf("jpg", "png", "m4a", "mp3", "pdf", "txt")) {
            assertEquals(AttachmentRoute.FILE_ATTACH, routeAttachment(ext, capturesAvailable = false), ext)
        }
    }
}
