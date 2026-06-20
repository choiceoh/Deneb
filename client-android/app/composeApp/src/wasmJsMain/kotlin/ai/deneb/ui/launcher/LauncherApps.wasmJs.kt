package ai.deneb.ui.launcher

// Web is not a launcher: empty list, no-op launch.
actual fun createLauncherApps(): LauncherApps = object : LauncherApps {
    override fun installed(): List<LauncherAppEntry> = emptyList()
    override fun launch(packageName: String): Boolean = false
}
