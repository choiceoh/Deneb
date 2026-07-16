package ai.deneb.deneb

import ai.deneb.getAppFilesDirectory
import java.io.File

internal actual fun platformWikiMirrorFiles(): WikiMirrorFiles = DesktopWikiMirrorFiles(File(getAppFilesDirectory(), "wiki_mirror"))

internal class DesktopWikiMirrorFiles(private val dir: File) : WikiMirrorFiles {
    override fun read(name: String): String? {
        val f = File(dir, name)
        return if (f.isFile) f.readText() else null
    }

    override fun write(name: String, content: String) {
        dir.mkdirs()
        File(dir, name).writeText(content)
    }

    override fun delete(name: String) {
        File(dir, name).delete()
    }
}
