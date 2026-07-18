package ai.deneb.deneb

import ai.deneb.getAppFilesDirectory
import android.util.AtomicFile
import java.io.File
import java.io.FileNotFoundException

internal actual fun platformWikiMirrorFiles(): WikiMirrorFiles = JvmWikiMirrorFiles(File(getAppFilesDirectory(), "wiki_mirror"))

internal class JvmWikiMirrorFiles(private val dir: File) : WikiMirrorFiles {
    override fun read(name: String): String? = try {
        AtomicFile(File(dir, name)).openRead().bufferedReader().use { it.readText() }
    } catch (_: FileNotFoundException) {
        null
    }

    override fun write(name: String, content: String) {
        dir.mkdirs()
        val target = AtomicFile(File(dir, name))
        val output = target.startWrite()
        try {
            output.write(content.encodeToByteArray())
            target.finishWrite(output)
        } catch (failure: Exception) {
            target.failWrite(output)
            throw failure
        }
    }

    override fun delete(name: String) {
        AtomicFile(File(dir, name)).delete()
    }
}
