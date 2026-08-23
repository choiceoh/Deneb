package ai.deneb.data

import com.russhwolf.settings.Settings

/**
 * A [Settings] store that lives only for this process — the last-resort fallback
 * when the platform's real store cannot be opened at all.
 *
 * Why this exists: [ai.deneb.createSecureSettings] is resolved from a Koin
 * `single` during `Application.onCreate`. If it throws, the app cannot start —
 * not once, but on every launch, with no in-app way out (the user has to clear
 * app data). Degrading to a session-only store keeps the app bootable and lets
 * the durable url+token mirror re-pair it, at the cost of settings written this
 * session not persisting. That trade is only ever taken after a real open has
 * failed twice; it is never a normal code path.
 *
 * In-memory is deliberate over "just use a plaintext file": this store holds API
 * keys and conversation encryption keys, so a fallback that persisted them
 * unencrypted would trade a boot failure for a secret leak.
 *
 * Not thread-safe beyond what a plain map gives, matching the platform Settings
 * implementations (SharedPreferences is internally synchronized; this fallback
 * is a degraded mode, not a durability guarantee).
 */
class InMemorySettings : Settings {
    private val values = mutableMapOf<String, Any>()

    override val keys: Set<String> get() = values.keys.toSet()
    override val size: Int get() = values.size

    override fun clear() = values.clear()
    override fun remove(key: String) {
        values.remove(key)
    }

    override fun hasKey(key: String): Boolean = values.containsKey(key)

    override fun putInt(key: String, value: Int) {
        values[key] = value
    }

    override fun getInt(key: String, defaultValue: Int): Int = getIntOrNull(key) ?: defaultValue
    override fun getIntOrNull(key: String): Int? = values[key] as? Int

    override fun putLong(key: String, value: Long) {
        values[key] = value
    }

    override fun getLong(key: String, defaultValue: Long): Long = getLongOrNull(key) ?: defaultValue
    override fun getLongOrNull(key: String): Long? = values[key] as? Long

    override fun putString(key: String, value: String) {
        values[key] = value
    }

    override fun getString(key: String, defaultValue: String): String = getStringOrNull(key) ?: defaultValue
    override fun getStringOrNull(key: String): String? = values[key] as? String

    override fun putFloat(key: String, value: Float) {
        values[key] = value
    }

    override fun getFloat(key: String, defaultValue: Float): Float = getFloatOrNull(key) ?: defaultValue
    override fun getFloatOrNull(key: String): Float? = values[key] as? Float

    override fun putDouble(key: String, value: Double) {
        values[key] = value
    }

    override fun getDouble(key: String, defaultValue: Double): Double = getDoubleOrNull(key) ?: defaultValue
    override fun getDoubleOrNull(key: String): Double? = values[key] as? Double

    override fun putBoolean(key: String, value: Boolean) {
        values[key] = value
    }

    override fun getBoolean(key: String, defaultValue: Boolean): Boolean = getBooleanOrNull(key) ?: defaultValue
    override fun getBooleanOrNull(key: String): Boolean? = values[key] as? Boolean
}
