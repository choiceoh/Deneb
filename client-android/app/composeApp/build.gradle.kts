import org.jetbrains.compose.desktop.application.dsl.TargetFormat
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
        mainClass = "ai.deneb.MainKt"

        buildTypes.release.proguard {
            configurationFiles.from(
                project.file("proguard-rules.pro"),
                project.file("proguard-desktop.pro"),
            )
        }

        nativeDistributions {
            targetFormats(TargetFormat.Dmg, TargetFormat.Msi, TargetFormat.Deb, TargetFormat.Rpm, TargetFormat.AppImage)
            packageName = "Deneb"
            // versionName was removed; derive a semver-shaped package version from the build code.
            // Honor the -PdenebVersionCode override (CI injects the git commit count) so each desktop
            // build gets a unique, increasing MSI ProductVersion. Without this every installer shared
            // 0.0.<floor> and Windows Installer silently skipped the upgrade (the build stayed pinned).
            // Falls back to the libs floor for local/IDE builds that don't pass the property.
            packageVersion = "0.0.${(project.findProperty("denebVersionCode") as? String) ?: libs.versions.android.versionCode
                .get()}"

            macOS {
                iconFile.set(project.file("icon.icns"))
            }
            windows {
                iconFile.set(project.file("icon.ico"))
                menuGroup = "Deneb"
            }
            linux {
                iconFile.set(project.file("icon.png"))
                modules("jdk.security.auth")
            }
        }
    }
}

// BouncyCastle is a cryptographically signed JCE provider jar. ProGuard rewrites
// it and strips the META-INF signatures, causing "SHA-256 digest error" at
// runtime. After ProGuard finishes, replace the processed jar with the original.
afterEvaluate {
    tasks.matching { it.name == "proguardReleaseJars" }.configureEach {
        doLast {
            val proguardDir =
                layout.buildDirectory
                    .dir("compose/tmp/main-release/proguard")
                    .get()
                    .asFile
            val processedJar = proguardDir.listFiles()?.find { it.name.startsWith("bcprov") } ?: return@doLast
            val originalJar =
                configurations["desktopRuntimeClasspath"]
                    .resolve()
                    .find { it.name.startsWith("bcprov") } ?: return@doLast
            originalJar.copyTo(processedJar, overwrite = true)
            logger.lifecycle("Restored original signed BouncyCastle jar: ${processedJar.name}")
        }
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
}
