package ai.deneb.deneb

/** Legacy history has request counts only; absent token measurements are not zero hits. */
internal fun engineTokenCacheText(measured: Boolean, ratio: Double, reused: Long, queried: Long): String = if (measured && queried > 0) {
    "${formatPercent(ratio * 100)} · ${formatTokenCount(reused)} / ${formatTokenCount(queried)} 토큰"
} else {
    "미측정"
}

internal fun engineRequestCacheText(ratio: Double, hits: Long, queries: Long): String = "${formatPercent(ratio * 100)} · $hits / $queries 건"
