package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.WidgetSummary
import ai.deneb.deneb.widgetSummary
import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.core.component.KoinComponent
import org.koin.core.component.inject

// Broadcast receivers get roughly 10s before the system treats them as hung; stay
// well inside it so a stalled gateway shows "새로고침 실패" instead of an ANR.
private const val WIDGET_FETCH_TIMEOUT_MS = 8_000L

// Home-screen widget: the next meeting and unread-mail count at a glance, with a
// tap that opens the Deneb chat. Refreshes on a slow 2h system cycle
// (deneb_widget_info.xml) plus event-driven updates via [WidgetRefresher]
// (FCM delivery, app foreground) — the alarm cadence is kept long for battery
// and freshness rides moments the radio is already awake. A native-only
// surface — something the retired Telegram bot never could put on the home
// screen.
class DenebWidgetProvider :
    AppWidgetProvider(),
    KoinComponent {

    private val repo: DataRepository by inject()

    override fun onUpdate(context: Context, mgr: AppWidgetManager, ids: IntArray) {
        // Paint a loading state immediately, then fetch off the main thread.
        for (id in ids) render(context, mgr, id, WidgetSummary(meeting = "…", unread = -1))
        val pending = goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            // goAsync() buys a broadcast a short grace period, not the client's
            // 180s request budget — overrunning it is an ANR kill, not a slow
            // widget. Give up well inside the window and paint the failure.
            val summary = withTimeoutOrNull(WIDGET_FETCH_TIMEOUT_MS) {
                (repo as? DenebGatewayClient)?.widgetSummary() ?: WidgetSummary(configured = false)
            } ?: WidgetSummary(ok = false)
            try {
                for (id in ids) render(context, mgr, id, summary)
            } finally {
                pending.finish()
            }
        }
    }

    private fun render(context: Context, mgr: AppWidgetManager, id: Int, s: WidgetSummary) {
        val views = RemoteViews(context.packageName, R.layout.deneb_widget)
        val meeting = when {
            !s.configured -> "Deneb 설정 필요"
            !s.ok -> "새로고침 실패"
            s.unread < 0 -> "불러오는 중…"
            s.meeting.isNotBlank() -> s.meeting
            else -> "예정된 일정 없음"
        }
        views.setTextViewText(R.id.widget_meeting, meeting)
        // Recent-mail glance: "sender · subject" with the unread count as a sub-line.
        // Hide the whole mail row while loading/unconfigured or when there is no mail,
        // so the widget never shows a lone envelope with nothing beside it.
        val latest = if (s.configured && s.ok) s.latestMail else ""
        views.setTextViewText(R.id.widget_mail_latest, latest)
        views.setViewVisibility(
            R.id.widget_mail_row,
            if (latest.isEmpty()) android.view.View.GONE else android.view.View.VISIBLE,
        )
        val unreadText = if (s.unread > 0) "미읽음 ${s.unread}" else ""
        views.setTextViewText(R.id.widget_mail_unread, unreadText)
        views.setViewVisibility(
            R.id.widget_mail_unread,
            if (unreadText.isEmpty()) android.view.View.GONE else android.view.View.VISIBLE,
        )
        val tap = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        views.setOnClickPendingIntent(R.id.widget_root, tap)
        mgr.updateAppWidget(id, views)
    }
}
