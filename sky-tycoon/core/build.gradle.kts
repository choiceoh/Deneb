plugins {
    alias(libs.plugins.kotlinMultiplatform)
    alias(libs.plugins.kotlinSerialization)
}

kotlin {
    jvm()

    sourceSets {
        commonMain.dependencies {
            implementation(libs.kotlinx.serialization.json)
        }
        commonTest.dependencies {
            implementation(libs.kotlin.test)
        }
    }
}

// Android 타깃은 SDK 가 있을 때만. AGP 타입에 정적으로 의존하지 않도록 Groovy 스크립트로 분리한다
// (SDK 없는 환경에서는 .kts 가 AGP 클래스를 못 찾아 스크립트 컴파일 단계에서 깨지기 때문).
if (gradle.extra["hasAndroidSdk"] as Boolean) {
    apply(from = "android.gradle")
}
