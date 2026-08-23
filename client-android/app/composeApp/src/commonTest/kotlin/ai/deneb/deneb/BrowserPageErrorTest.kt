package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserPageErrorTest {
    @Test
    fun mapsCommonFailuresToKorean() {
        assertTrue(browserPageErrorMessage(BrowserErrorCode.HOST_LOOKUP, "net::ERR_NAME").contains("주소를 찾을 수 없"))
        assertTrue(browserPageErrorMessage(BrowserErrorCode.TIMEOUT, null).contains("시간이 초과"))
        assertTrue(browserPageErrorMessage(BrowserErrorCode.FAILED_SSL_HANDSHAKE, null).contains("보안 연결"))
    }

    @Test
    fun unmappedCodeFallsBackToTheDescriptionNotABareNumber() {
        assertEquals("net::ERR_WEIRD", browserPageErrorMessage(-999, "net::ERR_WEIRD"))
        // …and to a sentence when even that is missing, never an empty banner.
        assertEquals("페이지를 열지 못했습니다", browserPageErrorMessage(-999, null))
        assertEquals("페이지를 열지 못했습니다", browserPageErrorMessage(-999, "   "))
    }

    @Test
    fun sslCodesAreDistinguished() {
        assertTrue(browserSslErrorMessage(BrowserSslCode.EXPIRED).contains("만료"))
        assertTrue(browserSslErrorMessage(BrowserSslCode.ID_MISMATCH).contains("도메인"))
        assertTrue(browserSslErrorMessage(BrowserSslCode.UNTRUSTED).contains("신뢰할 수 없"))
        assertTrue(browserSslErrorMessage(99).contains("보안 인증서"))
    }

    @Test
    fun rendererExitsDistinguishCrashesFromMemoryReclamation() {
        assertTrue(browserRendererGoneMessage(crashed = true).contains("비정상 종료"))
        assertTrue(browserRendererGoneMessage(crashed = false).contains("메모리"))
    }

    @Test
    fun httpGatewayStatusesOverlayAndSiteErrorsDoNot() {
        assertTrue(browserHttpErrorMessage(502).orEmpty().contains("연결하지 못했"))
        assertTrue(browserHttpErrorMessage(503).orEmpty().contains("연결하지 못했"))
        assertTrue(browserHttpErrorMessage(504).orEmpty().contains("연결하지 못했"))
        assertTrue(browserHttpErrorMessage(408).orEmpty().contains("시간이 초과"))
        assertTrue(browserHttpErrorMessage(429).orEmpty().contains("너무 많"))
        assertEquals(null, browserHttpErrorMessage(404))
        assertEquals(null, browserHttpErrorMessage(401))
        assertEquals(null, browserHttpErrorMessage(500))
        assertEquals(null, browserHttpErrorMessage(200))
    }

    @Test
    fun sslFailuresMatchTheAskedPageNotThirdPartyFrames() {
        assertTrue(browserSslErrorAffectsPage("https://pay.example/auth", "https://pay.example/auth"))
        assertTrue(browserSslErrorAffectsPage("https://pay.example/auth#x", "https://pay.example/auth"))
        assertTrue(browserSslErrorAffectsPage("https://pay.example/auth/", "https://pay.example/auth"))
        assertTrue(browserSslErrorAffectsPage("", "https://pay.example/auth"))
        assertFalse(browserSslErrorAffectsPage("https://shop.example/", "https://tracker.example/pixel"))
        assertFalse(browserSslErrorAffectsPage("https://shop.example/", ""))
    }
}

class BrowserJsDialogTest {
    @Test
    fun answersThePageExactlyOnce() {
        // The WebView blocks the JS thread on the result and throws if it is
        // delivered twice, so a double-tap must not reach the callback twice.
        var calls = 0
        var lastConfirmed: Boolean? = null
        val d = BrowserJsDialog(BrowserJsDialog.Kind.CONFIRM, "지울까요?") { confirmed, _ ->
            calls++
            lastConfirmed = confirmed
        }
        d.answer(true)
        d.answer(true)
        d.cancel()
        assertEquals(1, calls)
        assertEquals(true, lastConfirmed)
    }

    @Test
    fun dismissCancels() {
        var confirmed: Boolean? = null
        val d = BrowserJsDialog(BrowserJsDialog.Kind.CONFIRM, "지울까요?") { c, _ -> confirmed = c }
        d.cancel()
        assertFalse(confirmed!!)
    }

    @Test
    fun promptCarriesTheTypedValue() {
        var value: String? = null
        val d = BrowserJsDialog(BrowserJsDialog.Kind.PROMPT, "이름?", defaultValue = "기본") { _, v -> value = v }
        assertEquals("기본", d.defaultValue)
        d.answer(true, "선택")
        assertEquals("선택", value)
    }
}
