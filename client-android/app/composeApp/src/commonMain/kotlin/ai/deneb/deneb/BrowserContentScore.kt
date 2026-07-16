package ai.deneb.deneb

/**
 * Readability-style content scoring for the in-app translation browser.
 *
 * Keep the numeric formula in sync with `scoreContentCandidate` in
 * `androidMain/assets/deneb-translate.js`. Selector hits still win in the JS
 * walker; this path is the fallback when no known CMS selector matches.
 */

/** Minimum score for a scored node to become a primary content root. */
internal const val MIN_CONTENT_SCORE = 20

private val POSITIVE_CLASS_RE =
    Regex(
        "article|content|story|post|entry|body|markup|full[-_]?story|td[-_]?post|available[-_]?content|postbody|post[-_]?content",
        RegexOption.IGNORE_CASE,
    )

private val NEGATIVE_CLASS_RE =
    Regex(
        "nav|menu|sidebar|aside|footer|header|comment|related|share|social|promo|banner|ads?|subscribe|widget|cookie|consent|newsletter|recommend|popular|trending",
        RegexOption.IGNORE_CASE,
    )

/**
 * Pre-extracted DOM metrics for one candidate element. The WebView JS fills these
 * from a live node; tests build them from fixtures without a DOM.
 */
internal data class ContentScoreMetrics(
    val tag: String,
    val className: String = "",
    val id: String = "",
    val textLength: Int,
    val linkTextLength: Int = 0,
    val paragraphCount: Int = 0,
)

/**
 * One scored candidate. [containsIds] lists descendant candidate ids (for nest
 * filtering when picking roots). Empty means "unknown / treat as leaf".
 */
internal data class ContentScoreCandidate(
    val id: String,
    val metrics: ContentScoreMetrics,
    val score: Int = scoreContentCandidate(metrics),
    val containsIds: Set<String> = emptySet(),
)

/** Readability-inspired score. Higher = likelier article body. */
internal fun scoreContentCandidate(m: ContentScoreMetrics): Int {
    if (m.textLength < 80) return Int.MIN_VALUE

    var score = 0
    when (m.tag.uppercase()) {
        "ARTICLE", "MAIN" -> score += 30
        "SECTION" -> score += 15
        "DIV" -> score += 5
    }

    val classId = "${m.className} ${m.id}"
    val positiveHits = POSITIVE_CLASS_RE.findAll(classId).count().coerceAtMost(3)
    score += positiveHits * 25
    val negativeHits = NEGATIVE_CLASS_RE.findAll(classId).count().coerceAtMost(4)
    score -= negativeHits * 40

    score += (m.paragraphCount * 3).coerceAtMost(50)
    score += (m.textLength / 100).coerceAtMost(40)

    val denom = m.textLength.coerceAtLeast(1).toDouble()
    val linkDensity = m.linkTextLength.toDouble() / denom
    when {
        linkDensity > 0.35 -> score -= 50
        linkDensity > 0.2 -> score -= 20
    }

    return score
}

/**
 * Pick non-nested primary roots from scored candidates (highest score first).
 * Mirrors the nest filter in `contentRoots` after selector / score sort.
 */
internal fun pickScoredContentRoots(
    candidates: List<ContentScoreCandidate>,
    minScore: Int = MIN_CONTENT_SCORE,
    limit: Int = 12,
): List<ContentScoreCandidate> {
    val ranked = candidates
        .filter { it.score >= minScore && it.metrics.textLength >= 80 }
        .sortedWith(compareByDescending<ContentScoreCandidate> { it.score }.thenByDescending { it.metrics.textLength })
    val roots = ArrayList<ContentScoreCandidate>(limit.coerceAtMost(ranked.size))
    for (c in ranked) {
        if (roots.size >= limit) break
        val nested = roots.any { root ->
            root.id == c.id ||
                root.containsIds.contains(c.id) ||
                c.containsIds.contains(root.id)
        }
        if (!nested) roots.add(c)
    }
    return roots
}
