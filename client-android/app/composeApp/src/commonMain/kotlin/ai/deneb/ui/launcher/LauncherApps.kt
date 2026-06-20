package ai.deneb.ui.launcher

/**
 * Platform source of installed, launchable apps for the work-launcher drawer
 * (Phase 0). Android resolves MAIN/LAUNCHER activities via PackageManager and
 * launches them; non-launcher targets (desktop/iOS/wasm) return an empty list and
 * a no-op launch. Pure local — never touches the gateway, so the drawer (the
 * offline-first shell) always works even with the server down.
 */
interface LauncherApps {
    /** Installed launchable apps, label-sorted, excluding Deneb itself. */
    fun installed(): List<LauncherAppEntry>

    /** Launches the app by package name; returns false if it can't be launched. */
    fun launch(packageName: String): Boolean
}

expect fun createLauncherApps(): LauncherApps
