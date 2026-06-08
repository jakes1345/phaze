import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("com.google.gms.google-services")
}

// ─── Signing credentials from local.properties (gitignored) ───────────────────
// Copy local.properties.example → local.properties and fill in your values.
// Never commit passwords to source control.
val localProps = Properties().also { props ->
    val f = rootProject.file("local.properties")
    if (f.exists()) props.load(f.inputStream())
}
fun localProp(key: String, fallback: String = "") =
    (localProps[key] as? String)?.takeIf { it.isNotBlank() } ?: fallback

// ─── Version ──────────────────────────────────────────────────────────────────
val appVersionCode = 16
val appVersionName = "1.3.0"

android {
    namespace = "world.phazechat.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "world.phazechat.app"
        minSdk = 26
        targetSdk = 35
        versionCode = appVersionCode
        versionName = appVersionName
    }

    signingConfigs {
        create("release") {
            // Reads from local.properties — never hardcode credentials here.
            // KEYSTORE_PATH is relative to the android/ project root.
            // Default: app/phaze-release.keystore  (i.e. android/app/phaze-release.keystore)
            storeFile = rootProject.file(localProp("KEYSTORE_PATH", "app/phaze-release.keystore"))
            storePassword = localProp("KEYSTORE_PASS")
            keyAlias = localProp("KEY_ALIAS", "phaze")
            keyPassword = localProp("KEY_PASS")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("release")
        }
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
    }

    // AAB bundle optimisations for Play Store
    bundle {
        language { enableSplit = true }
        density { enableSplit = true }
        abi { enableSplit = true }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
}

dependencies {
    // Compose BOM
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.navigation:navigation-compose:2.8.5")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")

    // Networking
    implementation("com.squareup.okhttp3:okhttp:4.12.0")

    // JSON
    implementation("org.json:json:20231013")

    // Crypto (NaCl via Lazysodium)
    implementation("com.goterl:lazysodium-android:5.1.0@aar")
    implementation("net.java.dev.jna:jna:5.14.0@aar")

    // Core (NotificationCompat for the screen-share foreground service)
    implementation("androidx.core:core-ktx:1.13.1")

    // WebRTC
    implementation("io.getstream:stream-webrtc-android:1.2.2")

    // Firebase Cloud Messaging
    implementation(platform("com.google.firebase:firebase-bom:33.7.0"))
    implementation("com.google.firebase:firebase-messaging")

    // Image loading
    implementation("io.coil-kt:coil-compose:2.7.0")

    // DataStore for preferences
    implementation("androidx.datastore:datastore-preferences:1.1.1")

    // QR Code Scanning (ZXing)
    implementation("com.google.zxing:core:3.5.3")

    // CameraX for QR scanner
    val cameraVersion = "1.4.1"
    implementation("androidx.camera:camera-camera2:$cameraVersion")
    implementation("androidx.camera:camera-lifecycle:$cameraVersion")
    implementation("androidx.camera:camera-view:$cameraVersion")

    debugImplementation("androidx.compose.ui:ui-tooling")
}
