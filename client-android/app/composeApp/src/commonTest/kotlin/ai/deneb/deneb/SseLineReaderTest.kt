package ai.deneb.deneb

import io.ktor.utils.io.ByteReadChannel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class SseLineReaderTest {

    private suspend fun read(bytes: ByteArray): List<String> {
        val lines = mutableListOf<String>()
        readSseLines(ByteReadChannel(bytes)) { lines += it }
        return lines
    }

    private suspend fun read(text: String): List<String> = read(text.encodeToByteArray())

    @Test
    fun emptyChannelEmitsNothing() = runTest {
        assertEquals(emptyList(), read(byteArrayOf()))
    }

    @Test
    fun unterminatedFinalLineIsEmittedAtEof() = runTest {
        assertEquals(listOf("data: final"), read("data: final"))
    }

    @Test
    fun newlineTerminatedLineIsEmittedOnce() = runTest {
        assertEquals(listOf("data: one"), read("data: one\n"))
    }

    @Test
    fun trailingNewlineDoesNotInventAnAdditionalLine() = runTest {
        assertEquals(listOf("one", "two"), read("one\ntwo\n"))
    }

    @Test
    fun blankLinesInsideStreamArePreservedForSseFraming() = runTest {
        assertEquals(listOf("event: message", "", "data: body"), read("event: message\n\ndata: body\n"))
    }

    @Test
    fun oneBareNewlineRepresentsOneEmptyLine() = runTest {
        assertEquals(listOf(""), read("\n"))
    }

    @Test
    fun consecutiveBareNewlinesRepresentConsecutiveEmptyLines() = runTest {
        assertEquals(listOf("", "", ""), read("\n\n\n"))
    }

    @Test
    fun crlfDropsOnlyTheTransportCarriageReturn() = runTest {
        assertEquals(listOf("data: first", "data: second"), read("data: first\r\ndata: second\r\n"))
    }

    @Test
    fun unterminatedCarriageReturnIsAlsoNormalized() = runTest {
        assertEquals(listOf("data: body"), read("data: body\r"))
    }

    @Test
    fun repeatedCarriageReturnsDropOnlyTheLastOne() = runTest {
        assertEquals(listOf("value\r"), read("value\r\r\n"))
    }

    @Test
    fun multibyteKoreanSurvivesPerByteReading() = runTest {
        val text = "data: 발신인 이력을 대조했습니다"

        assertEquals(listOf(text), read("$text\n"))
    }

    @Test
    fun fourByteEmojiSurvivesAtLineEnd() = runTest {
        assertEquals(listOf("data: done 🚀"), read("data: done 🚀\n"))
    }

    @Test
    fun mixedAsciiUnicodeAndEmptyFramesKeepOrder() = runTest {
        val source = "event: tool\ndata: {\"detail\":\"메일 확인\"}\n\nevent: delta\ndata: 안녕 👋"

        assertEquals(
            listOf("event: tool", "data: {\"detail\":\"메일 확인\"}", "", "event: delta", "data: 안녕 👋"),
            read(source),
        )
    }

    @Test
    fun leadingTrailingSpacesAndTabsAreNotTrimmed() = runTest {
        assertEquals(listOf("  data: x  ", "\tcomment\t"), read("  data: x  \n\tcomment\t\n"))
    }

    @Test
    fun nulBytesRemainPartOfLinePayload() = runTest {
        val lines = read(byteArrayOf('a'.code.toByte(), 0, 'b'.code.toByte(), '\n'.code.toByte()))

        assertEquals(1, lines.size)
        assertContentEquals(charArrayOf('a', '\u0000', 'b'), lines.single().toCharArray())
    }

    @Test
    fun utf8BomIsPreservedForCallerPolicy() = runTest {
        val bom = byteArrayOf(0xEF.toByte(), 0xBB.toByte(), 0xBF.toByte())
        val bytes = bom + "data: x\n".encodeToByteArray()

        assertEquals(listOf("\uFEFFdata: x"), read(bytes))
    }

    @Test
    fun malformedUtf8UsesDecoderReplacementWithoutCrashingStream() = runTest {
        val bytes = byteArrayOf('a'.code.toByte(), 0xFF.toByte(), 'b'.code.toByte(), '\n'.code.toByte())

        val line = read(bytes).single()

        assertTrue(line.startsWith("a"))
        assertTrue(line.endsWith("b"))
        assertTrue('\uFFFD' in line)
    }

    @Test
    fun veryLongLineIsDeliveredAsOneCompleteValue() = runTest {
        val payload = "한글abc🚀".repeat(20_000)

        val lines = read("$payload\n")

        assertEquals(1, lines.size)
        assertEquals(payload, lines.single())
    }

    @Test
    fun callbackExceptionPropagatesAndStopsFurtherDelivery() = runTest {
        val delivered = mutableListOf<String>()
        val failure = assertFailsWith<IllegalStateException> {
            readSseLines(ByteReadChannel("one\ntwo\nthree\n".encodeToByteArray())) { line ->
                delivered += line
                if (line == "two") error("consumer failed")
            }
        }

        assertEquals("consumer failed", failure.message)
        assertEquals(listOf("one", "two"), delivered)
    }

    @Test
    fun callbackCancellationPropagatesRatherThanLookingLikeEof() = runTest {
        val failure = assertFailsWith<CancellationException> {
            readSseLines(ByteReadChannel("data: stop\n".encodeToByteArray())) {
                throw CancellationException("cancel consumer")
            }
        }

        assertEquals("cancel consumer", failure.message)
    }

    @Test
    fun eofAfterEmptySeparatorDoesNotRepeatSeparator() = runTest {
        assertEquals(listOf("data: x", ""), read("data: x\n\n"))
    }
}
