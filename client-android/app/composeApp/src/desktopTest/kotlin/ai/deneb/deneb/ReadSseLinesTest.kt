package ai.deneb.deneb

import io.ktor.utils.io.ByteReadChannel
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals

// Regression: native AI responses (and proactive pushes) showed `�` at Korean
// positions because the old SSE reader (Ktor readUTF8Line) decoded multibyte chars
// per network segment. readSseLines consumes bytes one at a time (readByte) and must
// reassemble a line's raw bytes before decoding — so this exercises the exact failure
// mode even from a whole-array channel.
class ReadSseLinesTest {

    @Test
    fun `reassembles multibyte characters instead of corrupting them`() = runTest {
        val payload = "DSpark는 오픈웨이트 좋다" // 3-byte Hangul that used to break into `�`
        val text = "event: delta\ndata: {\"delta\":\"$payload\"}\n\n"
        val lines = mutableListOf<String>()
        readSseLines(ByteReadChannel(text.encodeToByteArray())) { lines.add(it) }
        assertEquals(
            listOf("event: delta", "data: {\"delta\":\"$payload\"}", ""),
            lines,
        )
    }

    @Test
    fun `strips trailing CR and flushes an unterminated final line`() = runTest {
        val lines = mutableListOf<String>()
        readSseLines(ByteReadChannel("가\r\n나".encodeToByteArray())) { lines.add(it) }
        // CRLF normalized to "가"; the tail "나" (no trailing newline) is still flushed.
        assertEquals(listOf("가", "나"), lines)
    }
}
