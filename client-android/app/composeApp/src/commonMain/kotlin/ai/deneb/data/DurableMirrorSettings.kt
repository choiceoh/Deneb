package ai.deneb.data

import com.russhwolf.settings.Settings

/**
 * A [Settings] decorator that keeps a small whitelist of must-survive keys — the
 * gateway URL and client token — mirrored into a second, *unencrypted* store.
 *
 * Why this exists: on Android the encrypted settings file (`kai_secure_prefs`) is
 * deleted and recreated **empty** whenever it can't be decrypted, which happens
 * after an app update / Auto Backup restore because the hardware Keystore key
 * doesn't transfer (see `createSecureSettings` in `Platform.android.kt`). That
 * silently wiped the gateway token + URL on every update, so the app logged the
 * user out ("게이트웨이 상태가 확인되지 않았다") until they re-entered the token by hand.
 *
 * The [mirror] is a separate plain prefs file that the wipe does NOT touch, so
 * url+token survive it. Only these two keys are mirrored — API keys, email
 * passwords, and conversation encryption keys stay in the encrypted store only.
 *
 * Reads sync the two stores **both ways** for the mirrored keys so either can
 * reconstruct the other:
 *   - delegate missing, mirror present → heal the (wiped) encrypted store from the mirror
 *   - delegate present, mirror missing → backfill the mirror, so an existing user
 *     is protected on first read after this update even before they re-save
 * Writes go to both. Removes/clear clear both. Non-mirrored keys pass straight
 * through to [delegate].
 *
 * All other [Settings] members are delegated unchanged via `by delegate`.
 */
class DurableMirrorSettings(
    private val delegate: Settings,
    private val mirror: Settings,
    private val mirroredKeys: Set<String>,
) : Settings by delegate {

    override fun putString(key: String, value: String) {
        delegate.putString(key, value)
        if (key in mirroredKeys) mirror.putString(key, value)
    }

    override fun getString(key: String, defaultValue: String): String {
        if (key !in mirroredKeys) return delegate.getString(key, defaultValue)
        return syncedString(key) ?: defaultValue
    }

    override fun getStringOrNull(key: String): String? {
        if (key !in mirroredKeys) return delegate.getStringOrNull(key)
        return syncedString(key)
    }

    override fun remove(key: String) {
        delegate.remove(key)
        if (key in mirroredKeys) mirror.remove(key)
    }

    override fun clear() {
        delegate.clear()
        // The mirror holds only the whitelisted keys; drop them explicitly rather
        // than clear() in case the backing prefs file is ever shared.
        mirroredKeys.forEach { mirror.remove(it) }
    }

    /**
     * Returns the authoritative value for a mirrored key, healing or backfilling
     * the stores so a future wipe of either side is recoverable. The encrypted
     * [delegate] is authoritative when both hold the key. Returns null when
     * neither store has it (the caller then applies its own default).
     */
    private fun syncedString(key: String): String? {
        val inDelegate = delegate.hasKey(key)
        val inMirror = mirror.hasKey(key)
        return when {
            inDelegate -> {
                val value = delegate.getString(key, "")
                if (!inMirror) mirror.putString(key, value) // backfill: cover a future wipe
                value
            }

            inMirror -> {
                val value = mirror.getString(key, "")
                delegate.putString(key, value) // heal the wiped encrypted store
                value
            }

            else -> null
        }
    }

    companion object {
        // Must match DenebGatewayClient.KEY_URL / KEY_TOKEN. These are pinned
        // identity keys (like the "kai_secure_prefs" file name) — do not rename.
        val GATEWAY_KEYS: Set<String> = setOf("deneb.gatewayUrl", "deneb.clientToken")
    }
}
