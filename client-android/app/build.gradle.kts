plugins {
    // this is necessary to avoid the plugins to be loaded multiple times
    // in each subproject's classloader
    alias(libs.plugins.androidApplication) apply false
    alias(libs.plugins.androidLibrary) apply false
    alias(libs.plugins.androidMultiplatformLibrary) apply false
    alias(libs.plugins.composeMultiplatform) apply false
    alias(libs.plugins.composeCompiler) apply false
    alias(libs.plugins.kotlinMultiplatform) apply false
    // FCM: kept on the classpath but applied conditionally in :androidApp (only
    // when google-services.json is present), so desktop/CI builds that don't ship
    // the credential still configure. See androidApp/build.gradle.kts.
    alias(libs.plugins.googleServices) apply false
    alias(libs.plugins.spotless)
    alias(libs.plugins.detekt)
}

// Bug-focused static analysis over every Kotlin source set (no type
// resolution — fast, no compile needed). Style/complexity nags stay off in
// config/detekt.yml; spotless owns formatting. Mirrors the gateway's
// golangci philosophy: lint to block bugs, not to nag about style.
configure<io.gitlab.arturbosch.detekt.extensions.DetektExtension> {
    source.setFrom(
        files(
            "composeApp/src/commonMain/kotlin",
            "composeApp/src/androidMain/kotlin",
            "composeApp/src/desktopMain/kotlin",
            "composeApp/src/iosMain/kotlin",
            "composeApp/src/wasmJsMain/kotlin",
            "composeApp/src/jvmShared/kotlin",
            "androidApp/src",
        ),
    )
    config.setFrom(files("config/detekt.yml"))
    buildUponDefaultConfig = true
    baseline = file("config/detekt-baseline.xml")
}

configure<com.diffplug.gradle.spotless.SpotlessExtension> {
    kotlin {
        target("**/*.kt")
        targetExclude("**/build/**")
        ktlint()
            .editorConfigOverride(
                mapOf(
                    "ktlint_standard_no-wildcard-imports" to "disabled",
                    "ktlint_standard_package-name" to "disabled",
                    "ktlint_standard_function-naming" to "disabled",
                    "ktlint_standard_discouraged-comment-location" to "disabled",
                    "ktlint_standard_value-argument-comment" to "disabled",
                    "ktlint_standard_value-parameter-comment" to "disabled",
                    // Style-taste rules that fight the house conventions
                    // (file-header KDocs, non-screaming compose constants,
                    // topic-named files, exposed backing properties):
                    "ktlint_standard_kdoc" to "disabled",
                    "ktlint_standard_property-naming" to "disabled",
                    "ktlint_standard_backing-property-naming" to "disabled",
                    "ktlint_standard_filename" to "disabled",
                ),
            )
    }
    kotlinGradle {
        target("**/*.gradle.kts")
        ktlint()
    }
}
