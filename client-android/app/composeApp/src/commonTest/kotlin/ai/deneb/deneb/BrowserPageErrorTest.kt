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
    fun popupHitchAcceptsRealTargetsAndSkipsBlankAboutAndJavascript() {
        assertTrue(browserAdoptPopupUrl("https://pay.example/auth"))
        assertTrue(browserAdoptPopupUrl("http://login.example"))
        assertTrue(browserAdoptPopupUrl("intent://scan/#Intent;end"))
        assertFalse(browserAdoptPopupUrl(""))
        assertFalse(browserAdoptPopupUrl("about:blank"))
        assertFalse(browserAdoptPopupUrl("  about:blank  "))
        assertFalse(browserAdoptPopupUrl("javascript:void(0)"))
    }

    @Test
    fun rendererExitsDistinguishCrashesFromMemoryReclamation() {
        assertTrue(browserRendererGoneMessage(crashed = true).contains("비정상 종료"))
        assertTrue(browserRendererGoneMessage(crashed = false).contains("메모리"))
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
