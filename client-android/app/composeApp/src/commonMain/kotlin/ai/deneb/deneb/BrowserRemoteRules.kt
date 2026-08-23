package ai.deneb.deneb

import ai.deneb.deneb.generated.BrowserConfigOut

/**
 * Remotely delivered browser rules (`miniapp.browser.config.get`), ADDITIVE to
 * the compiled-in ad-block lists and site quirks: the gateway serves them from
 * `~/.deneb/browser-rules.json`, so new tracker hosts and site fixes ship
 * without an app release. An empty registry (no file, fetch failure, first
 * launch) means built-ins only — remote rules can never weaken the blocklist.
 */
internal data class BrowserRemoteRules(
    val version: Long = 0,
    val adHostSuffixes: List<String> = emptyList(),
    val adPathSegments: List<String> = emptyList(),
    val adPathTokens: List<String> = emptyList(),
    val adQueryMarkers: List<String> = emptyList(),
    val quirks: List<BrowserRemoteQuirk> = emptyList(),
)

internal data class BrowserRemoteQuirk(
    val hosts: Set<String> = emptySet(),
    val css: String = "",
)

/** Normalizes the wire payload: trims, lowercases hosts, drops empties. */
internal fun browserRemoteRulesFromWire(out: BrowserConfigOut): BrowserRemoteRules = BrowserRemoteRules(
    version = out.version.toLong(),
    adHostSuffixes = out.adHostSuffixes.mapNotNull { it.trim().lowercase().takeIf(String::isNotEmpty) }.distinct(),
    adPathSegments = out.adPathSegments.mapNotNull { it.trim().takeIf(String::isNotEmpty) }.distinct(),
    adPathTokens = out.adPathTokens.mapNotNull { it.trim().takeIf(String::isNotEmpty) }.distinct(),
    adQueryMarkers = out.adQueryMarkers.mapNotNull { it.trim().takeIf(String::isNotEmpty) }.distinct(),
    quirks = out.quirks.mapNotNull { quirk ->
        val hosts = quirk.hosts.mapNotNull { it.trim().lowercase().takeIf(String::isNotEmpty) }.toSet()
        val css = quirk.css.trim()
        if (hosts.isEmpty() || css.isEmpty()) null else BrowserRemoteQuirk(hosts, css)
    },
)

/**
 * Process-wide holder for the last installed remote rules. Readers snapshot the
 * reference on the hot `shouldInterceptRequest` path — no lock, no allocation.
 */
internal object BrowserRuleRegistry {
    @Volatile
    private var rules: BrowserRemoteRules = BrowserRemoteRules()

    fun install(rules: BrowserRemoteRules) {
        if (rules != this.rules) this.rules = rules
    }

    fun current(): BrowserRemoteRules = rules

    /** Tests only: restore the compiled-in-only baseline between cases. */
    fun reset() {
        install(BrowserRemoteRules())
    }
}
