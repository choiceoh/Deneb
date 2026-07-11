package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class TruncateFileNameTest {

    @Test
    fun namesAtOrBelowLimitAreReturnedUnchanged() {
        for (name in listOf("", "a", "report.pdf", "1234567890123456")) {
            assertEquals(name, truncateFileName(name, maxChars = 16), name)
        }
    }

    @Test
    fun extensionlessNameKeepsPrefixAndEllipsisWithinLimit() {
        val result = truncateFileName("very-long-document-name", maxChars = 10)

        assertEquals("very-long…", result)
        assertEquals(10, result.length)
    }

    @Test
    fun ordinaryExtensionIsPreservedExactly() {
        val result = truncateFileName("quarterly-financial-report.pdf", maxChars = 16)

        assertEquals("quarterly-f….pdf", result)
        assertEquals(16, result.length)
        assertTrue(result.endsWith(".pdf"))
    }

    @Test
    fun finalExtensionWinsForMultiDotName() {
        val result = truncateFileName("archive.release.notes.tar.gz", maxChars = 14)

        assertEquals("archive.re….gz", result)
        assertTrue(result.endsWith(".gz"))
        assertEquals(14, result.length)
    }

    @Test
    fun hiddenFileAndTrailingDotUseGenericTruncation() {
        assertEquals(".verylon…", truncateFileName(".verylonghiddenfile", maxChars = 9))
        assertEquals("filename…", truncateFileName("filename.with.trailing.", maxChars = 9))
    }

    @Test
    fun extensionLongerThanBudgetFallsBackToBoundedGenericForm() {
        val result = truncateFileName("a.extremelylongextension", maxChars = 8)

        assertEquals("a.extre…", result)
        assertEquals(8, result.length)
    }

    @Test
    fun tinyAndNonPositiveLimitsAreSafe() {
        assertEquals("", truncateFileName("filename.txt", maxChars = 0))
        assertEquals("", truncateFileName("filename.txt", maxChars = -1))
        assertEquals("…", truncateFileName("filename.txt", maxChars = 1))
        assertEquals("f…", truncateFileName("filename.txt", maxChars = 2))
    }

    @Test
    fun exactBudgetForExtensionKeepsOneBaseCharacter() {
        assertEquals("a….txt", truncateFileName("abcdef.txt", maxChars = 6))
    }

    @Test
    fun defaultLimitIsSixteenCharacters() {
        val result = truncateFileName("abcdefghijklmnopqrstuvwxyz.txt")

        assertEquals(16, result.length)
        assertEquals("abcdefghijk….txt", result)
    }

    @Test
    fun unicodeBaseNamePreservesExtensionAndValidLength() {
        val result = truncateFileName("회의록최종검토본수정완료.pdf", maxChars = 12)

        assertTrue(result.endsWith(".pdf"))
        assertEquals(12, result.length)
        assertTrue('…' in result)
    }

    @Test
    fun resultNeverExceedsPositiveBudgetAcrossNameShapes() {
        val names = listOf(
            "plain-very-long-file-name",
            "report.with.many.parts.pdf",
            ".hidden-configuration-file",
            "trailing-dot-name.",
            "a.extensionthatislongerthanmostbudgets",
            "한글파일이름이아주길어요.txt",
        )

        for (max in 1..20) {
            names.forEach { name ->
                val result = truncateFileName(name, max)
                assertTrue(result.length <= max, "name=$name max=$max result=$result")
            }
        }
    }

    @Test
    fun increasingBudgetEventuallyReturnsOriginalName() {
        val name = "quarterly-report.final.pdf"

        assertTrue(truncateFileName(name, name.length - 1) != name)
        assertEquals(name, truncateFileName(name, name.length))
        assertEquals(name, truncateFileName(name, name.length + 1))
    }
}
