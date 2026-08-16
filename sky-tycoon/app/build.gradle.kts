import org.jetbrains.compose.desktop.application.dsl.TargetFormat

plugins {
    alias(libs.plugins.kotlinMultiplatform)
    alias(libs.plugins.composeMultiplatform)
    alias(libs.plugins.composeCompiler)
}

kotlin {
    jvm("desktop")

    sourceSets {
        commonMain.dependencies {
            implementation(project(":core"))
            implementation(compose.runtime)
            implementation(compose.foundation)
            implementation(compose.material3)
            implementation(compose.materialIconsExtended)
            implementation(compose.ui)
        }
        commonTest.dependencies {
            implementation(libs.kotlin.test)
        }
        val desktopMain by getting {
            dependencies {
                implementation(compose.desktop.currentOs)
                implementation(libs.kotlinx.coroutines.core)
            }
        }
    }
}

compose.desktop {
    application {
        mainClass = "skytycoon.ui.desktop.MainKt"
        nativeDistributions {
            targetFormats(TargetFormat.Dmg, TargetFormat.Msi, TargetFormat.Deb)
            packageName = "SkyTycoon"
            packageVersion = "1.0.0"
        }
    }
}

if (gradle.extra["hasAndroidSdk"] as Boolean) {
    apply(from = "android.gradle")
}

// 헤드리스 스크린샷 — X 서버 없이 Skia 로 화면을 직접 렌더해 PNG 로 떨군다.
val desktopMainCompilation = kotlin.targets.getByName("desktop").compilations.getByName("main")
tasks.register<JavaExec>("screenshots") {
    group = "verification"
    description = "주요 화면을 PNG 로 렌더해 build/screenshots 에 저장한다."
    dependsOn(desktopMainCompilation.compileTaskProvider)
    mainClass.set("skytycoon.ui.desktop.ScreenshotMainKt")
    classpath = files(desktopMainCompilation.output.allOutputs, desktopMainCompilation.runtimeDependencyFiles)
    systemProperty("skiko.renderApi", "SOFTWARE")
    systemProperty("java.awt.headless", "true")
    args = listOf(layout.buildDirectory.dir("screenshots").get().asFile.absolutePath)
}
