package ai.deneb

import android.util.Log

actual object DenebLog {
    actual fun debug(tag: String, message: String) {
        Log.d(tag, message)
    }

    actual fun warn(tag: String, message: String) {
        Log.w(tag, message)
    }

    actual fun error(tag: String, message: String, throwable: Throwable?) {
        if (throwable != null) Log.e(tag, message, throwable) else Log.e(tag, message)
    }
}
