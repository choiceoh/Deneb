package ai.deneb.sensing

import android.app.AppOpsManager
import android.app.usage.UsageStatsManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Process
import org.koin.java.KoinJavaComponent
import java.util.concurrent.TimeUnit

private const val WINDOW_HOURS = 6L
private const val MIN_APP_MS = 3L * 60 * 1000 // ignore apps under 3 min of foreground time
private const val TOP_K = 5

/**
 * UsageStatsManager-backed digest. Context comes from Koin (registered via
 * androidContext() in DenebApplication), mirroring AndroidLauncherApps. Returns
 * null unless the user granted Usage access (Settings > Special app access > Usage
 * access > Deneb) AND there's meaningful foreground time to report.
 */
actual fun readWorkUsageDigest(): String? {
    val context = runCatching { KoinJavaComponent.get<Context>(Context::class.java) }.getOrNull() ?: return null
    if (!hasUsageAccess(context)) return null
    val usm = context.getSystemService(Context.USAGE_STATS_SERVICE) as? UsageStatsManager ?: return null

    val now = System.currentTimeMillis()
    val begin = now - TimeUnit.HOURS.toMillis(WINDOW_HOURS)
    val stats = usm.queryUsageStats(UsageStatsManager.INTERVAL_BEST, begin, now) ?: return null

    // queryUsageStats can return several buckets per package — sum foreground time.
    val byPkg = HashMap<String, Long>()
    for (s in stats) {
        val ms = s.totalTimeInForeground
        if (ms > 0) byPkg[s.packageName] = (byPkg[s.packageName] ?: 0L) + ms
    }

    val self = context.packageName
    val pm = context.packageManager
    val ranked = byPkg.entries
        .asSequence()
        .filter { it.key != self && it.value >= MIN_APP_MS }
        .filter { it.key !in EXCLUDED_PACKAGES }
        // Launchable apps only: drops system services / providers that rack up
        // background "foreground" time but aren't apps the user meaningfully used.
        .filter { pm.getLaunchIntentForPackage(it.key) != null }
        .sortedByDescending { it.value }
        .take(TOP_K)
        .map { appLabel(pm, it.key) to (it.value / 60_000L) } // ms → whole minutes
        .filter { it.second >= 1 }
        .toList()

    if (ranked.isEmpty()) return null
    val body = ranked.joinToString(" · ") { (label, mins) -> "$label ${mins}분" }
    return "지난 ${WINDOW_HOURS}시간 앱 사용: $body"
}

private fun hasUsageAccess(context: Context): Boolean {
    val appOps = context.getSystemService(Context.APP_OPS_SERVICE) as? AppOpsManager ?: return false
    val mode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        appOps.unsafeCheckOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), context.packageName)
    } else {
        @Suppress("DEPRECATION")
        appOps.checkOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), context.packageName)
    }
    return mode == AppOpsManager.MODE_ALLOWED
}

private fun appLabel(pm: PackageManager, pkg: String): String = runCatching { pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0)).toString() }
    .getOrNull()?.trim()?.takeIf { it.isNotEmpty() } ?: pkg

// Hygiene: keep authenticators / password managers out of the usage digest, mirroring
// the notification listener's sensitive-package floor.
private val EXCLUDED_PACKAGES = setOf(
    "com.google.android.apps.authenticator2",
    "com.azure.authenticator",
    "com.authy.authy",
    "com.lastpass.lpandroid",
    "com.agilebits.onepassword",
    "com.bitwarden.authenticator",
    "com.x8bit.bitwarden",
)
