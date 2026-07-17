-dontwarn javax.annotation.Nullable
-dontwarn javax.annotation.concurrent.GuardedBy

# kotlinx.serialization — keep serializer infrastructure for R8 full mode
-if @kotlinx.serialization.Serializable class **
-keepclassmembers class <1> {
    static <1>$Companion Companion;
}

-if @kotlinx.serialization.Serializable class ** {
    static **$* *;
}
-keepclassmembers class <2>$<3> {
    kotlinx.serialization.KSerializer serializer(...);
}

-if @kotlinx.serialization.Serializable class ** {
    public static ** INSTANCE;
}
-keepclassmembers class <1> {
    public static <1> INSTANCE;
    kotlinx.serialization.KSerializer serializer(...);
}

-keepattributes RuntimeVisibleAnnotations,AnnotationDefault,SourceFile,LineNumberTable

# Ktor plugins loaded via ServiceLoader
-keep class * implements io.ktor.serialization.kotlinx.KotlinxSerializationExtensionProvider { <init>(); }

# SLF4J providers loaded via ServiceLoader (slf4j-nop on desktop)
-keep class * implements org.slf4j.spi.SLF4JServiceProvider { <init>(); }

# FreeTTS — nl.marc_apps.tts uses it on desktop regardless of the engine enum.
# Voice directories are loaded via jar-manifest + Class.forName, which ProGuard
# cannot trace. Keep freetts broadly; size cost is ~400 KB since ProGuard
# could barely shrink it anyway.
-keep class com.sun.speech.freetts.** { *; }
-dontwarn com.sun.speech.freetts.**

# Ktor — HTTP client and networking (used by Coil for image loading).
# ProGuard strips ~50% of classes including JVM I/O adapters, auth headers,
# and content handlers that break HTTPS connections.
-keep class io.ktor.** { *; }
-dontwarn io.ktor.**

# Okio — I/O primitives used by Ktor CIO engine for sockets, TLS, compression.
# ProGuard strips Socket, CipherSink/Source, GzipSource, AsyncTimeout etc.
-keep class okio.** { *; }
-dontwarn okio.**

# Coil — ProGuard strips platform-specific Kotlin top-level function classes
# (SkiaImageDecoder_jvmKt, RealImageLoader_nonAndroidKt, FileSystems_jvmKt, …)
# that Coil needs at runtime on desktop. Keep broadly to avoid breakage.
# Coil does not ship consumer ProGuard rules (coil-kt/coil#2546).
-keep class coil3.** { *; }
-dontwarn coil3.**


# JNA — FileKit uses it on Windows (Shell32 IFileDialog / IShellItem COM) and
# macOS (Cocoa Foundation bindings). JNA resolves native symbols by exact
# method name via reflection; any rename produces errors like
# "Can't obtain static method dispose from class com.sun.jna.Native". See #167, #175.
-keep class com.sun.jna.** { *; }
-keep class * extends com.sun.jna.** { *; }
-keepclassmembers class * implements com.sun.jna.** {
    <methods>;
    <fields>;
}
-dontwarn com.sun.jna.**
-dontwarn java.awt.**

# FileKit — defines JNA Structure subclasses and COM interface proxies in
# .dialogs.platform.windows.jna.* and DBus interface proxies in
# .dialogs.platform.xdg.*. Method names must survive to match the native ABI.
-keep class io.github.vinceglb.filekit.** { *; }
-keep interface io.github.vinceglb.filekit.** { *; }
-dontwarn io.github.vinceglb.filekit.**

# DBus (dbus-java by hypfvieh) — used by FileKit's Linux XDG desktop-portal
# probe. Without it, the portal check fails and Linux falls back to
# java.awt.FileDialog which is unthemed on KDE/GNOME dark sessions. See #173.
-keep class org.freedesktop.dbus.** { *; }
-keep interface org.freedesktop.dbus.** { *; }
-keep class com.github.hypfvieh.** { *; }
-dontwarn org.freedesktop.dbus.**
-dontwarn com.github.hypfvieh.**

