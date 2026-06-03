package com.inspiredandroid.kai.deneb

import kotlinx.serialization.Serializable

// Self-served in-app update. The gateway serves the APK + manifest on its own
// port (the same base URL the client already uses for chat), so the update
// check works over the cloudflare tunnel — unlike the old :19010 side-server
// the tunnel never routed. The client fetches the manifest, compares it to the
// compiled-in version below, and offers a one-tap download when newer.
//
// IMPORTANT: bump BOTH of these together with the Gradle versionCode/appVersion
// every time a new APK is published, otherwise the running app can't tell it's
// out of date. version.json's "code" must match the new APK's code.
const val DENEB_VERSION_CODE = 154
const val DENEB_VERSION_NAME = "2.9.31"

/** Parsed update manifest served by the gateway's /api/v1/app/update/manifest. */
@Serializable
data class UpdateManifest(
    val code: Int = 0,
    val name: String = "",
    val file: String = "",
    val notes: String = "",
)

/** A newer build is available — surfaced to the settings UI. */
data class UpdateInfo(
    val versionName: String,
    val apkUrl: String,
    val notes: String,
)
