package ai.deneb.data

import ai.deneb.data.AppSettings.Companion.KEY_APP_OPENS
import ai.deneb.data.AppSettings.Companion.KEY_MIGRATION_COMPLETE
import com.russhwolf.settings.Settings

/**
 * One-time port of the legacy app-open counter from the previous on-device
 * preference store. On Android/iOS [createLegacySettings] returns that store; on
 * desktop/wasm it returns null and this is a no-op. Guarded by
 * [KEY_MIGRATION_COMPLETE] so it runs at most once per install.
 *
 * The former on-device provider/credential migrations (configured services,
 * per-instance API keys/models, OpenAI-compatible base-URL `/v1` normalization)
 * were removed together with the `Service` provider model. See PR #2387 and its
 * follow-up; the gateway now owns model selection.
 */
fun AppSettings.runMigrations(legacySettings: Settings?) {
    if (legacySettings == null) return
    if (settings.getBoolean(KEY_MIGRATION_COMPLETE, false)) return

    if (legacySettings.hasKey(KEY_APP_OPENS) && !settings.hasKey(KEY_APP_OPENS)) {
        settings.putInt(KEY_APP_OPENS, legacySettings.getInt(KEY_APP_OPENS, 0))
    }

    settings.putBoolean(KEY_MIGRATION_COMPLETE, true)
}
