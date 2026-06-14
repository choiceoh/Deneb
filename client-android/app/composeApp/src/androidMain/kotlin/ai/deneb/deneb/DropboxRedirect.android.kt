package ai.deneb.deneb

// Android registers an intent-filter for this scheme (androidApp manifest), so
// Dropbox can redirect back to the app after consent; MainActivity routes the
// ?code into DropboxAuthBridge for auto-capture (no manual paste).
actual fun dropboxRedirectUri(): String? = "deneb://dropbox-auth"
