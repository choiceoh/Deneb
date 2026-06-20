package ai.deneb

/**
 * Home-launcher mode for the work launcher (Phase 0) — lets Deneb act as the phone's
 * HOME app, opt-in and reversible. Android (foss) flips a dormant HOME activity-alias
 * via PackageManager so the launcher capability stays off until the user turns it on;
 * every other target reports unsupported and no-ops. The alias's component-enabled
 * state is itself the persistence — there's no separate setting to drift out of sync.
 */
interface LauncherMode {
    /** True only where Deneb can be a home app (the HOME alias exists — foss Android). */
    val supported: Boolean

    /** Whether the HOME alias is currently enabled (Deneb is a home-app candidate). */
    fun isEnabled(): Boolean

    /**
     * Enable/disable the HOME alias. Enabling makes Deneb selectable as the default
     * home in system settings; disabling removes it as a candidate (the OS falls back
     * to the previously-set launcher), so the switch is always reversible.
     */
    fun setEnabled(enabled: Boolean)

    /** Open the system home-app chooser so the user can set Deneb (or restore another
     *  launcher) as the default. */
    fun openHomeAppSettings()
}

expect fun createLauncherMode(): LauncherMode
