package ai.deneb

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.provider.Settings
import org.koin.java.KoinJavaComponent.inject

actual fun createLauncherMode(): LauncherMode = AndroidLauncherMode()

/**
 * Flips the foss HOME activity-alias (ai.deneb.HomeAlias) on/off and deep-links the
 * system home-app chooser. Context comes from Koin, like AndroidDaemonController. On
 * the Play flavor the alias isn't declared, so getComponentEnabledSetting throws and
 * [supported] is false — the settings row hides itself there.
 */
class AndroidLauncherMode : LauncherMode {
    private val context: Context by inject(Context::class.java)
    private val alias get() = ComponentName(context, HOME_ALIAS)

    override val supported: Boolean
        get() = runCatching { context.packageManager.getComponentEnabledSetting(alias) }.isSuccess

    // DEFAULT (manifest android:enabled="false") and DISABLED both mean "not a home
    // candidate", so only an explicit ENABLED counts as on.
    override fun isEnabled(): Boolean = runCatching {
        context.packageManager.getComponentEnabledSetting(alias) ==
            PackageManager.COMPONENT_ENABLED_STATE_ENABLED
    }.getOrDefault(false)

    override fun setEnabled(enabled: Boolean) {
        val state = if (enabled) {
            PackageManager.COMPONENT_ENABLED_STATE_ENABLED
        } else {
            PackageManager.COMPONENT_ENABLED_STATE_DISABLED
        }
        runCatching {
            context.packageManager.setComponentEnabledSetting(alias, state, PackageManager.DONT_KILL_APP)
        }
    }

    override fun openHomeAppSettings() {
        runCatching {
            context.startActivity(
                Intent(Settings.ACTION_HOME_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
    }

    private companion object {
        const val HOME_ALIAS = "ai.deneb.HomeAlias"
    }
}
