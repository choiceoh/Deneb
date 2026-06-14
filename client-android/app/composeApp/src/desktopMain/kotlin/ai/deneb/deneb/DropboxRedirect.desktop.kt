package ai.deneb.deneb

// Desktop has no custom-scheme deep-link handling → use the out-of-band
// paste-code flow (Dropbox shows the code; the user pastes it in the wizard).
actual fun dropboxRedirectUri(): String? = null
