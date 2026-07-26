package ai.deneb.ui.markdown

import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertNull
import kotlin.test.assertSame
import kotlin.test.assertTrue

/**
 * 스트리밍 캐럿의 계약: 캐럿은 **문서의 최전선** 인라인 런 하나에만 붙는다.
 *
 * 예전에는 파스 소스에 "▍"를 덧붙였고, 그래서 미완결 펜스 안에서는 생략하고 펜스
 * 라인 뒤에서는 줄을 바꾸는 방어 로직이 필요했다 (그 주석 스스로가 "CommonMark의
 * 근사"라고 불렀다). 이제는 레이아웃에서 그리므로 오염시킬 텍스트가 없다 — 대신
 * "어느 런이 최전선인가"가 유일한 계약이고, 이 테스트가 그걸 고정한다.
 *
 * 반환은 **동일 인스턴스**여야 한다: [InlineContent]가 `===`로 자기 자신인지
 * 판별하므로, 내용만 같은 새 리스트를 돌려주면 캐럿이 조용히 사라진다.
 */
class StreamCaretTailTest {
    private fun para(text: String) = Paragraph(persistentListOf(Text(text)))

    private fun doc(vararg blocks: BlockNode) = MarkdownDocument(persistentListOf(*blocks))

    @Test
    fun theLastParagraphOwnsTheCaret() {
        val tail = para("정리하면 이렇습니다")
        val document = doc(para("확인해봤습니다"), tail)
        assertSame(tail.inlines, document.streamCaretTail())
    }

    @Test
    fun anUnclosedFenceAtTheFrontierGetsNoCaret() {
        // 코드가 흐르는 동안엔 캐럿이 없다. 앞 문단으로 물러나면 커서가 메시지
        // 한가운데 박혀 "멈춘 스트림"처럼 읽히므로, 없는 편이 옳다.
        val document = doc(para("이렇게 실행하세요"), CodeFence(language = "kotlin", code = "val x = 1", closed = false))
        assertNull(document.streamCaretTail())
    }

    @Test
    fun aTableOrDisplayMathFrontierAlsoGetsNoCaret() {
        assertNull(doc(para("표입니다"), DisplayMath("x^2")).streamCaretTail())
        assertNull(doc(para("표입니다"), HorizontalRule).streamCaretTail())
    }

    @Test
    fun theCaretDescendsIntoTheLastListItem() {
        val tail = para("셋째 항목")
        val document = doc(
            para("목록입니다"),
            BulletList(
                items = persistentListOf(
                    ListItem(persistentListOf(para("첫째 항목"))),
                    ListItem(persistentListOf(tail)),
                ),
                tight = true,
            ),
        )
        assertSame(tail.inlines, document.streamCaretTail())
    }

    @Test
    fun theCaretDescendsIntoAQuote() {
        val tail = para("인용 마지막 줄")
        assertSame(tail.inlines, doc(Blockquote(persistentListOf(para("인용 첫 줄"), tail))).streamCaretTail())
    }

    @Test
    fun aHeadingFrontierOwnsTheCaret() {
        val heading = Heading(level = 2, inlines = persistentListOf(Text("정리")))
        assertSame(heading.inlines, doc(para("본문"), heading).streamCaretTail())
    }

    @Test
    fun anEmptyRunOrEmptyDocumentGetsNoCaret() {
        assertNull(MarkdownDocument(persistentListOf()).streamCaretTail())
        assertNull(doc(Paragraph(persistentListOf())).streamCaretTail())
    }

    @Test
    fun onlyTheFrontierRunMatchesEvenWhenTheWordsRepeat() {
        // 동일 인스턴스 판별이라야 같은 문장이 두 번 나올 때 캐럿이 둘 다 켜지지 않는다.
        val first = para("같은 문장")
        val tail = para("같은 문장")
        val caret = doc(first, tail).streamCaretTail()
        assertSame(tail.inlines, caret)
        assertTrue(caret !== first.inlines, "구조적으로 같아도 앞 문단은 최전선이 아니다")
    }
}
