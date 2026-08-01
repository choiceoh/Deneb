package ai.deneb.contacts

// Port of the gateway's NormalizePersonName (contacts/person_name.go) — the two
// MUST agree, because the gateway names a merge group and the device decides which
// raw contacts belong to it. Divergence here is not cosmetic: too loose and the
// apply links strangers, too strict and it links nothing. When the Go list of
// titles changes, change this one in the same commit.
private val personTitleSuffixes = listOf(
    "대표이사",
    "부사장", "본부장", "부회장",
    "부장", "차장", "과장", "대리", "사원", "주임", "팀장", "실장",
    "이사", "상무", "전무", "사장", "회장", "선임", "책임", "수석", "대표",
    "님", "씨", "군", "양",
)

private val nameSeparators = listOf("(", "（", "[", "<", "/", ",", "·")

/**
 * Reduce a display name to the address-book match key: cut at the first
 * affiliation separator, drop whitespace, peel trailing Korean titles without
 * shrinking below two characters, lowercase. Exact-key matching keeps short names
 * like "이수" distinct from "이수민".
 */
internal fun normalizePersonName(name: String): String {
    var s = name.trim()
    if (s.isEmpty()) return ""
    for (sep in nameSeparators) {
        val i = s.indexOf(sep)
        if (i >= 0) s = s.substring(0, i)
    }
    s = s.replace(" ", "").trim()
    while (true) {
        val hit = personTitleSuffixes.firstOrNull { s.endsWith(it) && s.length - it.length >= 2 }
            ?: break
        s = s.dropLast(hit.length)
    }
    return s.lowercase()
}

/** The address-book key, ignoring the '#' sort prefix some syncs prepend. */
internal fun personMatchKey(name: String): String = normalizePersonName(name.trimStart('#', '＃'))

/** Undo the first-syllable doubling a sync sometimes injects ("홍홍정우" → "홍정우"). */
internal fun dedoubleKey(key: String): String = if (key.length >= 3 && key[0] == key[1]) key.substring(1) else key

/**
 * Whether [candidate] (a device display name) can be one of [groupNames] — the
 * same conservative bridges the gateway uses: equal keys, plus first-syllable
 * doubling. An empty key (a title-only card like "부장") matches nothing; letting
 * it bridge is what fuses two strangers who each share a number with it.
 */
internal fun nameBelongsToGroup(candidate: String, groupKeys: Set<String>): Boolean {
    val k = personMatchKey(candidate)
    if (k.isEmpty() || groupKeys.isEmpty()) return false
    return k in groupKeys || dedoubleKey(k) in groupKeys
}

/** Match keys for a merge group's member names, empty keys dropped. */
internal fun groupMatchKeys(names: List<String>): Set<String> = names.asSequence()
    .flatMap {
        val k = personMatchKey(it)
        sequenceOf(k, dedoubleKey(k))
    }
    .filter { it.isNotEmpty() }
    .toSet()
