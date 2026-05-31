package com.inspiredandroid.kai

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import com.inspiredandroid.kai.data.DataRepository
import com.inspiredandroid.kai.deneb.DenebGatewayClient
import com.inspiredandroid.kai.deneb.WidgetSummary
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import org.koin.core.component.KoinComponent
import org.koin.core.component.inject

// Home-screen widget: the next meeting and unread-mail count at a glance, with a
// tap that opens the Deneb chat. Refreshes on the system's 30-min cycle
// (deneb_widget_info.xml) and whenever a widget is added or resized. A
// native-only surface — the Telegram bot can't put a glanceable card on the home
// screen.
class DenebWidgetProvider : AppWidgetProvider(), KoinComponent {

    private val repo: DataRepository by inject()

    override fun onUpdate(context: Context, mgr: AppWidgetManager, ids: IntArray) {
        // Paint a loading state immediately, then fetch off the main thread.
        for (id in ids) render(context, mgr, id, WidgetSummary(meeting = "…", unread = -1))
        val pending = goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            val summary = (repo as? DenebGatewayClient)?.widgetSummary()
                ?: WidgetSummary(configured = false)
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
        views.setTextViewText(
            R.id.widget_unread,
            if (s.configured && s.ok && s.unread >= 0) "받은편지함 미읽음 ${s.unread}" else "",
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
