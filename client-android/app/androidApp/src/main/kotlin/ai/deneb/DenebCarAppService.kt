package ai.deneb

import android.content.pm.ApplicationInfo
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
 * Host validation guards against another app ON THE PHONE binding to this
 * exported service while posing as a car host (work-feed cards carry mail
 * analyses — sensitive). Release builds accept only the library's built-in
 * allowlist of official Google hosts (Android Auto / AAOS), which is exactly
 * what the operator's phone projects to; debug builds allow all hosts so the
 * Desktop Head Unit keeps working without a signed host.
 */
class DenebCarAppService : CarAppService() {
    override fun createHostValidator(): HostValidator = if ((applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) != 0) {
        HostValidator.ALLOW_ALL_HOSTS_VALIDATOR
    } else {
        HostValidator.Builder(applicationContext)
            .addAllowedHosts(androidx.car.app.R.array.hosts_allowlist_sample)
            .build()
    }

    override fun onCreateSession(): Session = object : Session() {
        override fun onCreateScreen(intent: android.content.Intent): Screen = DenebCarFeedScreen(carContext)
    }
}
