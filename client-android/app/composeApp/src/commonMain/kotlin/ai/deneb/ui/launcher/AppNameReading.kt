package ai.deneb.ui.launcher

import ai.deneb.ui.text.HANGUL_ORDER
import ai.deneb.ui.text.hangulInitial
import ai.deneb.ui.text.sectionKeyOf

/**
 * Korean-phonetic reading for an app label, so the drawer's ㄱㄴㄷ scrub and the 자체앱
 * monograms file a Latin-branded app under its Korean 초성 (YouTube→유튜브→ㅇ,
 * Gmail→지메일→ㅈ) instead of a stray A–Z tail — the unified Hangul fast-scroll a Korean
 * user reaches for (the Niagara idiom). The label is still *displayed* verbatim
 * ("YouTube"); only the index/monogram is transliterated. Korean labels pass through;
 * digits/symbols stay under #.
 *
 * Two tiers: a curated reading map ([appReadings]) for common apps — and for the cases
 * where the first letter misleads (Gmail's G is ㅈ, the X app is ㅇ) — then a
 * letter→초성 fallback ([latinInitialJamo]) for the long tail. Korean transliteration
 * almost always keeps the initial consonant, so S→ㅅ, C→ㅋ, N→ㄴ … is right far more
 * often than not; the curated map carries the exceptions.
 */

// Latin first letter → the Korean 초성 its transliteration usually starts with. Covers
// the long tail of un-curated apps so the index stays purely Hangul.
private val latinInitialJamo: Map<Char, Char> = mapOf(
    'a' to 'ㅇ', 'b' to 'ㅂ', 'c' to 'ㅋ', 'd' to 'ㄷ', 'e' to 'ㅇ', 'f' to 'ㅍ',
    'g' to 'ㄱ', 'h' to 'ㅎ', 'i' to 'ㅇ', 'j' to 'ㅈ', 'k' to 'ㅋ', 'l' to 'ㄹ',
    'm' to 'ㅁ', 'n' to 'ㄴ', 'o' to 'ㅇ', 'p' to 'ㅍ', 'q' to 'ㅋ', 'r' to 'ㄹ',
    's' to 'ㅅ', 't' to 'ㅌ', 'u' to 'ㅇ', 'v' to 'ㅂ', 'w' to 'ㅇ', 'x' to 'ㅅ',
    'y' to 'ㅇ', 'z' to 'ㅈ',
)

// Curated Korean readings for common Korean-market apps that ship a Latin label. Used to
// pick the right 초성 (especially the first-letter exceptions like gmail/x) and to let
// search match the Korean reading. Keyed by the lowercased Latin label. Extend freely —
// anything absent still gets a Hangul bucket via [latinInitialJamo].
private val appReadings: Map<String, String> = mapOf(
    "youtube" to "유튜브", "chrome" to "크롬", "gmail" to "지메일", "google" to "구글",
    "instagram" to "인스타그램", "facebook" to "페이스북", "messenger" to "메신저",
    "whatsapp" to "왓츠앱", "netflix" to "넷플릭스", "spotify" to "스포티파이",
    "twitter" to "트위터", "tiktok" to "틱톡", "telegram" to "텔레그램",
    "discord" to "디스코드", "slack" to "슬랙", "zoom" to "줌", "line" to "라인",
    "uber" to "우버", "paypal" to "페이팔", "linkedin" to "링크드인",
    "snapchat" to "스냅챗", "reddit" to "레딧", "pinterest" to "핀터레스트",
    "notion" to "노션", "canva" to "캔바", "threads" to "스레드", "x" to "엑스",
)

/** The curated Korean reading for [label], if any (lowercased lookup). Lets search match
 *  a Korean reading typed for a Latin-branded app (유튜브 → "YouTube"). */
fun appKoreanReading(label: String): String? = appReadings[label.trim().lowercase()]

/**
 * A sort/section key for an app [label] to feed [sectionKeyOf]/koreanTextSections: the
 * Korean reading (so a Latin-brand app files under its 초성), a jamo-prefixed romaji for
 * an un-curated Latin app (keeps A–Z order *within* the Hangul bucket), or the label
 * verbatim for Korean/symbol labels.
 */
fun koreanAppSortKey(label: String): String {
    val t = label.trim()
    val first = t.firstOrNull() ?: return t
    // Already Korean (syllable or standalone jamo) → leave as-is; sectionKeyOf buckets it.
    if (hangulInitial(first) != null || first in HANGUL_ORDER) return t
    val lower = first.lowercaseChar()
    if (lower in 'a'..'z') {
        appReadings[t.lowercase()]?.let { return it }
        latinInitialJamo[lower]?.let { return "$it${t.lowercase()}" }
    }
    return t // digits/symbols → # via sectionKeyOf
}

/** The single-glyph initial (Korean 초성 / # ) for an app [label] — the 자체앱 monogram,
 *  matching the bucket the drawer's scrub sorts it into. */
fun appInitial(label: String): String = sectionKeyOf(koreanAppSortKey(label))
