import org.jetbrains.kotlin.gradle.ExperimentalWasmDsl
import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.targets.js.webpack.KotlinWebpackConfig

plugins {
    alias(libs.plugins.kotlinMultiplatform)
    alias(libs.plugins.androidMultiplatformLibrary)
    alias(libs.plugins.composeMultiplatform)
    alias(libs.plugins.composeCompiler)
    alias(libs.plugins.kotlinSerialization)
}

composeCompiler {
    stabilityConfigurationFiles.add(project.layout.projectDirectory.file("compose_stability.conf"))
}

kotlin {
    androidLibrary {
        namespace = "ai.deneb.shared"
        compileSdk =
            libs.versions.android.compileSdk
                .get()
                .toInt()
        minSdk =
            libs.versions.android.minSdk
                .get()
                .toInt()
        compilerOptions {
            jvmTarget.set(JvmTarget.JVM_21)
        }
        androidResources {
            enable = true
        }
        withHostTest {}
    }

    listOf(
        iosArm64(),
        iosSimulatorArm64(),
    ).forEach { iosTarget ->
        iosTarget.binaries.framework {
            baseName = "ComposeApp"
            // Kept dynamic. Originally forced by LiteRT-LM (its Package.swift `-Xlinker
            // -all_load` swept ComposeApp's static archive into thousands of duplicate
            // symbols at link time). LiteRT-LM has since been removed; isStatic=false is
            // retained until iOS linking is re-verified (likely revertible to true).
            isStatic = false
            // Must differ from the iosApp bundle identifier — iOS refuses to install a
            // .app whose embedded framework shares its parent's identifier (MIInstaller
            // error 57 / DuplicateIdentifier).
            binaryOption("bundleId", "ai.deneb.composeapp")
        }
    }

    jvm("desktop")

    @OptIn(ExperimentalWasmDsl::class)
    wasmJs {
        outputModuleName = "composeApp"
        browser {
            val rootDirPath = project.rootDir.path
            val projectDirPath = project.projectDir.path
            commonWebpackConfig {
                outputFileName = "composeApp.js"
                devServer =
                    (devServer ?: KotlinWebpackConfig.DevServer()).apply {
                        // Serve sources to debug inside browser
                        static(rootDirPath)
                        static(projectDirPath)
                    }
            }
        }
        binaries.executable()
    }

    sourceSets {
        val desktopMain by getting
        val commonMain by getting {
            kotlin.srcDir(layout.buildDirectory.dir("generated/src/commonMain/kotlin"))
        }
        val commonTest by getting {
            dependencies {
                implementation(kotlin("test"))
                implementation(libs.kotlinx.coroutines.test)
                implementation(libs.turbine)
                implementation(libs.multiplatform.settings.test)
            }
        }

        val androidMain by getting {
            kotlin.srcDir("src/jvmShared/kotlin")
        }
        desktopMain.kotlin.srcDir("src/jvmShared/kotlin")
        androidMain.dependencies {
            implementation(libs.androidx.activity.compose)
            implementation(libs.androidx.lifecycle.process)
            implementation(libs.spght.encryptedprefs)
            implementation(libs.ktor.client.android)
            implementation(libs.koin.android)
            implementation(libs.material)
            implementation(libs.bouncycastle.provider)
            // FusedLocationProvider for on-demand location sensing (foss flavor only
            // declares the permission; readCurrentLocation gates on it at runtime).
            implementation(libs.play.services.location)
        }
        commonMain.dependencies {
            implementation(libs.compose.material3)
            implementation(libs.compose.material.icons.core)
            implementation(libs.compose.material.icons.extended)
            implementation(libs.compose.runtime)
            implementation(libs.compose.foundation)
            implementation(libs.compose.ui)
            implementation(libs.compose.components.resources)
            implementation(libs.compose.components.uiToolingPreview)

            implementation(libs.androidx.navigation.compose)
            implementation(libs.androidx.lifecycle.viewmodel)
            implementation(libs.androidx.lifecycle.runtime.compose)
            implementation(libs.androidx.lifecycle.viewmodel.compose)

            implementation(libs.kotlinx.collections.immutable)
            implementation(libs.kotlinx.serialization.json)
            implementation(libs.kotlinx.datetime)

            implementation(libs.ktor.client.core)
            implementation(libs.ktor.client.content.negotiation)
            implementation(libs.ktor.serialization.kotlinx.json)
            implementation(libs.ktor.client.logging)

            implementation(libs.tts)
            implementation(libs.tts.compose)

            implementation(libs.koin.compose)
            implementation(libs.koin.compose.viewmodel)
            implementation(libs.koin.core)

            implementation(libs.multiplatform.settings)
            implementation(libs.multiplatform.settings.no.arg)

            implementation(libs.filekit.core)
            implementation(libs.filekit.compose)

            implementation(libs.coil.compose)
            implementation(libs.coil.svg)
            implementation(libs.coil.network.ktor3)

            implementation(libs.reorderable)
        }
        desktopMain.dependencies {
            implementation(compose.desktop.currentOs)
            implementation(libs.kotlinx.coroutines.swing)
            implementation(libs.ktor.client.cio)
            implementation(libs.bouncycastle.provider)
            implementation(libs.slf4j.nop)
            // Compose UI test API powers the headless semantics inspector (ui-inspect.sh /
            // the previewInspect task): it dumps the semantics tree as TEXT and drives nodes
            // by text/role, so a non-vision agent can verify + drive the mobile UI without
            // reading PNGs. Desktop-only — this is the verification harness target. Direct
            // coordinate because the compose.uiTest accessor is deprecated; the desktop
            // variant (ui-test-desktop, which carries runDesktopComposeUiTest) auto-resolves.
            implementation("org.jetbrains.compose.ui:ui-test:${libs.versions.compose.multiplatform.get()}")
        }
        iosMain.dependencies {
            implementation(libs.ktor.client.darwin)
            implementation(libs.ktor.network)
            implementation(libs.ktor.network.tls)
        }
        wasmJsMain.dependencies {
            implementation(libs.ktor.client.js)
        }
    }
}

