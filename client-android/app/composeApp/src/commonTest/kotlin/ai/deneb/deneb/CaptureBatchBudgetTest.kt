package ai.deneb.deneb

import ai.deneb.data.MAX_BATCH_BYTES
import ai.deneb.data.MAX_BATCH_FILES
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * The memory budget that keeps a capture batch buildable.
 *
 * captureBatch holds raw bytes + base64 + the assembled JSON body at once, so an
 * unbounded batch peaks at several times its raw size — build 838 died on a 105MB
 * allocation inside the request serializer. These pin the guard's shape: bounded,
 * in order, and honest about what it left out.
 */
class CaptureBatchBudgetTest {

    private fun attachment(name: String, bytes: Int) = DenebAttachment(ByteArray(bytes), name, "application/pdf")

    @Test
    fun keepsEverythingWhenTheBatchFitsTheBudget() {
        val files = listOf(attachment("a.pdf", 10), attachment("b.pdf", 20))

        val fit = fitCaptureBatch(files)

        assertEquals(listOf("a.pdf", "b.pdf"), fit.accepted.map { it.filename })
        assertTrue(fit.dropped.isEmpty())
    }

    @Test
    fun dropsByNameOnceTheByteBudgetIsSpent() {
        val half = (MAX_BATCH_BYTES / 2).toInt()
        val files = listOf(
            attachment("first.pdf", half),
            attachment("second.pdf", half),
            attachment("third.pdf", half),
        )

        val fit = fitCaptureBatch(files)

        // In order: the first two fill the budget, the third is reported, not silently cut.
        assertEquals(listOf("first.pdf", "second.pdf"), fit.accepted.map { it.filename })
        assertEquals(listOf("third.pdf"), fit.dropped)
    }

    @Test
    fun dropsASingleFileTooLargeForTheWholeBudget() {
        val fit = fitCaptureBatch(listOf(attachment("huge.pdf", (MAX_BATCH_BYTES + 1).toInt())))

        assertTrue(fit.accepted.isEmpty())
        assertEquals(listOf("huge.pdf"), fit.dropped)
    }

    @Test
    fun capsTheFileCountAcrossRepeatedPicks() {
        // The picker limits one pick to MAX_BATCH_FILES, but staging is cumulative,
        // so the cap has to hold here too.
        val files = (1..MAX_BATCH_FILES + 3).map { attachment("f$it.pdf", 1) }

        val fit = fitCaptureBatch(files)

        assertEquals(MAX_BATCH_FILES, fit.accepted.size)
        assertEquals(3, fit.dropped.size)
    }

    @Test
    fun skipsUnreadableFilesWithoutReportingThemAsOversized() {
        // Empty bytes mean the read failed, not that the file was too big — the
        // caller reports that, so naming it here would double up the message.
        val fit = fitCaptureBatch(listOf(attachment("good.pdf", 4), attachment("unreadable.pdf", 0)))

        assertEquals(listOf("good.pdf"), fit.accepted.map { it.filename })
        assertTrue(fit.dropped.isEmpty())
    }

    @Test
    fun namesABlankFilenameSoTheMessageIsStillReadable() {
        val fit = fitCaptureBatch(
            listOf(attachment("   ", (MAX_BATCH_BYTES + 1).toInt())),
        )

        assertEquals(listOf("첨부"), fit.dropped)
    }
}
