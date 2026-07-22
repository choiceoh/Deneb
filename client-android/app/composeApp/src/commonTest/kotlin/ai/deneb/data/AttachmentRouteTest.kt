package ai.deneb.data

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

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
    fun richDocumentFormatsArePickableAndAttach() {
        // Office / HWP / ODF the gateway extracts or converts must be offered by the
        // in-app picker (before this they were share-sheet only) and route to the
        // document-capture path.
        for (ext in listOf("doc", "xls", "ppt", "rtf", "odt", "ods", "odp", "hwp", "hwpx")) {
            assertTrue(ext in supportedFileExtensions, "picker should offer .$ext")
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

    @Test
    fun classifyFileUsesRecognizedMimeTypes() {
        assertEquals(FileCategory.IMAGE, classifyFile("image/avif", "file.bin"))
        assertEquals(FileCategory.PDF, classifyFile("application/pdf", "file.bin"))
        assertEquals(FileCategory.TEXT, classifyFile("text/csv", "file.bin"))
        assertEquals(FileCategory.TEXT, classifyFile("application/x-yaml", "file.bin"))
    }

    @Test
    fun classifyFilePrefersMimeTypeOverConflictingExtension() {
        assertEquals(FileCategory.IMAGE, classifyFile("image/png", "notes.txt"))
        assertEquals(FileCategory.TEXT, classifyFile("text/plain", "scan.pdf"))
        assertEquals(FileCategory.PDF, classifyFile("application/pdf", "photo.jpg"))
    }

    @Test
    fun classifyFileFallsBackToCaseInsensitiveExtensions() {
        assertEquals(FileCategory.IMAGE, classifyFile("application/octet-stream", "PHOTO.HEIC"))
        assertEquals(FileCategory.TEXT, classifyFile(null, "README.MD"))
        assertEquals(FileCategory.PDF, classifyFile(null, "contract.PDF"))
    }

    @Test
    fun classifyFileRejectsUnknownOrMissingTypes() {
        assertEquals(FileCategory.UNSUPPORTED, classifyFile(null, null))
        assertEquals(FileCategory.UNSUPPORTED, classifyFile(null, "archive.zip"))
        assertEquals(FileCategory.UNSUPPORTED, classifyFile("application/octet-stream", "archive.zip"))
    }
}
