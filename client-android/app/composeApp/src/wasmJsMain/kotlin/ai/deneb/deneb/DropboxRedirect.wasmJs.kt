package ai.deneb.deneb

// Web build has no custom-scheme deep-link handling → out-of-band paste-code flow.
actual fun dropboxRedirectUri(): String? = null
