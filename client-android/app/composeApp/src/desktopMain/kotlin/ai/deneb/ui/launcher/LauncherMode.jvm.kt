package ai.deneb.ui.launcher

// Desktop can't be an Android home app — launcher mode is unsupported here.
actual fun createLauncherMode(): LauncherMode = object : LauncherMode {
    override val supported = false
    override fun isEnabled() = false
    override fun setEnabled(enabled: Boolean) {}
    override fun openHomeAppSettings() {}
}
