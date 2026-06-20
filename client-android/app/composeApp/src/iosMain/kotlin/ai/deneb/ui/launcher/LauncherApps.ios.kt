package ai.deneb.ui.launcher

// iOS is not a launcher (the OS forbids replacing SpringBoard): empty list, no-op.
actual fun createLauncherApps(): LauncherApps = object : LauncherApps {
    override fun installed(): List<LauncherAppEntry> = emptyList()
    override fun launch(packageName: String): Boolean = false
}
