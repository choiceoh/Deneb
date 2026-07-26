package ai.deneb.sensing

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.provider.CallLog
import org.koin.java.KoinJavaComponent
import java.util.Calendar
import java.util.concurrent.TimeUnit

private const val WINDOW_HOURS = 24L
private const val MAX_CALLS = 12

// Under this, a call is a misdial or a rejected ring, not a conversation worth
// remembering. Kept low because a 30-second "가고 있습니다" still carries meaning.
private const val MIN_ANSWERED_SECONDS = 15

/**
 * CallLog-backed digest. Context comes from Koin (registered via androidContext()
 * in DenebApplication), mirroring [readWorkUsageDigest]. Returns null unless the
 * user granted READ_CALL_LOG AND there is a call inside the window.
 *
 * The window is a full day, longer than the usage digest's six hours: app usage
 * decays into noise, but "어제 저녁에 통화했다" stays true and stays relevant to a
 * deal.
 */
actual fun readRecentCallDigest(): String? {
    val context = runCatching { KoinJavaComponent.get<Context>(Context::class.java) }.getOrNull() ?: return null
    if (context.checkSelfPermission(Manifest.permission.READ_CALL_LOG) != PackageManager.PERMISSION_GRANTED) {
        return null
    }
    val since = System.currentTimeMillis() - TimeUnit.HOURS.toMillis(WINDOW_HOURS)
    val rows = runCatching {
        context.contentResolver.query(
            CallLog.Calls.CONTENT_URI,
            arrayOf(CallLog.Calls.NUMBER, CallLog.Calls.CACHED_NAME, CallLog.Calls.TYPE, CallLog.Calls.DURATION, CallLog.Calls.DATE),
            "${CallLog.Calls.DATE} >= ?",
            arrayOf(since.toString()),
            "${CallLog.Calls.DATE} DESC",
        )?.use { c ->
            buildList {
                while (c.moveToNext() && size < MAX_CALLS) {
                    val type = c.getInt(2)
                    val seconds = c.getLong(3)
                    // A short *answered* call is noise; a missed/rejected one is a
                    // fact worth reporting precisely because it has no duration.
                    val answered = type == CallLog.Calls.INCOMING_TYPE || type == CallLog.Calls.OUTGOING_TYPE
                    if (answered && seconds < MIN_ANSWERED_SECONDS) continue
                    add(
                        CallRow(
                            number = c.getString(0).orEmpty(),
                            name = c.getString(1).orEmpty(),
                            type = type,
                            seconds = seconds,
                            at = c.getLong(4),
                        ),
                    )
                }
            }
        }
    }.getOrNull().orEmpty()

    if (rows.isEmpty()) return null
    val body = rows.joinToString("\n") { "- " + it.format() }
    return "지난 ${WINDOW_HOURS}시간 통화 ${rows.size}건:\n$body"
}

private class CallRow(
    val number: String,
    val name: String,
    val type: Int,
    val seconds: Long,
    val at: Long,
) {
    fun format(): String {
        // Both name and number: the name is what the operator recognizes, the
        // number is what the gateway's synced address book joins on.
        val who = when {
            name.isNotBlank() && number.isNotBlank() -> "$name($number)"
            name.isNotBlank() -> name
            number.isNotBlank() -> number
            else -> "번호 미상"
        }
        return "$who ${directionLabel(type)}${durationLabel()} · ${clock(at)}"
    }

    private fun durationLabel(): String = when {
        type == CallLog.Calls.MISSED_TYPE || type == CallLog.Calls.REJECTED_TYPE -> ""
        seconds >= 60 -> " ${seconds / 60}분"
        else -> " ${seconds}초"
    }
}

private fun directionLabel(type: Int): String = when (type) {
    CallLog.Calls.INCOMING_TYPE -> "수신"
    CallLog.Calls.OUTGOING_TYPE -> "발신"
    CallLog.Calls.MISSED_TYPE -> "부재중"
    CallLog.Calls.REJECTED_TYPE -> "거절"
    CallLog.Calls.BLOCKED_TYPE -> "차단"
    CallLog.Calls.VOICEMAIL_TYPE -> "음성사서함"
    else -> "통화"
}

private fun clock(epochMillis: Long): String {
    val c = Calendar.getInstance().apply { timeInMillis = epochMillis }
    return "%02d:%02d".format(c.get(Calendar.HOUR_OF_DAY), c.get(Calendar.MINUTE))
}
