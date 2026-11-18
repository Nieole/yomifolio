plugins {
    id("com.android.application") version "8.9.1"
    kotlin("android") version "2.1.20"
}

// Public signing identity is supplied only by the trusted manual release job.
val releaseSecrets = listOf("MIHON_KEYSTORE_BASE64", "MIHON_KEYSTORE_PASSWORD",
    "MIHON_KEY_ALIAS", "MIHON_KEY_PASSWORD", "MIHON_KEYSTORE_PATH")
val releaseEnvironment = releaseSecrets.associateWith { providers.environmentVariable(it).orNull }
val canSignRelease = releaseEnvironment.values.all { !it.isNullOrBlank() }
gradle.taskGraph.whenReady {
    if (allTasks.any { it.name in setOf("assembleRelease", "packageRelease", "bundleRelease", "validateSigningRelease") }) {
        require(canSignRelease) { "Release requires all MIHON signing secrets and a temporary keystore path" }
        require(file(releaseEnvironment.getValue("MIHON_KEYSTORE_PATH")!!).isFile) {
            "Release keystore file is missing"
        }
    }
}

android {
    namespace = "eu.kanade.tachiyomi.extension.all.mangamanager"
    compileSdk = 35
    defaultConfig {
        applicationId = "eu.kanade.tachiyomi.extension.all.mangamanager"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "1.4.1"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_1_8
        targetCompatibility = JavaVersion.VERSION_1_8
    }
    kotlinOptions { jvmTarget = "1.8" }
    signingConfigs {
        if (canSignRelease) {
            create("publicRelease") {
                storeFile = file(releaseEnvironment.getValue("MIHON_KEYSTORE_PATH")!!)
                storePassword = releaseEnvironment.getValue("MIHON_KEYSTORE_PASSWORD")
                keyAlias = releaseEnvironment.getValue("MIHON_KEY_ALIAS")
                keyPassword = releaseEnvironment.getValue("MIHON_KEY_PASSWORD")
            }
        }
    }
    buildTypes {
        release {
            isMinifyEnabled = false
            isDebuggable = false
            if (canSignRelease) signingConfig = signingConfigs.getByName("publicRelease")
        }
    }
}

// The host owns these classes. Never package a second copy in the source APK.
dependencies {
    compileOnly("androidx.preference:preference:1.2.1")
    compileOnly("com.github.keiyoushi:extensions-lib:v1.4.2.1") { isTransitive = false }
    compileOnly("org.jetbrains.kotlin:kotlin-stdlib:2.1.20")
    compileOnly("com.squareup.okhttp3:okhttp:4.12.0")
    compileOnly("io.reactivex:rxjava:1.3.8")
    compileOnly("uy.kohesive.injekt:injekt-core:1.16.1")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
    testImplementation("org.jetbrains.kotlin:kotlin-stdlib:2.1.20")
    testImplementation("com.squareup.okhttp3:okhttp:4.12.0")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
}
