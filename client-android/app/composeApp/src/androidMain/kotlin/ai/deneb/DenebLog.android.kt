package ai.deneb

import android.util.Log

actual object DenebLog {
    // Off unless someone turns it on. Android's default level is INFO, so this is
    // false in a normal release build and true after
    // `adb shell setprop log.tag.<TAG> DEBUG` — which is exactly when developer
    // detail belongs in Logcat, and never on the user's device by default.
    actual fun debug(tag: String, message: String) {
        if (Log.isLoggable(tag, Log.DEBUG)) Log.d(tag, message)
    }

    actual fun warn(tag: String, message: String) {
        Log.w(tag, message)
    }

    actual fun error(tag: String, message: String, throwable: Throwable?) {
        if (throwable != null) Log.e(tag, message, throwable) else Log.e(tag, message)
    }
}
