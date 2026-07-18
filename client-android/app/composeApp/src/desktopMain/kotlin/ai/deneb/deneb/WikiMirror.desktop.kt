package ai.deneb.deneb

import ai.deneb.getAppFilesDirectory
import java.io.File
import java.io.FileOutputStream
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.Files
import java.nio.file.StandardCopyOption

internal actual fun platformWikiMirrorFiles(): WikiMirrorFiles = DesktopWikiMirrorFiles(File(getAppFilesDirectory(), "wiki_mirror"))

internal class DesktopWikiMirrorFiles(private val dir: File) : WikiMirrorFiles {
    override fun read(name: String): String? {
        val f = File(dir, name)
        return if (f.isFile) f.readText() else null
    }

    override fun write(name: String, content: String) {
        dir.mkdirs()
        val target = File(dir, name)
        val temporary = File.createTempFile(".${target.name}.", ".tmp", dir)
        try {
            FileOutputStream(temporary).use { output ->
                output.write(content.encodeToByteArray())
                output.fd.sync()
            }
            try {
                Files.move(
                    temporary.toPath(),
                    target.toPath(),
                    StandardCopyOption.ATOMIC_MOVE,
                    StandardCopyOption.REPLACE_EXISTING,
                )
            } catch (_: AtomicMoveNotSupportedException) {
                Files.move(temporary.toPath(), target.toPath(), StandardCopyOption.REPLACE_EXISTING)
            }
        } finally {
            temporary.delete()
        }
    }

    override fun delete(name: String) {
        File(dir, name).delete()
    }
}