compose.desktop {
    application {
        // The desktop JVM target is the headless mobile-UI verification substrate
        // (renderPreviews + native-app.sh `:composeApp:run`), not a shipped product —
        // the desktop workstation is a separate app (Andromeda). So only the run
        // entrypoint is configured. The installer packaging (nativeDistributions:
        // MSI/DMG/Deb/Rpm/AppImage) and its release-proguard (+ the BouncyCastle
        // signed-jar restore that only mattered for that proguard pass) were removed
        // with the desktop product.
        mainClass = "ai.deneb.MainKt"
    }
}

class VersionGeneratorPlugin : Plugin<Project> {
    override fun apply(project: Project) {
        project.afterEvaluate {
            // versionCode is the app's only version identity now — the semantic
            // versionName was removed (hand-managed, caused mislabeled/duplicate
            // publishes). Mirror androidApp's -PdenebVersionCode override so the
            // in-app updater's DENEB_VERSION_CODE matches the published APK; falls
            // back to libs for IDE/dev builds.
            val appVersionCode =
                (project.findProperty("denebVersionCode") as? String)
                    ?: libs.versions.android.versionCode
                        .get()

            // Generate Kotlin version file
            val versionFile =
                layout.buildDirectory
                    .file("generated/src/commonMain/kotlin/ai/deneb/Version.kt")
                    .get()
                    .asFile
            versionFile.parentFile?.mkdirs()
            versionFile.writeText(
                """
                package ai.deneb

                object Version {
                    const val appVersionCode = $appVersionCode
                }
                """.trimIndent(),
            )

            // Update iOS Config.xcconfig with version
            val xcConfigFile = rootProject.file("iosApp/Configuration/Config.xcconfig")
            if (xcConfigFile.exists()) {
                val appVersion = "0.0.$appVersionCode"
                val content = xcConfigFile.readText()
                val updatedContent =
                    if (content.contains("APP_VERSION=")) {
                        content.replace(Regex("APP_VERSION=.*"), "APP_VERSION=$appVersion")
                    } else {
                        content.trimEnd() + "\nAPP_VERSION=$appVersion\n"
                    }
                xcConfigFile.writeText(updatedContent)
            }

            // Aggregate the per-PR patch-note fragments in changelog.d/ into a compiled
            // list. The native changelog used to be one hand-edited listOf(...) in
            // DenebPatchNotes.kt; every PR prepended to the same top line, so every native
            // PR collided. Now each PR drops a YYYY-MM-DD-<slug>.md file there (new file →
            // no shared lines → no conflict), and this folds them — newest filename first —
            // into a generated source file under build/ that is NOT committed, so the
            // generated list can never itself become a shared file every PR edits (which
            // would just move the conflict). See changelog.d/README.md.
            val fragmentsDir = rootProject.file("changelog.d")
            val datePrefix = Regex("""^\d{4}-\d{2}-\d{2}-.*\.md$""")
            val fragments =
                (fragmentsDir.listFiles()?.toList() ?: emptyList())
                    .filter { it.isFile && datePrefix.matches(it.name) }
                    // Newest first: the date-prefixed name makes a reverse sort chronological.
                    .sortedByDescending { it.name }
            val entries =
                fragments.mapNotNull { file ->
                    val highlights =
                        file
                            .readLines()
                            .map { it.trim() }
                            .filter { it.isNotEmpty() && !it.startsWith("#") }
                    if (highlights.isEmpty()) {
                        null
                    } else {
                        val lines =
                            highlights.joinToString(",\n") { line ->
                                // Escape for a Kotlin "..." literal: backslash, quote, dollar.
                                val esc = line.replace("\\", "\\\\").replace("\"", "\\\"").replace("$", "\\$")
                                "            \"$esc\""
                            }
                        "    DenebPatchNote(\n        highlights = listOf(\n$lines,\n        ),\n    ),"
                    }
                }
            val body = if (entries.isEmpty()) "" else "\n${entries.joinToString("\n")}\n"
            val changelogFile =
                layout.buildDirectory
                    .file("generated/src/commonMain/kotlin/ai/deneb/deneb/DenebChangelogGenerated.kt")
                    .get()
                    .asFile
            changelogFile.parentFile?.mkdirs()
            changelogFile.writeText(
                "// Code generated from changelog.d/*.md by composeApp/build.gradle.kts. DO NOT EDIT.\n" +
                    "package ai.deneb.deneb\n\n" +
                    "/** Per-PR patch-note fragments, newest first. Prepended to the frozen history. */\n" +
                    "internal val GENERATED_CHANGELOG_FRAGMENTS: List<DenebPatchNote> = listOf($body)\n",
            )
        }
    }
}

