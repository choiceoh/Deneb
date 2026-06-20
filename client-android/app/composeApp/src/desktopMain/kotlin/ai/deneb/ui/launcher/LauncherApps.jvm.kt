package ai.deneb.ui.launcher

// Desktop is not a launcher: no installed-app list, no-op launch. The drawer shows
// its empty state here (the harness RenderPreview feeds mock apps directly).
actual fun createLauncherApps(): LauncherApps = object : LauncherApps {
    override fun installed(): List<LauncherAppEntry> = emptyList()
    override fun launch(packageName: String): Boolean = false
}
