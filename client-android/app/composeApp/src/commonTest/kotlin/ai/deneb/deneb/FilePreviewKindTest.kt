package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class FilePreviewKindTest {

    @Test
    fun everySupportedImageExtensionUsesImagePreview() {
        val extensions = listOf("png", "jpg", "jpeg", "gif", "webp", "bmp")

        extensions.forEach { extension ->
            assertEquals(
                FilePreviewKind.IMAGE,
                previewKindOf("image.$extension"),
                "expected .$extension to use the image preview",
            )
        }
    }

    @Test
    fun everySupportedTextExtensionUsesTextPreview() {
        val extensions = listOf(
            "txt",
            "md",
            "markdown",
            "json",
            "csv",
            "tsv",
            "log",
            "xml",
            "yaml",
            "yml",
            "kt",
            "go",
            "py",
            "js",
            "ts",
            "tsx",
            "jsx",
            "sh",
            "conf",
            "ini",
            "toml",
            "java",
            "c",
            "cpp",
            "h",
            "rs",
            "rb",
            "php",
            "sql",
            "html",
            "css",
            "env",
            "properties",
            "gradle",
            "kts",
        )

        extensions.forEach { extension ->
            assertEquals(
                FilePreviewKind.TEXT,
                previewKindOf("source.$extension"),
                "expected .$extension to use the text preview",
            )
        }
    }

    @Test
    fun extensionMatchingIsCaseInsensitive() {
        val cases = mapOf(
            "PHOTO.JPEG" to FilePreviewKind.IMAGE,
            "diagram.PnG" to FilePreviewKind.IMAGE,
            "README.MD" to FilePreviewKind.TEXT,
            "build.GrAdLe" to FilePreviewKind.TEXT,
        )

        cases.forEach { (name, kind) -> assertEquals(kind, previewKindOf(name), name) }
    }

    @Test
    fun finalExtensionControlsPreviewForMultiDotNames() {
        assertEquals(FilePreviewKind.IMAGE, previewKindOf("release.notes.final.webp"))
        assertEquals(FilePreviewKind.TEXT, previewKindOf("archive.backup.json"))
        assertNull(previewKindOf("notes.md.zip"))
        assertNull(previewKindOf("photo.jpg.part"))
    }

    @Test
    fun supportedDotfilesFollowTheirExtension() {
        assertEquals(FilePreviewKind.TEXT, previewKindOf(".env"))
        assertEquals(FilePreviewKind.IMAGE, previewKindOf(".png"))
        assertNull(previewKindOf(".gitignore"))
        assertNull(previewKindOf(".env.local"))
    }

    @Test
    fun directoryDotsDoNotCreateAnExtensionForTheLeafName() {
        assertNull(previewKindOf("folder.with.dots/README"))
        assertEquals(FilePreviewKind.TEXT, previewKindOf("folder.with.dots/README.md"))
        assertEquals(FilePreviewKind.IMAGE, previewKindOf("folder.with.dots/photo.png"))
    }

    @Test
    fun namesWithoutAUsableExtensionFallBackToShareLink() {
        listOf(
            "",
            "README",
            "LICENSE",
            "name.",
            "name..",
            ".",
            "..",
        ).forEach { name -> assertNull(previewKindOf(name), name) }
    }

    @Test
    fun binaryAndDocumentFormatsAreNotClaimedAsText() {
        listOf(
            "manual.pdf",
            "proposal.docx",
            "sheet.xlsx",
            "slides.pptx",
            "bundle.zip",
            "binary.exe",
            "library.so",
            "raw.dng",
        ).forEach { name -> assertNull(previewKindOf(name), name) }
    }

    @Test
    fun nearMatchExtensionsDoNotReceiveAFalsePreview() {
        listOf(
            "photo.jpeg2000",
            "photo.png2",
            "notes.markdownx",
            "source.kotlin",
            "config.yaml-old",
            "script.ts.bak",
        ).forEach { name -> assertNull(previewKindOf(name), name) }
    }

    @Test
    fun unicodeAndWhitespaceInBaseNameDoNotAffectExtension() {
        assertEquals(FilePreviewKind.TEXT, previewKindOf("회의 기록 최종.md"))
        assertEquals(FilePreviewKind.IMAGE, previewKindOf("프로필 사진 2.JPG"))
        assertEquals(FilePreviewKind.TEXT, previewKindOf("  spaced name  .toml"))
    }
}
