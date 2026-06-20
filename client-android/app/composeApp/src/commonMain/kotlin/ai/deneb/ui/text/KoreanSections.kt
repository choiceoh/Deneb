package ai.deneb.ui.text

/**
 * Korean-first alphabetical sectioning for long text lists (app drawer, contacts):
 * groups items under their initial — Hangul 초성 (ㄱ/ㄴ/…), Latin (A–Z), or # —
 * label-sorted within each, sections ordered Hangul → Latin → #. Domain-free; the
 * caller supplies a label extractor. Pairs with the ScrubIndex in [SectionedScrubList].
 */

private const val HANGUL_INITIALS = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ"

/** The 14 basic initials, in dictionary order — also the membership test for
 *  standalone basic jamo and the rank source. */
internal const val HANGUL_ORDER = "ㄱㄴㄷㄹㅁㅂㅅㅇㅈㅊㅋㅌㅍㅎ"

/** The compact-index initial consonant of a Hangul syllable, or null if [c] isn't
 *  one. Double consonants fold to their base (ㄲ→ㄱ …) so the index stays 14 letters. */
fun hangulInitial(c: Char): Char? {
    if (c.code < 0xAC00 || c.code > 0xD7A3) return null
    return when (val raw = HANGUL_INITIALS[(c.code - 0xAC00) / 588]) {
        'ㄲ' -> 'ㄱ'
        'ㄸ' -> 'ㄷ'
        'ㅃ' -> 'ㅂ'
        'ㅆ' -> 'ㅅ'
        'ㅉ' -> 'ㅈ'
        else -> raw
    }
}

/** Section key for a label: Hangul initial (syllable or standalone basic jamo), else
 *  uppercase Latin, else # — so CJK/accented/symbol labels share one bucket whose key
 *  matches their #-rank (otherwise each formed an orphan section colliding at rank). */
fun sectionKeyOf(label: String): String {
    val c = label.trimStart().firstOrNull() ?: return "#"
    hangulInitial(c)?.let { return it.toString() }
    if (c in HANGUL_ORDER) return c.toString()
    return if (c in 'A'..'Z' || c in 'a'..'z') c.uppercaseChar().toString() else "#"
}

/** Sort order: Hangul (ㄱ→ㅎ), then Latin (A→Z), then # (digits/symbols) last. */
fun sectionRank(key: String): Int {
    val c = key[0]
    val h = HANGUL_ORDER.indexOf(c)
    if (h >= 0) return h
    if (c in 'A'..'Z') return 100 + (c - 'A')
    return 1000
}

/** Items grouped under one initial, label-sorted. */
data class TextSection<T>(val key: String, val items: List<T>)

/** Group [items] into [TextSection]s by initial, sections ranked and items
 *  label-sorted within each. */
fun <T> koreanTextSections(items: List<T>, label: (T) -> String): List<TextSection<T>> = items.groupBy { sectionKeyOf(label(it)) }
    .toList()
    .sortedBy { (key, _) -> sectionRank(key) }
    .map { (key, list) -> TextSection(key, list.sortedBy { label(it).lowercase() }) }
