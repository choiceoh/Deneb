package ai.deneb

import android.app.Activity
import android.content.Intent
import android.net.Uri

/** Open the Play Store listing so the user can leave a review (no Play Core Review API). */
fun requestReview(activity: Activity) {
    val market = Intent(Intent.ACTION_VIEW, Uri.parse("market://details?id=${activity.packageName}"))
    market.addFlags(Intent.FLAG_ACTIVITY_NO_HISTORY or Intent.FLAG_ACTIVITY_NEW_DOCUMENT)
    try {
        activity.startActivity(market)
    } catch (_: Exception) {
        activity.startActivity(
            Intent(
                Intent.ACTION_VIEW,
                Uri.parse("https://play.google.com/store/apps/details?id=${activity.packageName}"),
            ),
        )
    }
}
