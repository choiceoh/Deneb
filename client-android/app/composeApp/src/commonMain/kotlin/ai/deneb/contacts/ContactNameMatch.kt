package ai.deneb.contacts

// Mirrors gateway-go/internal/domain/contacts/person_name.go so apply-time
// matching stays aligned with the dedup mirror contract.
internal fun normalizePersonName(name: String): String {
    var normalized = name.trim().trimStart('#', '＃')
    if (normalized.isEmpty()) return ""
    for (separator in listOf("(", "（", "[", "<", "/", ",", "·")) {
        val index = normalized.indexOf(separator)
        if (index >= 0) normalized = normalized.substring(0, index)
    }
    normalized = normalized.replace(" ", "").trim()
    val titleSuffixes = listOf(
        "대표이사",
        "부사장", "본부장", "부회장",
        "부장", "차장", "과장", "대리", "사원", "주임", "팀장", "실장",
        "이사", "상무", "전무", "사장", "회장", "선임", "책임", "수석", "대표",
        "님", "씨", "군", "양",
    )
    while (true) {
        var stripped = false
        for (suffix in titleSuffixes) {
            if (!normalized.endsWith(suffix)) continue
            val candidate = normalized.removeSuffix(suffix)
            if (candidate.length < 2) continue
            normalized = candidate
            stripped = true
            break
        }
        if (!stripped) break
    }
    return normalized.lowercase()
}
