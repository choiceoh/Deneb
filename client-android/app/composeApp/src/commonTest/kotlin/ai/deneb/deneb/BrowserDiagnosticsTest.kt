package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * The diagnostic runs against whatever page the operator is on — including
 * logged-in groupware and mail — so its contract is as much about what it must
 * *not* do as what it reports.
 */
class BrowserDiagnosticsTest {
    private val js = BROWSER_SCROLL_DIAGNOSTIC_JS

    @Test
    fun reportsEveryProbeTheSymptomsNeed() {
        // Each key answers one competing explanation; dropping one silently
        // makes the next capture unreadable, which is how two rounds of this
        // bug were already lost.
        for (key in listOf(
            "doc", "size", "overlays", "centerEl", "move", "listeners",
            "clips", "column", "cta", "atEnd", "quirk",
        )) {
            assertTrue(js.contains("out.$key ="), "diagnostic must report `$key`: missing")
        }
    }

    @Test
    fun treatsOurOwnQuirkAsASuspect() {
        // The scroll-unlock quirk overrides the page's overflow unconditionally.
        // A diagnostic that cannot see it would keep pointing at the site while
        // our own patch is the thing misbehaving.
        assertTrue(js.contains("__deneb-scroll-unlock"), "must report whether our quirk is applied")
        assertTrue(js.contains("bodyInline"), "must report what the page itself asked for")
    }

    @Test
    fun hitTestsTheButtonRatherThanTrustingItsStyles() {
        // "Button does not respond" is answered by who wins the hit test at the
        // button's own centre, not by pointer-events on the button itself.
        assertTrue(js.contains("elementFromPoint"), "cta probe must hit-test")
        assertTrue(js.contains("hitEl:"), "must name whatever wins the hit test")
    }

    @Test
    fun leavesThePageExactlyAsItFoundIt() {
        // The scroll probe moves the document; it must put it back, or the
        // measurement itself becomes a bug report.
        assertTrue(js.contains("target.scrollTop = before;"), "must restore scroll position")
        assertFalse(js.contains("innerHTML"), "must not rewrite page markup")
        assertFalse(js.contains(".remove()"), "must not delete nodes")
        assertFalse(js.contains(".click()"), "must not synthesize clicks")
    }

    @Test
    fun boundsItsOwnCostOnHugePages() {
        // A translated Reddit thread is tens of thousands of nodes and every
        // probe calls getComputedStyle. An unbounded scan would hang the page
        // we are trying to diagnose — and a truncated scan that reported
        // nothing would read as "no gate found", the wrong conclusion.
        assertTrue(js.contains("scanned > CAP"), "clip scan must be capped")
        assertTrue(js.contains("out.clipScanCapped"), "a capped scan must say so")
    }

    @Test
    fun neverThrowsIntoTheHostApp() {
        // evaluateJavascript delivers the string verbatim; an exception would
        // come back as `null` and lose the whole capture.
        assertTrue(js.contains("try {"), "must be wrapped")
        assertTrue(js.contains("catch (e)"), "must return an error payload instead of throwing")
        assertTrue(js.contains("""JSON.stringify({ error:"""), "the catch must still return JSON")
    }
}
