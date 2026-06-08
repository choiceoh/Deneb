package com.inspiredandroid.kai.deneb

import com.inspiredandroid.kai.Version
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class DenebPatchNotesTest {

    // The patch-notes sheet is compiled-in and hand-maintained, so it can drift out of
    // sync with the shipped build without anyone noticing — it once sat at 2.9.30 while
    // releases reached 2.9.53, so "패치노트 보기" only ever showed old versions and the
    // user's own build was absent. This guard fails the build when a version bump lands
    // without a matching head entry, forcing the list to describe the running build.
    @Test
    fun headMatchesCurrentBuild() {
        assertEquals(
            Version.appVersion,
            DENEB_PATCH_NOTES.first().version,
            "DENEB_PATCH_NOTES head must equal Version.appVersion (${Version.appVersion}); " +
                "prepend a matching entry in DenebPatchNotes.kt when bumping appVersion in libs.versions.toml.",
        )
    }

    @Test
    fun versionsAreUniqueAndHaveHighlights() {
        val versions = DENEB_PATCH_NOTES.map { it.version }
        assertEquals(
            versions.size,
            versions.toSet().size,
            "duplicate version in DENEB_PATCH_NOTES: ${versions.groupingBy { it }.eachCount().filter { it.value > 1 }.keys}",
        )
        assertTrue(
            DENEB_PATCH_NOTES.all { it.highlights.isNotEmpty() },
            "every patch note needs at least one highlight line",
        )
    }
}
