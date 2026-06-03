package com.inspiredandroid.kai.deneb

// Web build can't install an APK — defer to the caller's fallback (open the URL).
actual fun installAppUpdate(url: String, onFallback: () -> Unit) = onFallback()