apply<VersionGeneratorPlugin>()

// Off-screen render harness (desktopMain/.../RenderPreview.kt): renders Deneb
// composables to PNG via Skia so the UI can be inspected headlessly without
// building + installing the APK. Run: ./gradlew :composeApp:renderPreviews
tasks.register<JavaExec>("renderPreviews") {
    group = "deneb"
    description = "Render Deneb composable previews to /tmp/deneb-render/*.png"
    val desktopMain =
        kotlin.targets
            .getByName("desktop")
            .compilations
            .getByName("main")
    dependsOn(desktopMain.compileTaskProvider)
    classpath = files(desktopMain.output.allOutputs, desktopMain.runtimeDependencyFiles)
    mainClass.set("ai.deneb.RenderPreviewKt")
    systemProperty("java.awt.headless", "true")
    // The desktop target is a mobile-UI verifier now: render the real mobile branch
    // (Mobile.Android — bottom bar, mobile keyboard, system-back hides the in-app ←)
    // like native-app.sh's phone profile, not the desktop default. The override returns
    // early in Platform.jvm.kt (before any Platform.Desktop cast), so this is safe.
    systemProperty("deneb.platform", "phone")
}

tasks.register<JavaExec>("benchScrollRender") {
    group = "deneb"
    val desktopMain =
        kotlin.targets
            .getByName("desktop")
            .compilations
            .getByName("main")
    dependsOn(desktopMain.compileTaskProvider)
    classpath = files(desktopMain.output.allOutputs, desktopMain.runtimeDependencyFiles)
    mainClass.set("ai.deneb.ScrollRenderBenchKt")
    systemProperty("java.awt.headless", "true")
    systemProperty("deneb.platform", "phone")
}

// Headless semantics inspector (desktopMain/.../PreviewInspect.kt): dumps a screen's
// semantics tree as TEXT and drives it by node, so a non-vision agent can verify + drive
// the mobile UI without reading PNGs. Prefer scripts/dev/ui-inspect.sh; raw:
//   ./gradlew :composeApp:previewInspect -Pscreen=todo -Pactions="click:증가"
tasks.register<JavaExec>("previewInspect") {
    group = "deneb"
    description = "Dump (and optionally drive) a screen's semantics tree as text"
    val desktopMain =
        kotlin.targets
            .getByName("desktop")
            .compilations
            .getByName("main")
    dependsOn(desktopMain.compileTaskProvider)
    classpath = files(desktopMain.output.allOutputs, desktopMain.runtimeDependencyFiles)
    mainClass.set("ai.deneb.PreviewInspectKt")
    systemProperty("java.awt.headless", "true")
    systemProperty("deneb.platform", "phone")
    systemProperty("deneb.screen", (project.findProperty("screen") as? String) ?: "")
    systemProperty("deneb.actions", (project.findProperty("actions") as? String) ?: "")
    systemProperty("deneb.dark", (project.findProperty("dark") as? String) ?: "false")
}
