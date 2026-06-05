import com.android.build.api.variant.impl.VariantOutputImpl

plugins {
    alias(libs.plugins.androidApplication)
    alias(libs.plugins.composeMultiplatform)
    alias(libs.plugins.composeCompiler)
}

// versionCode normally comes from libs.versions.toml, but publish-apk.sh overrides
// it with -PdenebVersionCode=<auto>. That lets concurrent agent worktrees each get
// a distinct, monotonically increasing code (serve-dir max + 1, flock-serialized)
// instead of all hand-bumping libs and colliding. IDE/dev builds with no property
// fall back to the libs value.
val denebVersionCode: Int =
    (findProperty("denebVersionCode") as? String)?.toIntOrNull()
        ?: libs.versions.android.versionCode.get().toInt()

android {
    namespace = "com.inspiredandroid.kai"
    compileSdk =
        libs.versions.android.compileSdk
            .get()
            .toInt()
    ndkVersion = "29.0.14206865"

    defaultConfig {
        applicationId = "com.inspiredandroid.kai"
        minSdk =
            libs.versions.android.minSdk
                .get()
                .toInt()
        targetSdk =
            libs.versions.android.targetSdk
                .get()
                .toInt()
        versionCode = denebVersionCode
        versionName = libs.versions.appVersion.get()
    }

    flavorDimensions += "distribution"
    productFlavors {
        create("playStore") {
            dimension = "distribution"
        }
        create("foss") {
            dimension = "distribution"
            isDefault = true
        }
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
        jniLibs {
            useLegacyPackaging = true
        }
    }

    signingConfigs {
        create("release") {
            val ksFile = System.getenv("KEYSTORE_FILE")
            if (ksFile != null) {
                storeFile = file(ksFile)
                storePassword = System.getenv("KEYSTORE_PASSWORD")
                keyAlias = System.getenv("KEY_ALIAS")
                keyPassword = System.getenv("KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        getByName("release") {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "../composeApp/proguard-rules.pro")
            signingConfig =
                if (System.getenv("KEYSTORE_FILE") != null) {
                    signingConfigs.getByName("release")
                } else {
                    signingConfigs.getByName("debug")
                }
        }
    }

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }
}

// Name build artifacts with the version + short commit hash so a downloaded APK
// is self-describing (e.g. deneb-2.8.1-122-a1b2c3d4-fossDebug.apk) and, crucially,
// concurrent builds from different agent worktrees never overwrite each other in
// the shared publish dir. The hash comes from DENEB_BUILD_SHA, else git, else "nogit".
androidComponents {
    val versionName = libs.versions.appVersion.get()
    val versionCode = denebVersionCode
    val gitSha = (
        System.getenv("DENEB_BUILD_SHA")
            ?: runCatching {
                ProcessBuilder("git", "rev-parse", "--short=8", "HEAD")
                    .directory(rootDir)
                    .start()
                    .inputStream.bufferedReader().use { it.readText() }
                    .trim()
            }.getOrNull()
        ).orEmpty().ifBlank { "nogit" }
    onVariants { variant ->
        variant.outputs.forEach { output ->
            (output as? VariantOutputImpl)?.outputFileName?.set(
                "deneb-$versionName-$versionCode-$gitSha-${variant.name}.apk",
            )
        }
    }
}

dependencies {
    implementation(project(":composeApp"))
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.androidx.foundation.android)
    implementation(libs.compose.material3)
    implementation(libs.koin.android)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.filekit.core)
    implementation(libs.filekit.compose)
    implementation(libs.tts)
    implementation(libs.tts.compose)
    implementation(libs.compose.components.uiToolingPreview)
    debugImplementation(libs.compose.ui.tooling)
    "playStoreImplementation"(libs.play.review)
}
