@file:OptIn(ExperimentalForeignApi::class)

package ai.deneb.deneb

import ai.deneb.getAppFilesDirectory
import kotlinx.cinterop.ExperimentalForeignApi
import platform.Foundation.NSFileManager
import platform.Foundation.NSString
import platform.Foundation.NSUTF8StringEncoding
import platform.Foundation.stringWithContentsOfFile
import platform.Foundation.writeToFile

internal actual fun platformWikiMirrorFiles(): WikiMirrorFiles = IosWikiMirrorFiles("${getAppFilesDirectory()}/wiki_mirror")

internal class IosWikiMirrorFiles(private val dir: String) : WikiMirrorFiles {
    override fun read(name: String): String? = NSString.stringWithContentsOfFile("$dir/$name", NSUTF8StringEncoding, null)

    override fun write(name: String, content: String) {
        NSFileManager.defaultManager.createDirectoryAtPath(dir, true, null, null)
        @Suppress("CAST_NEVER_SUCCEEDS")
        check((content as NSString).writeToFile("$dir/$name", true, NSUTF8StringEncoding, null)) {
            "Unable to atomically replace wiki mirror file $name"
        }
    }

    override fun delete(name: String) {
        NSFileManager.defaultManager.removeItemAtPath("$dir/$name", null)
    }
}
