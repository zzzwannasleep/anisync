plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "com.zzzwannasleep.anisync"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // The application id is part of the published contract, not a build
        // detail: the Go side derives the default workspace from it, so
        // config.yaml and data/ live under
        // /data/data/com.zzzwannasleep.anisync/files/anisync/
        // (docs/ops-api.md §5 -- core.init's example uses exactly this path).
        // Changing it moves every user's data. Do not touch it casually.
        applicationId = "com.zzzwannasleep.anisync"

        // API 24 (Android 7.0) is the floor for the rdparty .so we ship:
        // it is the first release where the NDK-built libanisync_core.so can
        // be dlopen'd from the APK's lib/<abi>/ directory without hacks, and
        // it covers ~99% of devices ANI-RSS users have. See docs/build.md.
        minSdk = 24

        targetSdk = flutter.targetSdkVersion
        // Uses the version code from pubspec.yaml. When using split APKs, 1000 * ABI_VERSION
        // is added automatically by Flutter. (https://developer.android.com/studio/build/configure-apk-splits#configure-APK-versions)
        // You can force using the value of versionCode by specifying the `-P force-version-code-ignoring-abi=true`
        // flag during build.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    // libanisync_core.so is produced by scripts/build-core.ps1 into
    // android/app/src/main/jniLibs/<abi>/ (arm64-v8a, x86_64) and packaged into
    // the APK by AGP; the Go side is never compiled by Gradle. The directory is
    // declared explicitly so the wiring is visible in the build file instead of
    // relying on AGP's implicit default (see docs/ops-api.md §1.2).
    sourceSets {
        getByName("main") {
            jniLibs.srcDirs("src/main/jniLibs")
        }
    }

    buildTypes {
        release {
            // TODO: Add your own signing config for the release build.
            // Signing with the debug keys for now, so `flutter run --release` works.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
