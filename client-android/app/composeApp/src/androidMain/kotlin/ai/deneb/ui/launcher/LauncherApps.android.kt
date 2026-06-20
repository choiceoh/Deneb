package ai.deneb.ui.launcher

import android.content.Context
import android.content.Intent
import androidx.compose.ui.graphics.asImageBitmap
import androidx.core.graphics.drawable.toBitmap
import org.koin.java.KoinJavaComponent.inject

actual fun createLauncherApps(): LauncherApps = AndroidLauncherApps()

/**
 * PackageManager-backed app source. Context comes from Koin (registered via
 * androidContext() in DenebApplication), mirroring AndroidDaemonController. Package
 * visibility for the MAIN/LAUNCHER query is granted by the <queries> block in the
 * foss manifest (and automatically once Deneb is the default home).
 */
class AndroidLauncherApps : LauncherApps {

    private val context: Context by inject(Context::class.java)

    override fun installed(): List<LauncherAppEntry> {
        val pm = context.packageManager
        val query = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        return pm.queryIntentActivities(query, 0)
            .asSequence()
            .mapNotNull { ri ->
                val pkg = ri.activityInfo?.packageName ?: return@mapNotNull null
                if (pkg == context.packageName) return@mapNotNull null // hide self
                val label = runCatching { ri.loadLabel(pm).toString() }.getOrNull()?.trim().orEmpty()
                if (label.isEmpty()) return@mapNotNull null
                // App icons can be large adaptive drawables; cap the rasterized size so the
                // grid stays light. Failure (odd drawable) falls back to the lettered disc.
                val icon = runCatching { ri.loadIcon(pm).toBitmap(width = 144, height = 144).asImageBitmap() }.getOrNull()
                LauncherAppEntry(label = label, packageName = pkg, icon = icon)
            }
            .distinctBy { it.packageName }
            .sortedBy { it.label.lowercase() }
            .toList()
    }

    override fun launch(packageName: String): Boolean {
        val launch = context.packageManager.getLaunchIntentForPackage(packageName) ?: return false
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        return runCatching { context.startActivity(launch) }.isSuccess
    }
}
