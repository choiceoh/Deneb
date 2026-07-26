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
            "clips", "column", "cta", "atEnd", "quirk", "scanCapped", "nodes", "deadControls",
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
        // nothing would read as "no gate found", the wrong conclusion. A real
        // capture hit the first cap (6,000) on a 20,127px thread and returned an
        // empty `clips`, which is precisely the misreading this flag prevents.
        assertTrue(js.contains("i < CAP"), "the element scan must be capped")
        assertTrue(js.contains("out.scanCapped"), "a capped scan must say so")
        assertTrue(js.contains("out.nodes"), "report the DOM size the cap is judged against")
    }

    @Test
    fun findsUnpressableControlsWithoutKnowingTheirNames() {
        // The keyword `cta` probe only covers buttons we thought to list, and the
        // operator's next report was a sort control it did not match. The generic
        // sweep asks the question that actually matters — which visible control
        // loses its own hit test — and names what blocked it.
        assertTrue(js.contains("out.deadControls ="), "must sweep for unpressable controls")
        assertTrue(js.contains("blockedBy:"), "must name what wins instead: $js")
    }

    @Test
    fun doesNotRestrictOverlaysToFixedPositioning() {
        // The sheet that covered the whole viewport was `position: absolute`, so
        // a fixed/sticky filter reported `overlays: []` on two captures while the
        // culprit was topmost at every probe point.
        assertTrue(js.contains("s.position !== 'static'"), "any positioned node can overlay: $js")
        assertFalse(js.contains("'sticky'"), "the fixed/sticky filter is what missed it: $js")
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
