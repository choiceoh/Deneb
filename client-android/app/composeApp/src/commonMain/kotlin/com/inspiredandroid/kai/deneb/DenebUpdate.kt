package com.inspiredandroid.kai.deneb

import kotlinx.serialization.Serializable

// Self-served in-app update. The gateway host also serves the APK + a
// version.json on :19010 (the same box the operator builds on). The client
// fetches that manifest, compares it to the compiled-in version below, and
// offers a one-tap download when a newer build is published.
//
// IMPORTANT: bump BOTH of these together with the Gradle versionCode/appVersion
// every time a new APK is published, otherwise the running app can't tell it's
// out of date. version.json's "code" must match the new APK's code.
const val DENEB_VERSION_CODE = 152
const val DENEB_VERSION_NAME = "2.9.29"

/** Parsed update manifest (version.json served next to the APK). */
@Serializable
data class UpdateManifest(
    val code: Int = 0,
    val name: String = "",
    val url: String = "",
    val notes: String = "",
)

/** A newer build is available — surfaced to the settings UI. */
data class UpdateInfo(
    val versionName: String,
    val apkUrl: String,
    val notes: String,
)
