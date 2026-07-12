package ai.deneb

import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.SystemClock

/**
 * Event-driven home-widget refresh, the battery-cheap counterpart to the slow
 * system cycle (deneb_widget_info.xml). The system `updatePeriodMillis` alarm
 * wakes the radio on its own, so its cadence is stretched to 2h; freshness is
 * recovered by piggybacking a refresh on moments the radio is already awake —
 * an FCM delivery or the app coming to the foreground.
 *
 * No-op (zero cost) when no widget is placed on the home screen.
 */
object WidgetRefresher {
    // Callers fire on redundant triggers (every FCM message, every app open) —
    // collapse bursts so a chatty morning doesn't turn into a widget RPC per
    // notification. Benign race on the timestamp: worst case one extra refresh.
    private const val MIN_INTERVAL_MS = 5 * 60 * 1000L

    @Volatile
    private var lastRefreshElapsed = Long.MIN_VALUE

    fun requestRefresh(context: Context) {
        val appContext = context.applicationContext
        val manager = AppWidgetManager.getInstance(appContext) ?: return
        val ids = manager.getAppWidgetIds(ComponentName(appContext, DenebWidgetProvider::class.java))
        if (ids == null || ids.isEmpty()) return
        val now = SystemClock.elapsedRealtime()
        if (now - lastRefreshElapsed < MIN_INTERVAL_MS) return
        lastRefreshElapsed = now
        val intent = Intent(appContext, DenebWidgetProvider::class.java).apply {
            action = AppWidgetManager.ACTION_APPWIDGET_UPDATE
            putExtra(AppWidgetManager.EXTRA_APPWIDGET_IDS, ids)
        }
        appContext.sendBroadcast(intent)
    }
}
