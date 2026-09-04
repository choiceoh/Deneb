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
    fun documentCaptureMimePdfAnnouncesPdf() {
        assertEquals("application/pdf", documentCaptureMime("contract.pdf"))
        assertEquals("application/pdf", documentCaptureMime("CONTRACT.PDF"))
    }

    @Test
    fun documentCaptureMimeTextAndCodeStayTextPlain() {
        // The gateway's isTextFile list is narrower than our text set, so code/text
        // files NEED the hint to be read as text rather than declined as unsupported.
        for (name in listOf("notes.txt", "README.md", "data.csv", "Main.kt", "app.py", "hooks.ts", "server.go")) {
            assertEquals("text/plain", documentCaptureMime(name), name)
        }
    }

    @Test
    fun documentCaptureMimeBinaryDocsSendNoHintSoGatewayRoutesByFilename() {
        // OOXML + the host-converted formats (HWP / legacy Office / ODF) must NOT be
        // labeled text/plain — that short-circuits the gateway to its plain-text
        // branch and reads the binary as garbage before the converter runs.
        for (name in listOf(
            "report.docx", "sheet.xlsx", "deck.pptx",
            "old.doc", "old.xls", "old.ppt", "memo.rtf",
            "doc.odt", "sheet.ods", "deck.odp",
            "계약서.hwp", "계약서.hwpx",
        )) {
            assertEquals("", documentCaptureMime(name), name)
        }
    }

    @Test
    fun stagingAcceptsAudioEvenWhenNotInTheSupportedDocumentSet() {
        // supportedFileExtensions (image + text + document) carries no audio, but the
        // picker offers audio for transcription — so staging must accept it, or a picked
        // recording is wrongly rejected (regression when the composer moved to staging).
        for (ext in listOf("m4a", "MP3", "wav", "ogg", "opus", "aac", "flac")) {
            assertTrue(isStageableExtension(ext, supportedFileExtensions), ext)
        }
    }

    @Test
    fun stagingIsTheOnlyTypeGateSoItCoversEveryOfferedFamily() {
        // The attach picker no longer passes an extension filter (QuestionInput):
        // FileKit would turn one into a SAF MIME allowlist, and a recording whose
        // provider declares application/octet-stream or audio/x-m4a then showed up
        // greyed out and untappable. With the picker open to every file, THIS guard is
        // the only type gate left — so it must accept every family the gateway's batch
        // capture can extract (image OCR / audio ASR / document conversion), not just
        // the ones that happened to survive the MIME mapping.
        for (ext in imageExtensions + audioExtensions + documentExtensions) {
            assertTrue(isStageableExtension(ext, supportedFileExtensions), ".$ext must stage")
        }
    }

    @Test
    fun stagingAcceptsSupportedDocsAndImagesButRejectsUnknownOrEmpty() {
        for (ext in listOf("docx", "pdf", "png", "txt", "hwp", "PDF")) {
            assertTrue(isStageableExtension(ext, supportedFileExtensions + "pdf"), ext)
        }
        for (ext in listOf("zip", "exe", "")) {
            assertEquals(false, isStageableExtension(ext, supportedFileExtensions), ext)
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
    fun attachmentSizeGuardCapsNonImagesButNotImages() {
        // Images are downsampled before upload, so the payload cap does not apply —
        // a 40MB photo is fine even though a 40MB PDF is not.
        assertTrue(isWithinAttachmentSize("png", 40_000_000L), "image")
        assertTrue(isWithinAttachmentSize("JPG", MAX_RAW_IMAGE_BYTES.toLong()), "image at ceiling, ci")
        // ...but the encoded file is still read into memory to decode it, so a
        // pathological one is refused instead of being pulled in whole.
        assertEquals(false, isWithinAttachmentSize("jpg", 999_000_000L), "pathological image")
        // Non-images are capped.
        assertTrue(isWithinAttachmentSize("pdf", 10_000_000L), "small pdf")
        assertEquals(false, isWithinAttachmentSize("pdf", MAX_ATTACHMENT_BYTES + 1), "oversize pdf")
        assertEquals(false, isWithinAttachmentSize("mp4", 50_000_000L), "oversize video")
        // Unknown size (failed stat) must not bypass the non-image cap.
        assertEquals(false, isWithinAttachmentSize("pdf", 0L), "unknown pdf size")
        assertEquals(false, isWithinAttachmentSize("pdf", -1L), "invalid pdf size")
    }

    @Test
    fun formatFileSizeIsHumanReadable() {
        assertEquals("40 B", formatFileSize(40))
        assertEquals("2 KB", formatFileSize(2_000))
        assertEquals("2.3 MB", formatFileSize(2_300_000))
    }
}
