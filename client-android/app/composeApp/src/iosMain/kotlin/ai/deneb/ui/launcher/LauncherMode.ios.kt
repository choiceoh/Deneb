package ai.deneb.ui.launcher

// iOS has no third-party home-launcher capability — launcher mode is unsupported.
actual fun createLauncherMode(): LauncherMode = object : LauncherMode {
    override val supported = false
    override fun isEnabled() = false
    override fun setEnabled(enabled: Boolean) {}
    override fun openHomeAppSettings() {}
}
