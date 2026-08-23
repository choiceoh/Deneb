package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserDownloadTest {
    @Test
    fun contentDispositionWinsOverTheUrl() {
        assertEquals(
            "견적서.pdf",
            browserDownloadFileName(
                "https://x.com/d?id=99",
                "attachment; filename=\"견적서.pdf\"",
                "application/pdf",
            ),
        )
    }

    @Test
    fun decodesRfc6266ExtendedFilename() {
        // Korean attachment names arrive percent-encoded via filename*=.
        assertEquals(
            "계약서 최종.pdf",
            browserDownloadFileName(
                "https://x.com/d",
                "attachment; filename*=UTF-8''%EA%B3%84%EC%95%BD%EC%84%9C%20%EC%9C%B5%EC%A2%85.pdf"
                    .replace("%EC%9C%B5%EC%A2%85", "%EC%B5%9C%EC%A2%85"),
                "application/pdf",
            ),
        )
    }

    @Test
    fun fallsBackToTheUrlPathSegment() {
        assertEquals(
            "report.xlsx",
            browserDownloadFileName("https://x.com/files/report.xlsx?token=abc#p", null, null),
        )
    }

    @Test
    fun fallsBackToMimeWhenNothingNameable() {
        assertEquals("download.pdf", browserDownloadFileName("https://x.com/d", null, "application/pdf"))
        assertEquals(
            "download.xlsx",
            browserDownloadFileName(
                "https://x.com/d",
                null,
                "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet; charset=binary",
            ),
        )
        assertEquals("download", browserDownloadFileName("https://x.com/d", null, null))
    }

    @Test
    fun bareHostIsNotTreatedAsAFilename() {
        // "example.com" has a dot but is a host, not a file.
        assertEquals("download.pdf", browserDownloadFileName("https://example.com", null, "application/pdf"))
    }

    @Test
    fun stripsPathTraversalAndSeparators() {
        val name = browserDownloadFileName("https://x.com/d", "attachment; filename=\"../../etc/passwd\"", null)
        assertFalse(name.contains('/'), name)
        assertFalse(name.contains(".."), name)
        assertTrue(name.isNotBlank())
    }

    @Test
    fun nulBecomesAnUnderscoreLikeOtherUnsafeChars() {
        // The source declares NUL as '\u0000' so file tools do not misread the
        // file as binary; the code point itself must sanitize like any other
        // unsafe character.
        assertEquals("a_b", sanitizeFileName("a\u0000b"))
    }

    @Test
    fun neverReturnsBlank() {
        assertEquals("download", sanitizeFileName("   "))
        assertEquals("download", sanitizeFileName("..."))
        assertEquals("download", sanitizeFileName("///"))
    }

    @Test
    fun capsAbsurdlyLongNames() {
        val long = "a".repeat(500) + ".pdf"
        assertTrue(browserDownloadFileName("https://x.com/d", "attachment; filename=\"$long\"", null).length <= 120)
    }
}
