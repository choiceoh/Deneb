package ai.deneb.ui.text

// Display-width estimate in "units": every char is 1 unit except East-Asian-wide
// ones (Hangul/Han/kana/full-width), which are 2 — so Korean text is measured by
// its real on-screen width, not its character count. Shared by the markdown and
// deneb-ui table renderers for column sizing.
internal fun displayUnits(s: String): Int = s.sumOf { c -> if (isEastAsianWide(c)) 2 else 1 }

// East-Asian "wide" approximation: Hangul (jamo + syllables), CJK ideographs and
// radicals through Yi (covers kana 3040–30FF and CJK symbols 3000–303F), CJK compat,
// and full-width forms. Good enough to size columns; not a full Unicode width table.
internal fun isEastAsianWide(c: Char): Boolean {
    val u = c.code
    return u in 0x1100..0x115F ||
        u in 0x2E80..0xA4CF ||
        u in 0xAC00..0xD7A3 ||
        u in 0xF900..0xFAFF ||
        u in 0xFE30..0xFE4F ||
        u in 0xFF00..0xFF60 ||
        u in 0xFFE0..0xFFE6
}
