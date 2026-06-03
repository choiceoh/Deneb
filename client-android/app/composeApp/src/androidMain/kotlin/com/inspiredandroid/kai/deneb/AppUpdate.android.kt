package com.inspiredandroid.kai.deneb

import android.app.DownloadManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.Uri
import android.os.Build
import android.os.Environment
import org.koin.core.context.GlobalContext

// Downloads the APK with DownloadManager, then launches the system package
// installer when it finishes. The final "Install" tap stays the user's — Android
// requires it for sideloaded APKs. If install can't proceed (no Context, no
// "install unknown apps" permission, or a download/launch failure) we fall back
// to [onFallback] (the caller opens the URL in a browser), so the user is never
// left with a dead button.
actual fun installAppUpdate(url: String, onFallback: () -> Unit) {
    val context = runCatching { GlobalContext.get().get<Context>() }.getOrNull() ?: return onFallback()

    // Without the "install unknown apps" permission the installer silently no-ops,
    // so route to the browser instead (the user can grant it there + install).
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O &&
        !context.packageManager.canRequestPackageInstalls()
    ) {
        onFallback()
        return
    }

    val dm = context.getSystemService(Context.DOWNLOAD_SERVICE) as? DownloadManager
        ?: return onFallback()

    val request = DownloadManager.Request(Uri.parse(url))
        .setTitle("Deneb 업데이트")
        .setMimeType("application/vnd.android.package-archive")
        .setDestinationInExternalFilesDir(context, Environment.DIRECTORY_DOWNLOADS, "deneb-update.apk")
        .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
    val downloadId = runCatching { dm.enqueue(request) }.getOrElse {
        onFallback()
        return
    }

    val receiver = object : BroadcastReceiver() {
        override fun onReceive(ctx: Context, intent: Intent) {
            if (intent.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID, -1L) != downloadId) return
            runCatching { ctx.unregisterReceiver(this) }
            val apkUri = dm.getUriForDownloadedFile(downloadId)
            if (apkUri == null) {
                onFallback()
                return
            }
            val install = Intent(Intent.ACTION_VIEW).apply {
                setDataAndType(apkUri, "application/vnd.android.package-archive")
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_GRANT_READ_URI_PERMISSION)
            }
            runCatching { ctx.startActivity(install) }.onFailure { onFallback() }
        }
    }
    val filter = IntentFilter(DownloadManager.ACTION_DOWNLOAD_COMPLETE)
    // RECEIVER_EXPORTED is required on API 33+; DownloadManager's completion
    // broadcast comes from the system, so the receiver must accept it.
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        context.registerReceiver(receiver, filter, Context.RECEIVER_EXPORTED)
    } else {
        @Suppress("UnspecifiedRegisterReceiverFlag")
        context.registerReceiver(receiver, filter)
    }
}
