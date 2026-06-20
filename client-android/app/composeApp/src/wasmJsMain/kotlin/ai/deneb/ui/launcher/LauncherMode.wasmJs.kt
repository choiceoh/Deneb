package ai.deneb.ui.launcher

// The web target can't be a home launcher — launcher mode is unsupported.
actual fun createLauncherMode(): LauncherMode = object : LauncherMode {
    override val supported = false
    override fun isEnabled() = false
    override fun setEnabled(enabled: Boolean) {}
    override fun openHomeAppSettings() {}
}
