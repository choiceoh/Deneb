package ai.deneb.deneb

// The wasm target has no app-files storage; the mirror simply never persists
// (in-memory only for the session), matching ConversationStorage's no-op.
internal actual fun platformWikiMirrorFiles(): WikiMirrorFiles = object : WikiMirrorFiles {
    private val mem = mutableMapOf<String, String>()

    override fun read(name: String): String? = mem[name]

    override fun write(name: String, content: String) {
        mem[name] = content
    }

    override fun delete(name: String) {
        mem.remove(name)
    }
}
