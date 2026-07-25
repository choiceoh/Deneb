package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * The collapsed reasoning row's live line is the first non-empty line of the LAST
 * segment — "last" so the row moves as each new reasoning phase starts, "first
 * non-empty" so a segment that opens with blank lines still shows something.
 *
 * This line is shown ONLY while the turn streams. Once the turn finishes it is a
 * frozen fragment of raw model reasoning — often mid-sentence and in the model's
 * own language rather than the product's — so the finished row shows the step
 * count instead (see ReasoningBlockquote).
 */
class ReasoningPreviewLineTest {
    @Test
    fun takesFirstNonEmptyLineOfTheLastSegment() {
        val segments = listOf("첫 단계\n둘째 줄", "\n\n  최근 단계  \n다음 줄")
        assertEquals("최근 단계", reasoningPreviewLine(segments))
    }

    @Test
    fun emptyInputsYieldNoLine() {
        val cases = mapOf(
            "no segments" to emptyList(),
            "blank segment" to listOf("   \n\n  "),
            "empty string" to listOf(""),
        )
        for ((name, segments) in cases) {
            assertEquals("", reasoningPreviewLine(segments), name)
        }
    }

    @Test
    fun singleSegmentUsesItsOwnFirstLine() {
        assertEquals("확인할게요", reasoningPreviewLine(listOf("확인할게요\n그 다음")))
    }
}
