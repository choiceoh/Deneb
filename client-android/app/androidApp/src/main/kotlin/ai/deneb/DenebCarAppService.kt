package ai.deneb

import androidx.car.app.CarAppService
import androidx.car.app.Screen
import androidx.car.app.Session
import androidx.car.app.validation.HostValidator

/**
 * Android Auto phase 2: a minimal template surface — a read-only browser over
 * the work feed (오늘의 리포트·질문 카드). Reply/voice interaction stays on the
 * phase-1 MessagingStyle notifications; this screen is for glancing at what
 * arrived while driving, within the host's driver-distraction rules.
 *
 * ALLOW_ALL_HOSTS: this APK is self-published (no Play review) to a single
 * operator device, and the car host is whatever head unit that phone connects
 * to — a host allowlist would only break the one supported deployment.
 */
class DenebCarAppService : CarAppService() {
    override fun createHostValidator(): HostValidator = HostValidator.ALLOW_ALL_HOSTS_VALIDATOR

    override fun onCreateSession(): Session = object : Session() {
        override fun onCreateScreen(intent: android.content.Intent): Screen = DenebCarFeedScreen(carContext)
    }
}
