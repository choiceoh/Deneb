package ai.deneb.deneb

// iOS deep-link (URL scheme) handling is not wired yet → out-of-band paste-code
// flow. Add a CFBundleURLSchemes entry + DropboxAuthBridge hand-off to enable
// auto-capture here later.
actual fun dropboxRedirectUri(): String? = null
