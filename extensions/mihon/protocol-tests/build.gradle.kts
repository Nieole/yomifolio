plugins { kotlin("jvm") }

kotlin {
    jvmToolchain(21)
    sourceSets {
        main {
            kotlin.srcDir("../src/main/kotlin")
            kotlin.include("**/Connection.kt", "**/Protocol.kt", "**/PasswordPreferences.kt")
        }
        test { kotlin.srcDir("../src/test/kotlin") }
    }
}
dependencies {
    implementation("org.jetbrains.kotlin:kotlin-stdlib:2.1.20")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.json:json:20240303")
    testImplementation("junit:junit:4.13.2")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
}
