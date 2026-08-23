package ai.deneb.deneb

import android.content.Context
import android.os.Bundle
import android.os.Parcel
import android.webkit.WebView
import java.io.File

/**
 * Disk parking for one tab's WebView session state, so a killed process
 * restores the tab's back/forward history and scroll position instead of only
 * reloading the last URL.
 *
 * The in-memory path (`DenebWebViewState.platformState`) parks a state Bundle
 * while a tab is detached; this class is the same idea with a disk source, for
 * the case where the process dies before the user returns. Files live under
 * `filesDir/browser-tab-state/<tabId>.state` and are CONSUMED on load — a
 * restored state is never re-applied twice, and a corrupt file is deleted
 * rather than wedging its tab.
 *
 * Orphan files (a closed tab's state) are bounded, not tracked: the directory
 * keeps the newest [MAX_FILES] files and evicts the rest, so no cross-platform
 * bookkeeping is needed for tab closes.
 */
internal object BrowserTabStateDisk {
    private const val DIR_NAME = "browser-tab-state"
    private const val FORMAT_VERSION = 1
    private const val MAX_STATE_BYTES = 1 shl 20 // a back/forward list is KBs; 1MB is a broken outlier
    private const val MAX_FILES = 12 // 8 live tabs + headroom

    /** Serializes [web]'s session now; best-effort, never throws. */
    fun save(context: Context, tabId: String, web: WebView) {
        if (tabId.isBlank()) return
        val bundle = Bundle()
        if (runCatching { web.saveState(bundle) }.getOrNull() == null) return
        val bytes = runCatching {
            val parcel = Parcel.obtain()
            try {
                parcel.writeInt(FORMAT_VERSION)
                parcel.writeInt(web.scrollX)
                parcel.writeInt(web.scrollY)
                parcel.writeBundle(bundle)
                parcel.marshall()
            } finally {
                parcel.recycle()
            }
        }.getOrNull() ?: return
        if (bytes.isEmpty() || bytes.size > MAX_STATE_BYTES) return
        runCatching {
            val dir = dir(context)
            dir.mkdirs()
            val target = File(dir, fileName(tabId))
            val tmp = File(dir, "${fileName(tabId)}.tmp")
            tmp.writeBytes(bytes)
            if (!tmp.renameTo(target)) {
                tmp.delete()
                target.writeBytes(bytes)
            }
            pruneToBudget(dir)
        }
    }

    /** Reads and consumes the saved state, or null when none/corrupt. */
    fun load(context: Context, tabId: String): SavedState? {
        if (tabId.isBlank()) return null
        val file = File(dir(context), fileName(tabId))
        val bytes = runCatching { file.readBytes() }.getOrNull()
        file.delete()
        if (bytes == null || bytes.size > MAX_STATE_BYTES) return null
        return runCatching {
            val parcel = Parcel.obtain()
            try {
                parcel.unmarshall(bytes, 0, bytes.size)
                parcel.setDataPosition(0)
                val version = parcel.readInt()
                if (version != FORMAT_VERSION) return null
                val scrollX = parcel.readInt()
                val scrollY = parcel.readInt()
                val bundle = parcel.readBundle(BrowserTabStateDisk::class.java.classLoader) ?: return null
                SavedState(bundle, scrollX, scrollY)
            } finally {
                parcel.recycle()
            }
        }.getOrNull()
    }

    /** Drops the saved state — a deliberate navigation made its history stale. */
    fun remove(context: Context, tabId: String) {
        if (tabId.isBlank()) return
        runCatching { File(dir(context), fileName(tabId)).delete() }
    }

    private fun dir(context: Context): File = File(context.filesDir, DIR_NAME)

    private fun pruneToBudget(dir: File) {
        val files = runCatching { dir.listFiles { f -> f.isFile }?.toList() }.getOrNull() ?: return
        if (files.size <= MAX_FILES) return
        files.sortedByDescending { it.lastModified() }.drop(MAX_FILES).forEach { runCatching { it.delete() } }
    }

    /** Tab ids are [A-Za-z0-9_-]+ already (decode-sanitized); hash anything
     *  unexpected instead of letting it near a path. */
    private fun fileName(tabId: String): String = if (tabId.all { it.isLetterOrDigit() || it == '-' || it == '_' }) {
        "$tabId.state"
    } else {
        "${Integer.toHexString(tabId.hashCode())}.state"
    }

    internal data class SavedState(val bundle: Bundle, val scrollX: Int, val scrollY: Int)
}