# kotlinx.coroutines — keep ALL members, not just volatile fields. Without
# this, ProGuard's method-specialization optimizer rewrites builders like
# async/launch into synthetic methods whose declared return type is the
# concrete subtype (DeferredCoroutine) while the return instruction yields
# the parent interface (Deferred); the JVM verifier rejects this with
# "Bad return type ... async$<hash>". The explicit -optimizations line
# disables that specialization family as a belt-and-braces safety net.
-keep class kotlinx.coroutines.** { *; }
-dontwarn kotlinx.coroutines.**
-optimizations !method/specialization/*

# ---- Desktop-release hardening (Windows/Linux/macOS) ----
# The Compose Gradle plugin ships default-compose-desktop-rules.pro with only
# targeted keeps (Skia, Skiko, SliderDefaults, SnapshotStateKt__DerivedStateKt,
# coroutine volatile fields, kotlinx.serialization infra). We add broader,
# defensive keeps for the libraries the app uses on desktop. Size cost is
# small because the app already references most of these APIs.

# kotlinx.io — Ktor 3.x public functions take kotlinx.io.Source/Sink/Buffer/
# RawSource/RawSink/files.Path as parameters. ProGuard prints
# "Note: ... but not the descriptor class 'kotlinx.io.Source'" ×dozens during
# the release build, meaning Ktor entry points reference kotlinx.io classes
# that are shrunk. Any Ktor I/O path then NoClassDefFoundErrors at runtime.
-keep class kotlinx.io.** { *; }
-dontwarn kotlinx.io.**

# kotlinx.datetime — the Compose plugin only dontwarns; app uses Instant,
# LocalDateTime, TimeZone extensively. Small (~60 KB) defensive keep.
-keep class kotlinx.datetime.** { *; }
-dontwarn kotlinx.datetime.**

# Koin — DI runtime resolves definitions by KClass identity. koinViewModel<T>()
# embeds a reified KClass at the call site that must match the class kept by
# ProGuard. The library itself uses kotlin-reflect heavily.
-keep class org.koin.** { *; }
-keepclassmembers class org.koin.** { *; }
-dontwarn org.koin.**

# App classes — Koin resolves dependencies via KClass lookup (get<Foo>()) and
# reified koinViewModel<T>(). ViewModel constructors take DataRepository,
# TaskScheduler, DaemonController, etc. as parameters; without keeping them,
# ProGuard flags "descriptor class missing" and the reflective KClass handle
# Koin holds would point at a renamed class. Also makes user-submitted
# stack traces readable without a mapping file.
-keep class ai.deneb.** { *; }

# androidx.lifecycle / savedstate — required by compose-navigation and
# Koin ViewModel support. Not covered by the Compose plugin defaults.
-keep class androidx.lifecycle.** { *; }
-keep class androidx.savedstate.** { *; }
-dontwarn androidx.lifecycle.**
-dontwarn androidx.savedstate.**

# Navigation Compose — typed routes are @Serializable, NavType lookup uses
# reflection, and the library references Android-only IntentActivity classes
# that don't exist on desktop (harmless but noisy without dontwarn).
-keep class androidx.navigation.** { *; }
-keep class org.jetbrains.androidx.navigation.** { *; }
-dontwarn androidx.navigation.**
-dontwarn org.jetbrains.androidx.navigation.**

# multiplatform-settings (russhwolf) — JVM backend delegates to
# java.util.prefs and uses reflection to pick the platform implementation.
-keep class com.russhwolf.settings.** { *; }
-dontwarn com.russhwolf.settings.**

# kotlinx.collections.immutable — ImmutableList / ImmutableMap are exposed
# from public APIs in the app; keeping whole app package above pins references
# to these types which must also survive.
-keep class kotlinx.collections.immutable.** { *; }
-dontwarn kotlinx.collections.immutable.**


# nl.marc-apps:tts — TextToSpeech instance type leaks into app signatures.
-keep class nl.marc_apps.tts.** { *; }
-dontwarn nl.marc_apps.tts.**

# SLF4J API — Ktor client logging binds to it; slf4j-nop provider is loaded
# via ServiceLoader (already kept above) and needs the Logger interface intact.
-keep class org.slf4j.** { *; }
-dontwarn org.slf4j.**

# Compose runtime / UI is intentionally NOT broadly kept here — the Compose
# Gradle plugin (desktop) and androidx.compose's consumer ProGuard rules
# (Android R8) already ship targeted keeps for the reflective hotspots
# (SliderDefaults, SnapshotStateKt__DerivedStateKt, etc.). Letting Compose
# be the one package ProGuard is allowed to rewrite is what buys us the size
# win on desktop (the desktop-release ProGuard pass and its
# proguard-desktop.pro were removed with the desktop product; this file now
# feeds Android R8 only — desktop-era keeps like FreeTTS/JNA/FileKit/DBus are
# inert on the Android graph and can be pruned once a device-verified release
# build confirms it).
-dontwarn androidx.compose.**
-dontwarn org.jetbrains.compose.**
-dontwarn androidx.annotation.**

# Generic-type reflection attributes — originally added for Gson (a LiteRT-LM
# transitive dep; both left the graph when that SDK was removed), but
# Signature/InnerClasses/EnclosingMethod are load-bearing for any reflective
# serialization on the Android graph, so the attribute keep stays. The
# Gson-specific class keeps were removed with the SDK.
-keepattributes Signature,InnerClasses,EnclosingMethod
-dontwarn com.google.gson.**
-dontwarn sun.misc.Unsafe

# WorkManager (androidx.work, added with the Android Auto voice-reply worker)
# boots via androidx.startup at process start and instantiates its Room
# database (WorkDatabase_Impl) reflectively through getDeclaredConstructor().
# R8 stripped that no-arg constructor in the first WorkManager-bearing release
# (build 571), killing the app before Application.onCreate:
#   NoSuchMethodException: androidx.work.impl.WorkDatabase_Impl.<init> []
# Standard Room keep: preserve no-arg constructors of generated RoomDatabases.
-keep class * extends androidx.room.RoomDatabase { <init>(); }
