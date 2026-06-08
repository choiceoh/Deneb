package com.inspiredandroid.kai.deneb

import kotlin.test.Test
import kotlin.test.assertTrue

class DenebPatchNotesTest {

    // versionName was removed, so the sheet is a flat reverse-chronological changelog
    // with no per-entry version label (and no head==current-build guard — there is no
    // semantic version to match against anymore). The only invariant left is that the
    // list is non-empty and every entry carries at least one highlight line, since an
    // empty entry would render as a blank gap in the "패치노트" sheet.
    @Test
    fun everyEntryHasHighlights() {
        assertTrue(
            DENEB_PATCH_NOTES.isNotEmpty(),
            "DENEB_PATCH_NOTES must not be empty",
        )
        assertTrue(
            DENEB_PATCH_NOTES.all { it.highlights.isNotEmpty() },
            "every patch note needs at least one highlight line",
        )
    }
}
