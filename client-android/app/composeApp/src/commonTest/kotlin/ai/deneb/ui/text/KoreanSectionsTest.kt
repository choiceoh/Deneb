package ai.deneb.ui.text

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class KoreanSectionsTest {

    private data class Row(val id: Int, val label: String)

    @Test
    fun hangulInitialCoversTheSyllableRange() {
        val cases = mapOf(
            '가' to 'ㄱ',
            '깎' to 'ㄱ',
            '나' to 'ㄴ',
            '다' to 'ㄷ',
            '따' to 'ㄷ',
            '라' to 'ㄹ',
            '마' to 'ㅁ',
            '바' to 'ㅂ',
            '빠' to 'ㅂ',
            '사' to 'ㅅ',
            '싸' to 'ㅅ',
            '아' to 'ㅇ',
            '자' to 'ㅈ',
            '짜' to 'ㅈ',
            '차' to 'ㅊ',
            '카' to 'ㅋ',
            '타' to 'ㅌ',
            '파' to 'ㅍ',
            '하' to 'ㅎ',
            '힣' to 'ㅎ',
        )

        cases.forEach { (syllable, initial) ->
            assertEquals(initial, hangulInitial(syllable), syllable.toString())
        }
    }

    @Test
    fun hangulInitialRejectsNonSyllablesAndBoundaryNeighbors() {
        for (char in listOf('ힰ', 'ㄱ', 'A', '1', '中', 'é')) {
            assertNull(hangulInitial(char), char.toString())
        }
    }

    @Test
    fun sectionKeyTrimsLeadingSpaceAndFoldsCase() {
        assertEquals("ㄱ", sectionKeyOf("  김민준"))
        assertEquals("A", sectionKeyOf("alpha"))
        assertEquals("Z", sectionKeyOf("  Zoom"))
        assertEquals("ㅅ", sectionKeyOf("ㅅ standalone"))
    }

    @Test
    fun sectionKeyUsesCatchAllForBlankNumericSymbolAndNonLatinLabels() {
        for (label in listOf("", "   ", "123", "_hidden", "éclair", "中文", "ㄲ")) {
            assertEquals("#", sectionKeyOf(label), label)
        }
    }

    @Test
    fun sectionRankOrdersHangulThenLatinThenCatchAll() {
        val hangul = HANGUL_ORDER.map { sectionRank(it.toString()) }
        val latin = ('A'..'Z').map { sectionRank(it.toString()) }

        assertEquals(hangul.sorted(), hangul)
        assertEquals(latin.sorted(), latin)
        assertTrue(hangul.max() < latin.min())
        assertTrue(latin.max() < sectionRank("#"))
        assertEquals(sectionRank("#"), sectionRank("unknown"))
    }

    @Test
    fun koreanTextSectionsOrdersSectionsAndRows() {
        val rows = listOf(
            Row(1, "Zoom"),
            Row(2, "김철수"),
            Row(3, "alpha"),
            Row(4, "나성호"),
            Row(5, "강민지"),
            Row(6, "123상사"),
            Row(7, "Beta"),
            Row(8, "_system"),
        )

        val sections = koreanTextSections(rows, Row::label)

        assertEquals(listOf("ㄱ", "ㄴ", "A", "B", "Z", "#"), sections.map { it.key })
        assertEquals(listOf("강민지", "김철수"), sections.first().items.map { it.label })
        assertEquals(listOf("123상사", "_system"), sections.last().items.map { it.label })
    }

    @Test
    fun rowSortingIsCaseInsensitiveAndStableForEqualLabels() {
        val rows = listOf(
            Row(1, "apple"),
            Row(2, "Alpha"),
            Row(3, "APPLE"),
        )

        val section = koreanTextSections(rows, Row::label).single()

        assertEquals("A", section.key)
        assertEquals(listOf(2, 1, 3), section.items.map { it.id })
    }

    @Test
    fun emptyInputProducesNoSections() {
        assertEquals(emptyList(), koreanTextSections(emptyList<String>()) { it })
    }
}
