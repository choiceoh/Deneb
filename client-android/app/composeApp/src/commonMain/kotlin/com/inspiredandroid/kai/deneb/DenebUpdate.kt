package com.inspiredandroid.kai.deneb

import com.inspiredandroid.kai.Version
import kotlinx.serialization.Serializable

// Self-served in-app update. The gateway serves the APK + manifest on its own
// port (the same base URL the client already uses for chat), so the update
// check works over the cloudflare tunnel — unlike the old :19010 side-server
// the tunnel never routed. The client fetches the manifest, compares it to the
// compiled-in version below, and offers a one-tap download when newer.
//
// Derived from the generated Version object (VersionGeneratorPlugin in
// composeApp/build.gradle.kts) — the SAME versionCode the APK manifest uses.
// publish-apk.sh assigns the code, so the in-app version can't drift from the
// installed APK. There is no semantic versionName anymore: the build is
// identified by versionCode alone, surfaced as "빌드 N" in the UI.
const val DENEB_VERSION_CODE = Version.appVersionCode

/** Parsed update manifest served by the gateway's /api/v1/app/update/manifest. */
@Serializable
data class UpdateManifest(
    val code: Int = 0,
    val file: String = "",
    val notes: String = "",
)

/** A newer build is available — surfaced to the settings UI. */
data class UpdateInfo(
    val buildLabel: String,
    val apkUrl: String,
    val notes: String,
)
