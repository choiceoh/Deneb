package ai.deneb.deneb

/**
 * Trigger an OTA install of a published APK.
 *
 * On Android this downloads the APK via DownloadManager and launches the system
 * package installer — the final "Install" tap is still the user's, since Android
 * requires confirmation for sideloaded APKs. Every other platform falls back via
 * [onFallback] (e.g. opening the URL in a browser), because only Android can
 * install an APK this way.
 */
expect fun installAppUpdate(url: String, onFallback: () -> Unit)
