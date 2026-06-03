package com.inspiredandroid.kai.deneb

// Desktop can't install an Android APK — defer to the caller's fallback (browser).
actual fun installAppUpdate(url: String, onFallback: () -> Unit) = onFallback()
