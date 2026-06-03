package com.inspiredandroid.kai.deneb

// iOS can't sideload an APK — defer to the caller's fallback (browser / App Store).
actual fun installAppUpdate(url: String, onFallback: () -> Unit) = onFallback()
