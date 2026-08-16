rootProject.name = "sky-tycoon"

pluginManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        gradlePluginPortal()
        mavenCentral()
        maven("https://maven.pkg.jetbrains.space/public/p/compose/dev")
    }
}

dependencyResolutionManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        maven("https://maven.pkg.jetbrains.space/public/p/compose/dev")
    }
}

// Android SDK 가 있는 개발 머신에서만 :android 모듈을 빌드에 포함한다.
// SDK 없는 환경(CI 컨테이너 등)에서도 :core 와 :app(데스크톱)은 그대로 빌드·테스트된다.
val androidSdkDir: String? = sequenceOf(
    System.getenv("ANDROID_HOME"),
    System.getenv("ANDROID_SDK_ROOT"),
    file("local.properties").takeIf { it.exists() }?.let { f ->
        java.util.Properties().apply { f.inputStream().use(::load) }.getProperty("sdk.dir")
    },
).firstOrNull { !it.isNullOrBlank() && file(it).isDirectory }

gradle.extra["hasAndroidSdk"] = androidSdkDir != null

include(":core", ":app")
if (androidSdkDir != null) {
    include(":android")
    logger.lifecycle("[sky-tycoon] Android SDK 감지 → :android 모듈 포함 ($androidSdkDir)")
} else {
    logger.lifecycle("[sky-tycoon] Android SDK 없음 → :core, :app(데스크톱)만 빌드합니다.")
}
